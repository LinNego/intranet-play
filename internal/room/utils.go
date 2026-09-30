package room

import (
	"fmt"
	"intranet-play/internal/game"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
	"strconv"
	"strings"
)

type localCmd struct {
	kind    string
	x, y    int
	message string
}

type Room struct {
	board   *game.Board
	players [2]string
	turn    int
	status  game.GameStatus
	lastMsg string
	app     *ui.App
	you     int
}

func NewRoom(name string, boardSize int) *Room {
	if boardSize < 10 {
		boardSize = 10
	}
	if boardSize > 20 {
		boardSize = 20
	}
	return &Room{
		board:   game.NewBoard(boardSize),
		players: [2]string{name, ""},
		turn:    1,
		status:  game.GameStatusPending,
	}
}

func (r *Room) frame() ui.Frame {
	youName, opponent := r.players[0], r.players[1]
	if r.you == 2 {
		youName, opponent = r.players[1], r.players[0]
	}
	return ui.Frame{
		Board:    r.board,
		Status:   r.status,
		Turn:     r.turn,
		You:      r.you,
		YouName:  youName,
		Opponent: opponent,
		LastMsg:  r.lastMsg,
	}
}

func (r *Room) attachUI(you int) {
	r.you = you
	r.app = ui.NewApp(r.frame())
}

func (r *Room) render() {
	if r == nil || r.app == nil {
		return
	}
	r.app.UpdateFrame(r.frame())
}

func (r *Room) hint(msg string) {
	r.lastMsg = msg
	r.render()
}

func parseLineCmd(line string) localCmd {
	line = strings.TrimSpace(line)
	switch {
	case line == "leave" || line == "quit":
		return localCmd{kind: "leave"}
	case line == "resign":
		return localCmd{kind: "resign"}
	case line == "restart":
		return localCmd{kind: "restart"}
	case line == "chat" || line == "say":
		return localCmd{kind: "hint", message: "usage: chat TEXT"}
	case strings.HasPrefix(line, "chat "):
		return localCmd{kind: "chat", message: strings.TrimSpace(line[5:])}
	case strings.HasPrefix(line, "say "):
		return localCmd{kind: "chat", message: strings.TrimSpace(line[4:])}
	default:
		x, y, err := parsePos(line)
		if err != nil {
			return localCmd{kind: "hint", message: "bad input, e.g. 7,7 or a,2"}
		}
		return localCmd{kind: "place", x: x, y: y}
	}
}

func parsePos(s string) (int, int, error) {
	s = strings.ReplaceAll(s, "，", ",")
	s = strings.ReplaceAll(s, " ", ",")
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad pos")
	}
	x, err1 := parseAxis(strings.TrimSpace(parts[0]))
	y, err2 := parseAxis(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("bad pos")
	}
	return x, y, nil
}

func parseAxis(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	// decimal first
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	// single hex/base36 digit: 0-9 a-z
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0'), nil
		case c >= 'a' && c <= 'z':
			return int(c-'a') + 10, nil
		case c >= 'A' && c <= 'Z':
			return int(c-'A') + 10, nil
		}
	}
	return 0, fmt.Errorf("bad axis")
}

func sendErr(peer *netx.Conn, from, msg string) error {
	env, err := protocol.NewEnvelope(protocol.TypeError, from, protocol.ErrorPayload{Message: msg})
	if err != nil {
		return err
	}
	return peer.Send(env)
}

func broadcastState(r *Room, conn *netx.Conn, from string) error {
	env, err := protocol.NewEnvelope(protocol.TypeState, from, protocol.StatePayload{
		Board:  r.board,
		Status: r.status,
		Turn:   r.turn,
		Winner: r.board.Winner,
	})
	if err != nil {
		return err
	}
	r.render()
	return conn.Send(env)
}
