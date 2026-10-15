package room

import (
	"encoding/json"
	"fmt"

	"intranet-play/internal/bj"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// RunBjClient 以客户端身份加入牌桌。客户端只渲染房主下发的视图，
// 自己不做任何规则判定（点数、输赢都由房主算）。
func RunBjClient(name string, addr string) error {
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
	lobby, err := waitBjLobby(conn)
	if err != nil {
		return err
	}

	r := newBjRoom(addr, lobby.Chips, lobby.Max, lobby.Dealer)
	r.applyLobby(lobby)
	r.logf("已加入牌桌，你是座位 %d，等房主 start", r.you)
	// Run 之前不能 render（Program.Send 会永久阻塞），日志放进初始帧。
	r.refreshSeatInfo()
	r.app = ui.NewBjApp(r.frame())

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

func waitBjLobby(conn *netx.Conn) (protocol.BjLobbyPayload, error) {
	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				return protocol.BjLobbyPayload{}, fmt.Errorf("房主关闭了连接")
			}
			switch env.Type {
			case protocol.TypeBjLobby:
				var p protocol.BjLobbyPayload
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return protocol.BjLobbyPayload{}, fmt.Errorf("大厅数据不合法: %w", err)
				}
				return p, nil
			case protocol.TypeError:
				var p protocol.ErrorPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil && p.Message != "" {
					return protocol.BjLobbyPayload{}, fmt.Errorf("%s", p.Message)
				}
				return protocol.BjLobbyPayload{}, fmt.Errorf("被房主拒绝")
			}
		case <-conn.Done():
			return protocol.BjLobbyPayload{}, fmt.Errorf("连接已关闭")
		}
	}
}

func (r *bjRoom) applyLobby(p protocol.BjLobbyPayload) {
	r.you = p.You
	r.startChips = p.Chips
	r.maxSeats = p.Max
	if p.Dealer != "" {
		r.dealerFlag = p.Dealer
	}
	r.seats = seatsFromBjPayload(p.Seats)
	// 收到大厅消息就说明牌桌当前没有进行中的对局（对局中房主只会广播公共状态）
	r.started = false
	r.view = emptyBjView()
}

func seatsFromBjPayload(list []protocol.BjSeatPayload) []*bjSeat {
	seats := make([]*bjSeat, 0, len(list))
	for _, sp := range list {
		seats = append(seats, &bjSeat{
			name:   sp.Name,
			joined: sp.Name != "",
			online: sp.Online,
			isHost: sp.IsHost,
		})
	}
	return seats
}

func (r *bjRoom) serveClient(conn *netx.Conn, name string) error {
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

func (r *bjRoom) handleClientEnv(env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeBjLobby:
		var p protocol.BjLobbyPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.applyLobby(p)

	case protocol.TypeBjStart:
		var p protocol.BjStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.started = true
		r.startChips = p.Chips
		mode := "虚拟庄家"
		if bj.DealerMode(p.Mode) == bj.DealerRotate {
			mode = "轮流坐庄"
		}
		r.logf("第 %d 轮开始：%d 人，初始筹码 %d，%s", p.Round, p.Seats, p.Chips, mode)

	case protocol.TypeBjPublic:
		var p protocol.BjPublicPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.view = p.View
		r.started = r.view.Phase != ""
		if len(r.seats) != len(p.Seats) {
			r.seats = seatsFromBjPayload(p.Seats)
		} else {
			for i, sp := range p.Seats {
				if i < len(r.seats) && r.seats[i] != nil {
					r.seats[i].name, r.seats[i].online, r.seats[i].isHost = sp.Name, sp.Online, sp.IsHost
				}
			}
		}

	case protocol.TypeBjEvent:
		var p protocol.BjEventPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		if p.Seat == bj.SeatSystem {
			r.logf("%s", p.Text)
		} else {
			r.logf("%s %s", r.nameOf(p.Seat), p.Text)
		}

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
		if r.pendingKind == "bet" {
			msg += "（用法: bet 10）"
		}
		r.logf("被拒绝: %s", msg)
	}
	r.render()
}

func (r *bjRoom) handleClientLocal(line string, conn *netx.Conn, name string) (done bool) {
	cmd := parseBjCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("%s", bjHelpText)
		r.render()

	case "next":
		if !r.started || r.view.Phase != bj.PhaseSettle {
			r.logf("本局还没结束")
			r.render()
			return false
		}
		if env, err := protocol.NewEnvelope(protocol.TypeBjNext, name, nil); err == nil {
			_ = conn.Send(env)
		}

	case "start", "restart":
		r.logf("只有房主能 %s", cmd.kind)
		r.render()

	case "bet", "hit", "stand", "double":
		if !r.started {
			r.logf("对局还没开始")
			r.render()
			return false
		}
		act, err := bjActionFromCmd(cmd)
		if err != nil {
			r.logf("%s", err.Error())
			r.render()
			return false
		}
		env, err := protocol.NewEnvelope(protocol.TypeBjAction, name, protocol.BjActionPayload{Action: act})
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
		r.logf("已退出牌桌")
		r.render()
		return true

	case "bad":
		r.logf("%s", cmd.text)
		r.render()
	}
	return false
}
