package protocol

import (
	"encoding/json"
	"intranet-play/internal/game"
)

type MsgType string

const (
	TypeJoin    MsgType = "JOIN"
	TypePlace   MsgType = "PLACE"
	TypeChat    MsgType = "CHAT"
	TypeLeave   MsgType = "LEAVE"
	TypeError   MsgType = "ERROR"
	TypeRestart MsgType = "RESTART"
	TypeState   MsgType = "STATE"
	TypeWelcome MsgType = "WELCOME"
	TypeResign  MsgType = "RESIGN"
)

type Envelope struct {
	Type    MsgType         `json:"type"`
	From    string          `json:"from,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Pos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type JoinPayload struct {
	Name string `json:"name"`
}

type PlacePayload struct {
	Pos Pos `json:"pos"`
}

type WelcomePayload struct {
	Size     int    `json:"size"`
	Opponent string `json:"opponent"`
}

type StatePayload struct {
	Board  *game.Board     `json:"board"`
	Status game.GameStatus `json:"status"`
	Turn   int             `json:"turn"`
	Winner int             `json:"winner"`
}

type ChatPayload struct {
	Message string `json:"message"`
}
type ErrorPayload struct {
	Message string `json:"message"`
}

func NewEnvelope(t MsgType, from string, payload any) (Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		raw = b
	}
	return Envelope{
		Type:    t,
		From:    from,
		Payload: raw,
	}, nil
}

func Encode(env Envelope) ([]byte, error) {
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
func DecodeLine(line []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
