package room

import (
	"bufio"
	"encoding/json"
	"fmt"
	"intranet-play/internal/game"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"os"
	"strconv"
)

func RunHost(name string, port int, boardSize int) error {
	fmt.Println("host", name, "listen", port)
	fmt.Println("waiting for peer...")
	ln, conn, err := netx.ListenAndAccept(netx.LocalAddr(port))
	if err != nil {
		return err
	}
	defer ln.Close()
	defer conn.Close()

	env, ok := <-conn.Inbox()
	if !ok {
		return fmt.Errorf("peer disconnected")
	}
	if env.Type != protocol.TypeJoin {
		return fmt.Errorf("unexpected message: %s", env.Type)
	}

	var joinPayload protocol.JoinPayload
	if err := json.Unmarshal(env.Payload, &joinPayload); err != nil {
		return fmt.Errorf("invalid join payload: %w", err)
	}
	opponent := joinPayload.Name
	if opponent == "" {
		return fmt.Errorf("empty peer name")
	}
	fmt.Println("peer joined:", opponent)

	room := NewRoom(name, boardSize)
	room.players[1] = opponent
	if err := presetRoom(room); err != nil {
		return err
	}

	welcome, err := protocol.NewEnvelope(protocol.TypeWelcome, name, protocol.WelcomePayload{
		Size:     room.board.Size,
		Opponent: name,
	})
	if err != nil {
		return err
	}
	if err := conn.Send(welcome); err != nil {
		return fmt.Errorf("send welcome failed: %w", err)
	}

	room.lastMsg = "session start"
	room.attachUI(1)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runHostLoop(room, conn, name)
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

func runHostLoop(room *Room, conn *netx.Conn, hostName string) error {
	defer room.app.Quit()

	if err := broadcastState(room, conn, hostName); err != nil {
		return err
	}

	for {
		select {
		case env, ok := <-conn.Inbox():
			if !ok {
				room.hint("peer disconnected")
				return nil
			}
			done, err := handleHostRecv(env, room, conn, hostName)
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
			done, err := handleHostStd(parseLineCmd(line), room, conn, hostName)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-conn.Done():
			room.hint("connection closed")
			return nil
		case <-room.app.Done():
			return nil
		}
	}
}

func presetRoom(room *Room) error {
	sc := bufio.NewScanner(os.Stdin)

	fmt.Printf("matrix size (10-20) [default %d]:\n", room.board.Size)
	if !sc.Scan() {
		return fmt.Errorf("failed to read size")
	}
	text := sc.Text()
	if text != "" {
		size, err := strconv.Atoi(text)
		if err != nil || size < 10 || size > 20 {
			return fmt.Errorf("invalid size: %q", text)
		}
		room.board = game.NewBoard(size)
	}

	fmt.Println("first slot (1: host, 2: guest) [default 1]:")
	if !sc.Scan() {
		return fmt.Errorf("failed to read first slot")
	}
	text = sc.Text()
	turn := 1
	if text != "" {
		var err error
		turn, err = strconv.Atoi(text)
		if err != nil || (turn != 1 && turn != 2) {
			return fmt.Errorf("invalid first slot: %q", text)
		}
	}
	room.turn = turn
	room.status = game.GameStatusContinue
	return nil
}

func handleHostRecv(env protocol.Envelope, room *Room, conn *netx.Conn, hostName string) (done bool, err error) {
	switch env.Type {
	case protocol.TypePlace:
		if room.status != game.GameStatusContinue {
			_ = sendErr(conn, hostName, "session not active")
			return false, nil
		}
		if room.turn != 2 {
			_ = sendErr(conn, hostName, "not your turn")
			return false, nil
		}
		var placePayload protocol.PlacePayload
		if err := json.Unmarshal(env.Payload, &placePayload); err != nil {
			_ = sendErr(conn, hostName, "invalid place payload")
			return false, nil
		}
		st, err := room.board.Place(2, placePayload.Pos.X, placePayload.Pos.Y)
		if err != nil {
			_ = sendErr(conn, hostName, err.Error())
			return false, nil
		}
		room.status = st
		if st == game.GameStatusContinue {
			room.turn = 1
		}
		room.lastMsg = fmt.Sprintf("%s write (%d,%d)", env.From, placePayload.Pos.X, placePayload.Pos.Y)
		return false, broadcastState(room, conn, hostName)

	case protocol.TypeChat:
		var chatPayload protocol.ChatPayload
		if err := json.Unmarshal(env.Payload, &chatPayload); err != nil {
			_ = sendErr(conn, hostName, "invalid chat payload")
			return false, nil
		}
		room.hint(fmt.Sprintf("%s: %s", env.From, chatPayload.Message))
		return false, nil

	case protocol.TypeLeave:
		room.hint(fmt.Sprintf("%s left", env.From))
		return true, nil

	case protocol.TypeResign:
		if room.status != game.GameStatusContinue {
			return false, nil
		}
		room.status = game.GameStatusWin
		room.board.Winner = 1
		room.lastMsg = fmt.Sprintf("%s yield", env.From)
		return false, broadcastState(room, conn, hostName)

	case protocol.TypeRestart:
		room.board = game.NewBoard(room.board.Size)
		room.turn = 1
		room.status = game.GameStatusContinue
		room.board.Winner = 0
		room.lastMsg = "session reset"
		return false, broadcastState(room, conn, hostName)
	}
	return false, nil
}

func handleHostStd(cmd localCmd, room *Room, conn *netx.Conn, hostName string) (done bool, err error) {
	switch cmd.kind {
	case "hint":
		room.hint(cmd.message)
		return false, nil

	case "place":
		if room.status != game.GameStatusContinue {
			room.hint("session done; type restart")
			return false, nil
		}
		if room.turn != 1 {
			room.hint("not your turn")
			return false, nil
		}
		st, err := room.board.Place(1, cmd.x, cmd.y)
		if err != nil {
			room.hint("write failed: " + err.Error())
			return false, nil
		}
		room.status = st
		if st == game.GameStatusContinue {
			room.turn = 2
		}
		room.lastMsg = fmt.Sprintf("local write (%d,%d)", cmd.x, cmd.y)
		return false, broadcastState(room, conn, hostName)

	case "chat":
		room.hint("me: " + cmd.message)
		env, err := protocol.NewEnvelope(protocol.TypeChat, hostName, protocol.ChatPayload{Message: cmd.message})
		if err != nil {
			return false, err
		}
		return false, conn.Send(env)

	case "leave":
		env, err := protocol.NewEnvelope(protocol.TypeLeave, hostName, nil)
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
		room.status = game.GameStatusWin
		room.board.Winner = 2
		room.lastMsg = "local yield"
		return false, broadcastState(room, conn, hostName)

	case "restart":
		room.board = game.NewBoard(room.board.Size)
		room.turn = 1
		room.status = game.GameStatusContinue
		room.board.Winner = 0
		room.lastMsg = "session reset"
		return false, broadcastState(room, conn, hostName)
	}
	return false, nil
}
