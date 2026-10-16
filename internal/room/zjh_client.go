package room

import (
	"encoding/json"
	"fmt"
	"strings"

	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
	"intranet-play/internal/zjh"
)

// RunZjhClient 以客户端身份加入牌桌。客户端只渲染房主下发的视图：
// 没看牌之前，服务端根本不会把牌发给它。
func RunZjhClient(name string, addr string) error {
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
	lobby, err := waitZjhLobby(conn)
	if err != nil {
		return err
	}

	r := newZjhRoom(addr, lobby.Chips, lobby.Ante, lobby.MaxStake, lobby.MaxRounds, lobby.Max)
	r.applyLobby(lobby)
	r.logf("已加入牌桌，你是座位 %d，等房主 start", r.you)
	// Run 之前不能 render（Program.Send 会永久阻塞），日志放进初始帧。
	r.refreshSeatInfo()
	r.app = ui.NewZjhApp(r.frame())

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

func waitZjhLobby(conn *netx.Conn) (protocol.ZjhLobbyPayload, error) {
	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				return protocol.ZjhLobbyPayload{}, fmt.Errorf("房主关闭了连接")
			}
			switch env.Type {
			case protocol.TypeZjhLobby:
				var p protocol.ZjhLobbyPayload
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return protocol.ZjhLobbyPayload{}, fmt.Errorf("大厅数据不合法: %w", err)
				}
				return p, nil
			case protocol.TypeError:
				var p protocol.ErrorPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil && p.Message != "" {
					return protocol.ZjhLobbyPayload{}, fmt.Errorf("%s", p.Message)
				}
				return protocol.ZjhLobbyPayload{}, fmt.Errorf("被房主拒绝")
			}
		case <-conn.Done():
			return protocol.ZjhLobbyPayload{}, fmt.Errorf("连接已关闭")
		}
	}
}

func (r *zjhRoom) applyLobby(p protocol.ZjhLobbyPayload) {
	r.you = p.You
	r.startChips = p.Chips
	r.ante = p.Ante
	r.maxStake = p.MaxStake
	r.maxRounds = p.MaxRounds
	r.maxSeats = p.Max
	r.seats = zjhSeatsFromPayload(p.Seats)
	// 收到大厅消息说明牌桌当前没有进行中的对局
	r.started = false
	r.view = emptyZjhView()
	r.priv = zjh.PrivateView{}
	r.peek = ""
}

func zjhSeatsFromPayload(list []protocol.ZjhSeatPayload) []*zjhSeat {
	seats := make([]*zjhSeat, 0, len(list))
	for _, sp := range list {
		seats = append(seats, &zjhSeat{
			name:   sp.Name,
			joined: sp.Name != "",
			online: sp.Online,
			isHost: sp.IsHost,
		})
	}
	return seats
}

func (r *zjhRoom) serveClient(conn *netx.Conn, name string) error {
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

func (r *zjhRoom) handleClientEnv(env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeZjhLobby:
		var p protocol.ZjhLobbyPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.applyLobby(p)

	case protocol.TypeZjhStart:
		var p protocol.ZjhStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.started = true
		r.ante = p.Ante
		r.logf("第 %d 局开始：%d 人，底注 %d", p.Round, p.Seats, p.Ante)

	case protocol.TypeZjhPublic:
		var p protocol.ZjhPublicPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		r.view = p.View
		r.started = r.view.Phase != ""
		if len(r.seats) != len(p.Seats) {
			r.seats = zjhSeatsFromPayload(p.Seats)
		} else {
			for i, sp := range p.Seats {
				if i < len(r.seats) && r.seats[i] != nil {
					r.seats[i].name, r.seats[i].online, r.seats[i].isHost = sp.Name, sp.Online, sp.IsHost
				}
			}
		}

	case protocol.TypeZjhHand:
		var p protocol.ZjhHandPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		// 房主只会发我自己的手牌；没看牌时引擎给的是空数组
		if p.View.Seat != r.you {
			return
		}
		r.priv = p.View

	case protocol.TypeZjhPeek:
		var p protocol.ZjhPeekPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		if p.Peek.A == r.you || p.Peek.B == r.you {
			r.peek = r.peekText(p.Peek)
		}

	case protocol.TypeZjhEvent:
		var p protocol.ZjhEventPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		if p.Seat == zjh.SeatSystem {
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
		if r.pendingKind == "raise" {
			msg += "（用法: raise 6）"
		}
		r.logf("被拒绝: %s", msg)
	}
	r.render()
}

func (r *zjhRoom) handleClientLocal(line string, conn *netx.Conn, name string) (done bool) {
	cmd := parseZjhCmd(line)
	switch cmd.kind {
	case "none":
		return false

	case "help":
		r.logf("%s", zjhHelpText)
		r.render()

	case "next":
		if !r.started || r.view.Phase != zjh.PhaseSettle {
			r.logf("本局还没结束")
			r.render()
			return false
		}
		if env, err := protocol.NewEnvelope(protocol.TypeZjhNext, name, nil); err == nil {
			_ = conn.Send(env)
		}

	case "start", "restart":
		r.logf("只有房主能 %s", cmd.kind)
		r.render()

	case "look", "call", "raise", "fold", "compare":
		if !r.started {
			r.logf("对局还没开始")
			r.render()
			return false
		}
		act, err := zjhActionFromCmd(cmd)
		if err != nil {
			r.logf("%s", err.Error())
			r.render()
			return false
		}
		env, err := protocol.NewEnvelope(protocol.TypeZjhAction, name, protocol.ZjhActionPayload{Action: act})
		if err != nil {
			return false
		}
		r.pendingKind = cmd.kind
		if err := conn.Send(env); err != nil {
			r.logf("发送失败: %s", strings.TrimSpace(err.Error()))
			r.render()
		}

	case "chat":
		// 聊天不做服务端回声（房主只转发给别人，免得发送方看到两条），
		// 所以这里必须自己记一条，否则说话的人看不到自己说了什么。
		r.logf("我: %s", cmd.text)
		r.render()
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
