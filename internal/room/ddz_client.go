package room

import (
	"encoding/json"
	"fmt"

	"intranet-play/internal/ddz"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// RunDdzClient 以客户端身份加入房间。客户端只渲染房主下发的视图，
// 自己不做任何规则判定，手牌也只从 DDZ_HAND 单播里拿。
func RunDdzClient(name string, addr string) error {
	conn, err := netx.Connect(addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	join, err := protocol.NewEnvelope(protocol.TypeJoin, name, protocol.JoinPayload{Name: name})
	if err != nil {
		return err
	}
	if err := conn.Send(join); err != nil {
		return err
	}

	fmt.Println("已连接，等待房主分配座位...")
	lobby, err := waitDdzLobby(conn)
	if err != nil {
		return err
	}

	r := newDdzRoom(addr, lobby.Base, lobby.Rounds)
	r.applyLobby(lobby)
	r.logf("已加入房间，你是座位 %d，等待房主 start", r.you)
	// 同房主：Run 之前不能 render（Program.Send 会永久阻塞），日志放进初始帧。
	r.refreshSeatInfo()
	r.app = ui.NewDdzApp(r.frame())

	if ready, err := protocol.NewEnvelope(protocol.TypeDdzReady, name, nil); err == nil {
		_ = conn.Send(ready)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- r.serveClient(conn, name) }()

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

func waitDdzLobby(conn *netx.Conn) (protocol.DdzLobbyPayload, error) {
	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				return protocol.DdzLobbyPayload{}, fmt.Errorf("房主关闭了连接")
			}
			switch env.Type {
			case protocol.TypeDdzLobby:
				var p protocol.DdzLobbyPayload
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return protocol.DdzLobbyPayload{}, fmt.Errorf("大厅数据不合法: %w", err)
				}
				return p, nil
			case protocol.TypeError:
				var p protocol.ErrorPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil && p.Message != "" {
					return protocol.DdzLobbyPayload{}, fmt.Errorf("%s", p.Message)
				}
				return protocol.DdzLobbyPayload{}, fmt.Errorf("被房主拒绝")
			}
		case <-conn.Done():
			return protocol.DdzLobbyPayload{}, fmt.Errorf("连接已关闭")
		}
	}
}

func (r *ddzRoom) applyLobby(p protocol.DdzLobbyPayload) {
	r.you = p.You
	r.base = p.Base
	r.rounds = p.Rounds
	r.seatsFromPayload(p.Seats)
	r.started = false
	r.view = emptyView()
	r.hand = nil
	r.revealReset()
}

func (r *ddzRoom) seatsFromPayload(list []protocol.DdzSeatPayload) {
	for i := 0; i < ddz.Seats; i++ {
		r.seats[i] = nil
	}
	for _, sp := range list {
		if sp.Seat < 0 || sp.Seat >= ddz.Seats {
			continue
		}
		r.seats[sp.Seat] = &ddzSeat{
			name:   sp.Name,
			joined: sp.Name != "",
			online: sp.Online,
			ready:  sp.Ready,
			isHost: sp.IsHost,
		}
	}
}

func (r *ddzRoom) serveClient(conn *netx.Conn, name string) error {
	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				r.logf("与房主的连接已断开")
				r.render()
				return nil
			}
			r.handleClientEnv(env)

		case line, ok := <-r.app.Commands():
			if !ok {
				return nil
			}
			if r.handleClientLocal(line, conn, name) {
				return nil
			}

		case <-conn.Done():
			r.logf("连接已关闭")
			r.render()
			return nil

		case <-r.app.Done():
			return nil
		}
	}
}

func (r *ddzRoom) handleClientEnv(env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeDdzLobby:
		var p protocol.DdzLobbyPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.applyLobby(p)

	case protocol.TypeDdzStart:
		var p protocol.DdzStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.started = true
		r.base = p.Base
		r.rounds = p.Rounds
		r.logf("第 %d 局开始，底分 %d", p.Round, p.Base)

	case protocol.TypeDdzPublic:
		var p protocol.DdzPublicPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.view = p.View
		if p.Rounds > 0 {
			r.rounds = p.Rounds
		}
		r.seatsFromPayload(p.Seats)
		r.started = r.view.Phase != ""

	case protocol.TypeDdzHand:
		var p protocol.DdzHandPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.hand = p.View.Hand

	case protocol.TypeDdzEvent:
		var p protocol.DdzEventPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		if p.Seat == ddz.SeatSystem {
			r.logf("%s", p.Text)
		} else {
			r.logf("%s %s", r.nameOf(p.Seat), p.Text)
		}

	case protocol.TypeDdzRoundEnd:
		var p protocol.DdzRoundEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.view = p.View
		r.reveal = p.Reveal

	case protocol.TypeDdzMatchEnd:
		var p protocol.DdzMatchEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.seatsFromPayload(p.Seats)
		r.logf("整场结束：%s，总比分 %v", rankText(p.Scores), p.Scores)
		r.started = false
		r.view = emptyView()
		r.hand = nil
		r.revealReset()

	case protocol.TypeChat:
		var p protocol.ChatPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.logf("%s: %s", env.From, p.Message)

	case protocol.TypeError:
		var p protocol.ErrorPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		msg := p.Message
		if r.pendingKind == "play" {
			msg += "（写法: play 55 / play 34567 / play #3 #7）"
		}
		r.logf("被拒绝: %s", msg)
	}
	r.render()
}

func (r *ddzRoom) handleClientLocal(line string, conn *netx.Conn, name string) (done bool) {
	cmd := parseDdzCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("命令: play 55 / play 34567（按牌面）| play #3 #7（按编号）| bid 0-3 | pass | hint | next | chat TEXT | leave")
		r.render()

	case "hint":
		r.showHints()

	case "ready":
		if env, err := protocol.NewEnvelope(protocol.TypeDdzReady, name, nil); err == nil {
			_ = conn.Send(env)
		}
		r.logf("已发送就绪")
		r.render()

	case "next":
		if !r.started || r.view.Phase != ddz.PhaseRoundEnd {
			r.logf("本局还没结束")
			r.render()
			return false
		}
		if env, err := protocol.NewEnvelope(protocol.TypeDdzNext, name, nil); err == nil {
			_ = conn.Send(env)
		}

	case "start":
		r.logf("只有房主能 start")
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
		env, err := protocol.NewEnvelope(protocol.TypeDdzAction, name, protocol.DdzActionPayload{Action: act})
		if err != nil {
			return false
		}
		r.pendingKind = cmd.kind
		if err := conn.Send(env); err != nil {
			r.logf("发送失败: %s", err.Error())
			r.render()
		}

	case "chat":
		env, err := protocol.NewEnvelope(protocol.TypeChat, name, protocol.ChatPayload{Message: cmd.text})
		if err == nil {
			_ = conn.Send(env)
		}

	case "leave":
		if env, err := protocol.NewEnvelope(protocol.TypeLeave, name, nil); err == nil {
			_ = conn.Send(env)
		}
		r.logf("已退出房间")
		r.render()
		return true

	case "bad":
		r.logf("%s", cmd.text)
		r.render()
	}
	return false
}
