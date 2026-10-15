package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"intranet-play/internal/bj"
	"intranet-play/internal/ddz"
)

// countCardIDs 数一数报文里出现了多少张牌的 ID。
func countCardIDs(raw []byte) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return -1
	}
	n := 0
	var walk func(any)
	walk = func(node any) {
		switch t := node.(type) {
		case map[string]any:
			for k, child := range t {
				if k == "id" {
					n++
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return n
}

// TestBjPublicRoundTrip 验证公共牌桌报文能原样往返，且庄家暗牌不出现在报文里。
func TestBjPublicRoundTrip(t *testing.T) {
	view := bj.PublicView{
		Round: 2,
		Phase: bj.PhasePlaying,
		Turn:  1,
		Dealer: bj.DealerView{
			Up:     []bj.Card{{ID: 5, Rank: ddz.Rank6, Suit: 0}},
			Hidden: 1,
			Score:  6,
		},
		Seats: []bj.SeatView{{
			Seat: 0, Bet: 10, Status: bj.StatusStood, Score: 18, Chips: 90,
			Cards: []bj.Card{{ID: 9, Rank: ddz.Rank10, Suit: 1}, {ID: 21, Rank: ddz.Rank8, Suit: 2}},
		}},
		LastEvent: "发牌完成",
	}
	env, err := NewEnvelope(TypeBjPublic, "host", BjPublicPayload{
		View:  view,
		Seats: []BjSeatPayload{{Seat: 0, Name: "Alice", Online: true, IsHost: true}},
		Max:   4,
	})
	if err != nil {
		t.Fatalf("构造报文失败: %v", err)
	}
	line, err := Encode(env)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	decoded, err := DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if decoded.Type != TypeBjPublic {
		t.Fatalf("类型 = %s, 期望 %s", decoded.Type, TypeBjPublic)
	}

	var p BjPublicPayload
	if err := json.Unmarshal(decoded.Payload, &p); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if p.View.Dealer.Hidden != 1 || len(p.View.Dealer.Up) != 1 {
		t.Fatalf("庄家明暗牌信息丢失: %+v", p.View.Dealer)
	}
	if p.View.Seats[0].Score != 18 || len(p.View.Seats[0].Cards) != 2 {
		t.Fatalf("座位信息丢失: %+v", p.View.Seats[0])
	}
	if p.Max != 4 || len(p.Seats) != 1 || p.Seats[0].Name != "Alice" {
		t.Fatalf("座位表丢失: %+v", p)
	}

	// 关键断言：报文里出现的牌 ID 数 == 可见牌张数（1 张庄家明牌 + 2 张闲家牌）
	// 如果有暗牌泄漏，这里会多出来。
	if got, want := countCardIDs(decoded.Payload), 3; got != want {
		t.Fatalf("报文里出现了 %d 张牌，期望 %d 张（暗牌不该出现）: %s", got, want, decoded.Payload)
	}
	if strings.Contains(string(decoded.Payload), `"hand"`) {
		t.Fatalf("21 点的公共报文不该有 hand 字段: %s", decoded.Payload)
	}
}

func TestBjActionAndLobbyRoundTrip(t *testing.T) {
	act := bj.Action{Kind: bj.ActBet, Amount: 25}
	env, err := NewEnvelope(TypeBjAction, "Bob", BjActionPayload{Action: act})
	if err != nil {
		t.Fatalf("构造动作失败: %v", err)
	}
	line, _ := Encode(env)
	decoded, err := DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var ap BjActionPayload
	if err := json.Unmarshal(decoded.Payload, &ap); err != nil {
		t.Fatalf("动作载荷解析失败: %v", err)
	}
	if ap.Action.Kind != bj.ActBet || ap.Action.Amount != 25 {
		t.Fatalf("动作丢失: %+v", ap.Action)
	}

	lobby, err := NewEnvelope(TypeBjLobby, "host", BjLobbyPayload{
		You: 2, Max: 4, Chips: 100,
		Seats: []BjSeatPayload{{Seat: 0, Name: "Alice", IsHost: true}, {Seat: 1, Name: "Bob", Online: true}},
	})
	if err != nil {
		t.Fatalf("构造大厅失败: %v", err)
	}
	line, _ = Encode(lobby)
	decoded, err = DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var lp BjLobbyPayload
	if err := json.Unmarshal(decoded.Payload, &lp); err != nil {
		t.Fatalf("大厅载荷解析失败: %v", err)
	}
	if lp.You != 2 || lp.Max != 4 || lp.Chips != 100 || len(lp.Seats) != 2 {
		t.Fatalf("大厅信息丢失: %+v", lp)
	}
	if !lp.Seats[0].IsHost || lp.Seats[1].IsHost {
		t.Fatalf("房主标记不对: %+v", lp.Seats)
	}
}
