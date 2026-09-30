package room

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"intranet-play/internal/ddz"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// ddzInbound 是汇聚到主循环的一条消息。所有连接都通过它进入唯一的状态机。
type ddzInbound struct {
	seat    int
	env     protocol.Envelope
	offline bool
}

// RunDdzHost 开一个 3 人斗地主房间：本进程占座位 0，再接受两条客户端连接。
func RunDdzHost(name string, port, base, rounds int) error {
	ln, err := netx.Listen(netx.LocalAddr(port))
	if err != nil {
		return err
	}

	r := newDdzHostRoom(fmt.Sprintf("%d", port), name, base, rounds)
	r.logf("房间已开启（端口 %d），等待另外 2 位玩家加入", port)
	r.logf("命令: help 查看用法，start 开始对局")
	// 注意：必须在 app.Run() 之前把界面建好并且不调用 render()。
	// render 最终会走 bubbletea 的 Program.Send，而它在 Run 之前是永久阻塞的，
	// 在主 goroutine 上调用会直接 deadlock。启动信息直接放进初始帧。
	r.app = ui.NewDdzApp(r.frame())

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

// serveHost 是房主侧的单线程主循环：accept、网络消息、本地命令都汇聚到这里。
func (r *ddzRoom) serveHost(ln net.Listener) error {
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

	hub := make(chan ddzInbound, 32)
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

// acceptConn 给新连接分配座位；座位满了就明确拒绝。
func (r *ddzRoom) acceptConn(conn *netx.Conn, hub chan<- ddzInbound) {
	seat := r.freeSeat()
	if seat < 0 {
		env, err := protocol.NewEnvelope(protocol.TypeError, "room", protocol.ErrorPayload{
			Message: "room full: 3 人局已满",
		})
		if err == nil {
			_ = conn.Send(env)
		}
		_ = conn.Close()
		r.logf("有第 4 个连接尝试加入，已拒绝")
		r.render()
		return
	}
	r.seats[seat] = &ddzSeat{conn: conn, online: true}
	r.logf("有连接进入座位 %d，等待名字", seat)
	r.render()

	go func() {
		for env := range conn.Inbox() {
			hub <- ddzInbound{seat: seat, env: env}
		}
		hub <- ddzInbound{seat: seat, offline: true}
	}()
}

func (r *ddzRoom) handleInbound(in ddzInbound) {
	s := r.seats[in.seat]
	if s == nil {
		return
	}

	if in.offline {
		r.dropSeat(in.seat, "断线")
		return
	}

	switch in.env.Type {
	case protocol.TypeJoin:
		var p protocol.JoinPayload
		if err := json.Unmarshal(in.env.Payload, &p); err != nil || strings.TrimSpace(p.Name) == "" {
			r.sendErr(s, "名字不合法")
			return
		}
		s.name = strings.TrimSpace(p.Name)
		s.joined = true
		s.ready = true
		r.logf("%s 加入房间，坐座位 %d", s.name, in.seat)
		if r.allJoined() && !r.started {
			r.logf("三人到齐，房主输入 start 开始对局")
		}
		r.pushAll()

	case protocol.TypeDdzReady:
		s.ready = true
		r.pushAll()

	case protocol.TypeDdzNext:
		r.tryNextRound(s)

	case protocol.TypeDdzAction:
		var p protocol.DdzActionPayload
		if err := json.Unmarshal(in.env.Payload, &p); err != nil {
			r.sendErr(s, "动作格式错误")
			return
		}
		if err := r.applyAction(in.seat, p.Action); err != nil {
			r.sendErr(s, err.Error())
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
		r.logf("%s: %s", r.nameOf(in.seat), msg)
		r.relayChat(in.seat, msg)
		r.render()

	case protocol.TypeLeave:
		r.dropSeat(in.seat, "离开")
	}
}

// dropSeat 处理断线/离开：3 人局没法托管，直接作废本局回大厅。
func (r *ddzRoom) dropSeat(seat int, reason string) {
	s := r.seats[seat]
	if s == nil {
		return
	}
	name := r.nameOf(seat)
	s.online = false
	s.conn = nil
	s.ready = false
	r.logf("%s（座位 %d）%s", name, seat, reason)
	if r.started && r.view.Phase != ddz.PhaseRoundEnd {
		r.abortRound("有人中途掉线，本局作废")
		return
	}
	r.pushAll()
}

// applyAction 是房主侧唯一的状态推进入口。
func (r *ddzRoom) applyAction(seat int, act ddz.Action) error {
	if r.game == nil {
		return fmt.Errorf("对局还没开始")
	}
	events, err := r.game.Apply(seat, act)
	if err != nil {
		return err
	}
	for _, e := range events {
		if e.Seat == ddz.SeatSystem {
			r.logf("%s", e.Text)
		} else {
			r.logf("%s %s", r.nameOf(e.Seat), e.Text)
		}
		r.broadcastEvent(e)
	}
	r.pushAll()
	return nil
}

func (r *ddzRoom) handleHostLocal(line string) (done bool) {
	cmd := parseDdzCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("命令: play 1 2 / play 1-3 | bid 0-3 | pass | start | next | chat TEXT | leave")
		r.render()

	case "start":
		if r.started {
			r.logf("对局已经开始了")
			r.render()
			return false
		}
		if !r.allJoined() {
			r.logf("还差 %d 位玩家，等他们连上再 start", r.missingCount())
			r.render()
			return false
		}
		r.startMatch()

	case "next":
		r.tryNextRound(nil)

	case "ready":
		r.logf("你是房主，输入 start 即可开始")
		r.render()

	case "play", "bid", "pass":
		if !r.started {
			r.logf("对局还没开始")
			r.render()
			return false
		}
		act, err := actionFromCmd(cmd, r.hand)
		if err != nil {
			r.logf("输入有问题: %s", err.Error())
			r.render()
			return false
		}
		if err := r.applyAction(r.you, act); err != nil {
			r.logf("操作被拒: %s", err.Error())
			r.render()
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
		r.logf("房主离开了房间")
		r.render()
		return true

	case "bad":
		r.logf("%s", cmd.text)
		r.render()
	}
	return false
}

func (r *ddzRoom) startMatch() {
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.started = true
	r.game = ddz.NewGame(r.rng, 0, r.base)
	r.revealReset()
	r.view = r.game.PublicView()

	if env, err := protocol.NewEnvelope(protocol.TypeDdzStart, "host", protocol.DdzStartPayload{
		Round: r.game.Round, Rounds: r.rounds, Base: r.base,
	}); err == nil {
		r.broadcast(env)
	}
	if r.rounds > 0 {
		r.logf("第 %d 局开始，底分 %d，共 %d 局", r.game.Round, r.base, r.rounds)
	} else {
		r.logf("第 %d 局开始，底分 %d，不限局数", r.game.Round, r.base)
	}
	r.pushAll()
}

// tryNextRound 开下一局或整场结算。reply 非 nil 时表示由某个客户端请求触发。
func (r *ddzRoom) tryNextRound(reply *ddzSeat) {
	if !r.started || r.game == nil {
		if reply != nil {
			r.sendErr(reply, "对局还没开始")
		} else {
			r.logf("对局还没开始")
			r.render()
		}
		return
	}
	if r.game.Phase != ddz.PhaseRoundEnd {
		if reply != nil {
			r.sendErr(reply, "本局还没结束")
		} else {
			r.logf("本局还没结束")
			r.render()
		}
		return
	}
	if r.rounds > 0 && r.game.Round >= r.rounds {
		r.endMatch()
		return
	}
	r.game.NextRound()
	r.revealReset()
	r.logf("第 %d 局开始", r.game.Round)
	r.pushAll()
}

func (r *ddzRoom) abortRound(reason string) {
	r.logf("%s（房主可输入 start 重开）", reason)
	r.started = false
	r.game = nil
	r.pushAll()
}

func (r *ddzRoom) endMatch() {
	scores := r.game.Scores
	winner := 0
	for i := 1; i < ddz.Seats; i++ {
		if scores[i] > scores[winner] {
			winner = i
		}
	}
	env, err := protocol.NewEnvelope(protocol.TypeDdzMatchEnd, "host", protocol.DdzMatchEndPayload{
		Seats: r.seatPayloads(), Scores: scores[:], Winner: winner,
	})
	if err == nil {
		r.broadcast(env)
	}
	r.logf("整场结束：%s，总比分 %v，输入 start 再来一场", rankText(scores[:]), scores)
	r.started = false
	r.game = nil
	r.pushAll()
}
