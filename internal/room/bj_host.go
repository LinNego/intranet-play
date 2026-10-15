package room

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"intranet-play/internal/bj"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// bjInbound 是汇聚到主循环的一条消息。带上 conn 而不是座位号：
// 座位会在有人掉线时压缩，靠连接反查座位才不会张冠李戴。
type bjInbound struct {
	conn    *netx.Conn
	env     protocol.Envelope
	offline bool
}

// RunBjHost 开一个 2~6 人 21 点房间：本进程占座位 0，其余人 join 进来。
func RunBjHost(name string, port, chips, maxSeats int) error {
	ln, err := netx.Listen(netx.LocalAddr(port))
	if err != nil {
		return err
	}

	r := newBjHostRoom(fmt.Sprintf("%d", port), name, chips, maxSeats)
	r.logf("房间已开启（端口 %d，最多 %d 人，初始筹码 %d）", port, maxSeats, r.startChips)
	r.logf("等人齐后输入 start；对局中可用 hit/stand/double，结算后 next 开下一轮")
	// Run 之前不能 render（Program.Send 会永久阻塞），日志直接放进初始帧。
	r.app = ui.NewBjApp(r.frame())

	errCh := make(chan error, 1)
	go func() { errCh <- r.serveHost(ln) }()

	uiErr := r.app.Run()
	r.app.Quit()

	select {
	case loopErr := <-errCh:
		if loopErr != nil {
			return loopErr
		}
	default:
	}
	return uiErr
}

func (r *bjRoom) serveHost(ln net.Listener) error {
	defer ln.Close()

	acceptCh := make(chan *netx.Conn)
	go func() {
		defer close(acceptCh)
		for {
			conn, err := netx.Accept(ln)
			if err != nil {
				return
			}
			acceptCh <- conn
		}
	}()

	hub := make(chan bjInbound, 32)
	for {
		select {
		case conn, ok := <-acceptCh:
			if !ok {
				acceptCh = nil
				continue
			}
			r.acceptConn(conn, hub)

		case in := <-hub:
			r.handleInbound(in)

		case line, ok := <-r.app.Commands():
			if !ok {
				r.app.Quit()
				return nil
			}
			if r.handleHostLocal(line) {
				r.app.Quit()
				return nil
			}

		case <-r.app.Done():
			return nil
		}
	}
}

func (r *bjRoom) acceptConn(conn *netx.Conn, hub chan<- bjInbound) {
	if len(r.seats) >= r.maxSeats {
		env, err := protocol.NewEnvelope(protocol.TypeError, "room", protocol.ErrorPayload{
			Message: fmt.Sprintf("room full: 本桌最多 %d 人", r.maxSeats),
		})
		if err == nil {
			_ = conn.Send(env)
		}
		_ = conn.Close()
		r.logf("有第 %d 个连接尝试加入，已拒绝", len(r.seats)+1)
		r.render()
		return
	}
	seat := len(r.seats)
	r.seats = append(r.seats, &bjSeat{conn: conn, online: true})
	r.logf("有连接进入座位 %d，等待名字", seat)
	r.pushAll()

	go func() {
		for env := range conn.Inbox() {
			hub <- bjInbound{conn: conn, env: env}
		}
		hub <- bjInbound{conn: conn, offline: true}
	}()
}

func (r *bjRoom) handleInbound(in bjInbound) {
	seat := r.seatOf(in.conn)
	if seat < 0 {
		return // 这个人已经不在座位表里了
	}

	if in.offline {
		r.dropSeat(seat, "断线")
		return
	}

	switch in.env.Type {
	case protocol.TypeJoin:
		var p protocol.JoinPayload
		if err := json.Unmarshal(in.env.Payload, &p); err != nil || strings.TrimSpace(p.Name) == "" {
			r.sendErr(seat, "名字不合法")
			return
		}
		r.seats[seat].name = strings.TrimSpace(p.Name)
		r.seats[seat].joined = true
		r.logf("%s 加入牌桌，坐座位 %d", r.seats[seat].name, seat)
		if !r.started && r.allJoined() {
			r.logf("共 %d 人，房主可以输入 start 开局", len(r.seats))
		}
		r.pushAll()

	case protocol.TypeBjNext:
		r.tryNextRound(seat)

	case protocol.TypeBjAction:
		var p protocol.BjActionPayload
		if err := json.Unmarshal(in.env.Payload, &p); err != nil {
			r.sendErr(seat, "动作格式错误")
			return
		}
		if err := r.applyAction(seat, p.Action); err != nil {
			r.sendErr(seat, err.Error())
		}

	case protocol.TypeChat:
		var p protocol.ChatPayload
		if err := json.Unmarshal(in.env.Payload, &p); err != nil {
			return
		}
		msg := strings.TrimSpace(p.Message)
		if msg == "" {
			return
		}
		r.logf("%s: %s", r.nameOf(seat), msg)
		r.relayChat(seat, msg)
		r.render()

	case protocol.TypeLeave:
		r.dropSeat(seat, "离开")
	}
}

// dropSeat 把座位从表里摘掉并压缩，保持座位号连续（房间座位号 == 对局座位号）。
// 对局中有人掉线就作废本局回大厅：21 点虽然能托管，但家庭局里不如重开简单。
func (r *bjRoom) dropSeat(seat int, reason string) {
	if seat < 0 || seat >= len(r.seats) {
		return
	}
	name := r.nameOf(seat)
	if s := r.seats[seat]; s != nil && s.conn != nil {
		_ = s.conn.Close()
	}
	r.seats = append(r.seats[:seat], r.seats[seat+1:]...)
	r.logf("%s（座位 %d）%s，已移出牌桌", name, seat, reason)

	if r.started && r.view.Phase != bj.PhaseSettle {
		r.abortGame("有人中途离开，本局作废")
		return
	}
	// 结算阶段掉线：把这一局的对局座位数收紧，让剩下的人能继续
	if r.started && len(r.game.Hands) > len(r.seats) {
		r.abortGame("人数变化，本局作废")
		return
	}
	r.pushAll()
}

// applyAction 是房主侧唯一的状态推进入口。
func (r *bjRoom) applyAction(seat int, act bj.Action) error {
	if r.game == nil {
		return fmt.Errorf("对局还没开始")
	}
	if seat >= len(r.game.Hands) {
		return fmt.Errorf("你不在这一局里（等房主 restart 后加入）")
	}
	events, err := r.game.Apply(seat, act)
	if err != nil {
		return err
	}
	r.afterEvents(events)
	r.pushAll()
	return nil
}

func (r *bjRoom) handleHostLocal(line string) (done bool) {
	cmd := parseBjCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("%s", bjHelpText)
		r.render()

	case "start":
		if r.started {
			r.logf("对局已经开始了")
			r.render()
			return false
		}
		r.startGame(false)

	case "restart":
		if r.started && r.view.Phase != bj.PhaseSettle {
			r.logf("本局还没结算，等这一轮打完再 restart")
			r.render()
			return false
		}
		r.startGame(true)

	case "next":
		r.tryNextRound(r.you)

	case "bet", "hit", "stand", "double":
		act, err := bjActionFromCmd(cmd)
		if err != nil {
			r.logf("%s", err.Error())
			r.render()
			return false
		}
		if err := r.applyAction(r.you, act); err != nil {
			r.rejectHint(err, cmd)
		}

	case "chat":
		r.logf("我: %s", cmd.text)
		env, err := protocol.NewEnvelope(protocol.TypeChat, r.nameOf(r.you), protocol.ChatPayload{Message: cmd.text})
		if err == nil {
			r.broadcast(env)
		}
		r.render()

	case "leave":
		env, err := protocol.NewEnvelope(protocol.TypeLeave, r.nameOf(r.you), nil)
		if err == nil {
			r.broadcast(env)
		}
		r.logf("房主离开了牌桌")
		r.render()
		return true

	case "bad":
		r.logf("%s", cmd.text)
		r.render()
	}
	return false
}

// startGame 建一局新的 21 点：座位数 = 当前已经报上名字的人数。
// 中途加入的人（座位号超出对局范围）先旁观，等房主 restart。
func (r *bjRoom) startGame(restart bool) {
	if r.joinedCount() == 0 {
		r.logf("还没有人加入")
		r.render()
		return
	}
	if !r.allJoined() {
		r.logf("还有 %d 个连接没报上名字，等他们 join 完再开", len(r.seats)-r.joinedCount())
		r.render()
		return
	}
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.game = bj.NewGame(r.rng, len(r.seats), r.startChips)
	r.started = true
	r.view = r.game.PublicView()

	env, err := protocol.NewEnvelope(protocol.TypeBjStart, "host", protocol.BjStartPayload{
		Round: r.game.Round, Chips: r.startChips, Seats: len(r.seats),
	})
	if err == nil {
		r.broadcast(env)
	}
	if restart {
		r.logf("重开一局：%d 人，初始筹码 %d", len(r.seats), r.startChips)
	} else {
		r.logf("开局：%d 人，初始筹码 %d，请下注", len(r.seats), r.startChips)
	}
	r.pushAll()
}

func (r *bjRoom) tryNextRound(seat int) {
	if !r.started || r.game == nil {
		r.sendErr(seat, "对局还没开始")
		return
	}
	if r.game.Phase != bj.PhaseSettle {
		r.sendErr(seat, "本局还没结束")
		return
	}
	r.afterEvents(r.game.NextRound())
	r.pushAll()
}

func (r *bjRoom) abortGame(reason string) {
	r.logf("%s（房主可输入 start 重开）", reason)
	r.started = false
	r.game = nil
	r.pushAll()
}
