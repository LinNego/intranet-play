package room

import (
	"encoding/json"
	"fmt"
	"intranet-play/internal/game"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
)

func RunClient(name string, addr string) error {
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

	fmt.Println("connected, waiting for host setup...")

	room, err := waitClientWelcome(conn, name)
	if err != nil {
		return err
	}
	room.attachUI(2)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runClientLoop(room, conn, name)
	}()

	uiErr := room.app.Run()
	room.app.Quit()

	select {
	case loopErr := <-errCh:
		if loopErr != nil {
			return loopErr
		}
	default:
	}
	return uiErr
}

func waitClientWelcome(conn *netx.Conn, name string) (*Room, error) {
	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				return nil, fmt.Errorf("host disconnected")
			}
			if env.Type != protocol.TypeWelcome {
				continue
			}
			var welcomePayload protocol.WelcomePayload
			if err := json.Unmarshal(env.Payload, &welcomePayload); err != nil {
				return nil, err
			}
			size := welcomePayload.Size
			if size <= 0 {
				size = 15
			}
			return &Room{
				board:   game.NewBoard(size),
				players: [2]string{env.From, name},
				turn:    1,
				status:  game.GameStatusPending,
				lastMsg: fmt.Sprintf("joined peer=%s n=%d", welcomePayload.Opponent, size),
			}, nil
		case <-conn.Done():
			return nil, fmt.Errorf("connection closed")
		}
	}
}

func runClientLoop(room *Room, conn *netx.Conn, name string) error {
	defer room.app.Quit()
	room.render()

	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				room.hint("host disconnected")
				return fmt.Errorf("host disconnected")
			}
			done, err := handleClientRecv(env, room, name)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case line, ok := <-room.app.Commands():
			if !ok {
				return nil
			}
			done, err := handleClientStd(parseLineCmd(line), room, conn, name)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-conn.Done():
			room.hint("connection closed")
			return fmt.Errorf("connection closed")
		case <-room.app.Done():
			return nil
		}
	}
}

func handleClientRecv(env protocol.Envelope, room *Room, name string) (done bool, err error) {
	switch env.Type {
	case protocol.TypeState:
		var statePayload protocol.StatePayload
		if err := json.Unmarshal(env.Payload, &statePayload); err != nil {
			return false, err
		}
		if statePayload.Board == nil {
			return false, fmt.Errorf("empty board in state")
		}
		room.board = statePayload.Board
		room.status = statePayload.Status
		room.turn = statePayload.Turn
		room.board.Winner = statePayload.Winner
		if room.lastMsg == "" {
			room.lastMsg = "matrix updated"
		}
		room.render()
		return false, nil

	case protocol.TypeChat:
		var chatPayload protocol.ChatPayload
		if err := json.Unmarshal(env.Payload, &chatPayload); err != nil {
			return false, nil
		}
		room.hint(fmt.Sprintf("%s: %s", env.From, chatPayload.Message))
		return false, nil

	case protocol.TypeError:
		msg := "error"
		var errPayload protocol.ErrorPayload
		if err := json.Unmarshal(env.Payload, &errPayload); err == nil && errPayload.Message != "" {
			msg = errPayload.Message
		}
		room.hint("err: " + msg)
		return false, nil

	case protocol.TypeLeave:
		room.hint(fmt.Sprintf("%s left", env.From))
		return true, nil

	case protocol.TypeResign:
		room.hint(fmt.Sprintf("%s yield", env.From))
		return false, nil
	}
	return false, nil
}

func handleClientStd(cmd localCmd, room *Room, conn *netx.Conn, name string) (done bool, err error) {
	switch cmd.kind {
	case "hint":
		room.hint(cmd.message)
		return false, nil

	case "place":
		if room.status != game.GameStatusContinue {
			room.hint("session done; type restart")
			return false, nil
		}
		if room.turn != 2 {
			room.hint("not your turn")
			return false, nil
		}
		env, err := protocol.NewEnvelope(protocol.TypePlace, name, protocol.PlacePayload{
			Pos: protocol.Pos{X: cmd.x, Y: cmd.y},
		})
		if err != nil {
			return false, err
		}
		room.lastMsg = fmt.Sprintf("sent write (%d,%d)", cmd.x, cmd.y)
		room.render()
		return false, conn.Send(env)

	case "chat":
		room.hint("me: " + cmd.message)
		env, err := protocol.NewEnvelope(protocol.TypeChat, name, protocol.ChatPayload{Message: cmd.message})
		if err != nil {
			return false, err
		}
		return false, conn.Send(env)

	case "leave":
		env, err := protocol.NewEnvelope(protocol.TypeLeave, name, nil)
		if err != nil {
			return false, err
		}
		_ = conn.Send(env)
		room.hint("local left")
		return true, nil

	case "resign":
		if room.status != game.GameStatusContinue {
			room.hint("session not active")
			return false, nil
		}
		env, err := protocol.NewEnvelope(protocol.TypeResign, name, nil)
		if err != nil {
			return false, err
		}
		room.lastMsg = "yield requested"
		room.render()
		return false, conn.Send(env)

	case "restart":
		env, err := protocol.NewEnvelope(protocol.TypeRestart, name, nil)
		if err != nil {
			return false, err
		}
		room.lastMsg = "reset requested"
		room.render()
		return false, conn.Send(env)
	}
	return false, nil
}
