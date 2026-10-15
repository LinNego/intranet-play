package room

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
	"intranet-play/internal/zjh"
)

// zjhInbound 是汇聚到主循环的一条消息。带上 conn 而不是座位号：
// 座位会在有人掉线时压缩，靠连接反查座位才不会张冠李戴。
type zjhInbound struct {
	conn    *netx.Conn
	env     protocol.Envelope
	offline bool
}

// RunZjhHost 开一个 2~6 人炸金花房间：本进程占座位 0，其余人 join 进来。
func RunZjhHost(name string, port, chips, ante, maxStake, maxRounds, maxSeats int) error {
	ln, err := netx.Listen(netx.LocalAddr(port))
	if err != nil {
		return err
	}

	r := newZjhHostRoom(fmt.Sprintf("%d", port), name, chips, ante, maxStake, maxRounds, maxSeats)
	r.logf("房间已开启（端口 %d，最多 %d 人，筹码 %d，底注 %d）", port, maxSeats, r.startChips, r.ante)
	r.logf("看牌才知道自己的牌；闷牌跟注半价，闷牌平局占优")
	r.logf("等人齐后输入 start；轮到你时 call 跟注 / raise N 加注 / compare N 比牌 / fold 弃牌")
	// Run 之前不能 render（Program.Send 会永久阻塞），日志直接放进初始帧。
	r.app = ui.NewZjhApp(r.frame())

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

func (r *zjhRoom) serveHost(ln net.Listener) error {
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

	hub := make(chan zjhInbound, 32)
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

func (r *zjhRoom) acceptConn(conn *netx.Conn, hub chan<- zjhInbound) {
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
	r.seats = append(r.seats, &zjhSeat{conn: conn, online: true})
	r.logf("有连接进入座位 %d，等待名字", seat)
	r.pushAll()

	go func() {
		for env := range conn.Inbox() {
			hub <- zjhInbound{conn: conn, env: env}
		}
		hub <- zjhInbound{conn: conn, offline: true}
	}()
}

func (r *zjhRoom) handleInbound(in zjhInbound) {
	seat := r.seatOf(in.conn)
	if seat < 0 {
		return
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

	case protocol.TypeZjhNext:
		r.tryNextRound(seat)

	case protocol.TypeZjhAction:
		var p protocol.ZjhActionPayload
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

// dropSeat 摘掉座位并压缩，保持座位号连续。对局中有人掉线就作废本局回大厅：
// 他的牌是暗的，别人没法替他决策。
func (r *zjhRoom) dropSeat(seat int, reason string) {
	if seat < 0 || seat >= len(r.seats) {
		return
	}
	name := r.nameOf(seat)
	if s := r.seats[seat]; s != nil && s.conn != nil {
		_ = s.conn.Close()
	}
	r.seats = append(r.seats[:seat], r.seats[seat+1:]...)
	r.logf("%s（座位 %d）%s，已移出牌桌", name, seat, reason)

	if r.started && r.view.Phase != zjh.PhaseSettle && r.view.Phase != zjh.PhaseOver {
		r.abortGame("有人中途离开，本局作废")
		return
	}
	if r.started && len(r.game.Players) > len(r.seats) {
		r.abortGame("人数变化，本局作废")
		return
	}
	r.pushAll()
}

func (r *zjhRoom) applyAction(seat int, act zjh.Action) error {
	if r.game == nil {
		return fmt.Errorf("对局还没开始")
	}
	if seat >= len(r.game.Players) {
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

func (r *zjhRoom) handleHostLocal(line string) (done bool) {
	cmd := parseZjhCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("%s", zjhHelpText)
		r.render()

	case "start":
		if r.started && r.view.Phase != zjh.PhaseOver {
			r.logf("对局已经开始了")
			r.render()
			return false
		}
		r.startGame(false)

	case "restart":
		if r.started && r.view.Phase != zjh.PhaseSettle && r.view.Phase != zjh.PhaseOver {
			r.logf("本局还没结算，等这一局打完再 restart")
			r.render()
			return false
		}
		r.startGame(true)

	case "next":
		r.tryNextRound(r.you)

	case "look", "call", "raise", "fold", "compare":
		act, err := zjhActionFromCmd(cmd)
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

// startGame 发牌开一局：座位数 = 当前连接数。
func (r *zjhRoom) startGame(restart bool) {
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
	if len(r.seats) < 2 {
		r.logf("至少要 2 个人才能开炸金花")
		r.render()
		return
	}
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.game = zjh.NewGame(r.rng, len(r.seats), r.startChips, r.ante, r.maxStake, r.maxRounds)
	r.started = true
	r.peek = ""
	r.view = r.game.PublicView()

	env, err := protocol.NewEnvelope(protocol.TypeZjhStart, "host", protocol.ZjhStartPayload{
		Round: r.game.Round, Ante: r.ante, Seats: len(r.seats),
	})
	if err == nil {
		r.broadcast(env)
	}
	if restart {
		r.logf("重开一桌：%d 人，筹码 %d，底注 %d", len(r.seats), r.startChips, r.ante)
	} else {
		r.logf("开局：%d 人，筹码 %d，底注 %d，请按顺序行动", len(r.seats), r.startChips, r.ante)
	}
	r.afterEvents([]zjh.Event{{Seat: zjh.SeatSystem, Text: fmt.Sprintf("奖池 %d", r.game.Pot)}})
	r.pushAll()
}

func (r *zjhRoom) tryNextRound(seat int) {
	if !r.started || r.game == nil {
		r.sendErr(seat, "对局还没开始")
		return
	}
	if r.game.Phase != zjh.PhaseSettle {
		r.sendErr(seat, "本局还没结束")
		return
	}
	r.peek = ""
	r.afterEvents(r.game.NextRound())
	r.pushAll()
}

func (r *zjhRoom) abortGame(reason string) {
	r.logf("%s（房主可输入 start 重开）", reason)
	r.started = false
	r.game = nil
	r.pushAll()
}
