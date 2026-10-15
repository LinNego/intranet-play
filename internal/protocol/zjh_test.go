package protocol

import (
	"encoding/json"
	"testing"

	"intranet-play/internal/ddz"
	"intranet-play/internal/zjh"
)

// TestZjhPublicHasNoCards 是炸金花最硬的隐私要求：公共报文里一张牌都不能有。
func TestZjhPublicHasNoCards(t *testing.T) {
	view := zjh.PublicView{
		Round: 2,
		Phase: zjh.PhaseBetting,
		Turn:  1,
		Pot:   9,
		Stake: 3,
		Ante:  1,
		Seats: []zjh.SeatView{
			{Seat: 0, Looked: true, Status: zjh.StatusPlaying, Bet: 4, Chips: 96},
			{Seat: 1, Looked: false, Status: zjh.StatusPlaying, Bet: 2, Chips: 98},
			{Seat: 2, Looked: false, Status: zjh.StatusFolded, Bet: 1, Chips: 99},
		},
		LastEvent: "第 2 局开始",
	}
	env, err := NewEnvelope(TypeZjhPublic, "host", ZjhPublicPayload{
		View:  view,
		Seats: []ZjhSeatPayload{{Seat: 0, Name: "Alice", Online: true, IsHost: true}},
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
	var p ZjhPublicPayload
	if err := json.Unmarshal(decoded.Payload, &p); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if p.View.Pot != 9 || len(p.View.Seats) != 3 || p.View.Seats[0].Bet != 4 {
		t.Fatalf("公共状态丢失: %+v", p.View)
	}
	if got := countCardIDs(decoded.Payload); got != 0 {
		t.Fatalf("公共报文里出现了 %d 张牌，一张都不该有: %s", got, decoded.Payload)
	}
}

// TestZjhHandEmptyBeforeLook 没看牌时手牌必须是空数组——
// 不能"发下去但界面不显示"，那样改个客户端就能偷看。
func TestZjhHandEmptyBeforeLook(t *testing.T) {
	blind := zjh.PrivateView{Seat: 1, Looked: false, Cost: 1, Stake: 2, MaxStake: 20}
	env, err := NewEnvelope(TypeZjhHand, "host", ZjhHandPayload{View: blind})
	if err != nil {
		t.Fatalf("构造报文失败: %v", err)
	}
	line, _ := Encode(env)
	decoded, err := DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var p ZjhHandPayload
	if err := json.Unmarshal(decoded.Payload, &p); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if p.View.Looked || len(p.View.Cards) != 0 {
		t.Fatalf("没看牌时不该带牌: %+v", p.View)
	}
	if countCardIDs(decoded.Payload) != 0 {
		t.Fatalf("没看牌时报文里不该有牌: %s", decoded.Payload)
	}

	// 看牌之后才带牌
	looked := zjh.PrivateView{
		Seat: 1, Looked: true, HandLabel: "金花",
		Cards: []zjh.Card{{ID: 3, Rank: ddz.RankA, Suit: ddz.SuitHeart}},
	}
	env2, err := NewEnvelope(TypeZjhHand, "host", ZjhHandPayload{View: looked})
	if err != nil {
		t.Fatalf("构造报文失败: %v", err)
	}
	line2, _ := Encode(env2)
	decoded2, err := DecodeLine(line2[:len(line2)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var p2 ZjhHandPayload
	if err := json.Unmarshal(decoded2.Payload, &p2); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if !p2.View.Looked || len(p2.View.Cards) != 1 || p2.View.HandLabel != "金花" {
		t.Fatalf("看牌后应当带牌: %+v", p2.View)
	}
}

// TestZjhPeekCarriesBothHands 比牌报文带双方牌面（只发给当事人，这一点由房间层保证）。
func TestZjhPeekCarriesBothHands(t *testing.T) {
	peek := zjh.Peek{
		A: 0, B: 2, Loser: 2, Reason: "座位0 的豹子 压过 座位2 的顺子",
		CardsA: []zjh.Card{{ID: 1, Rank: ddz.RankA, Suit: ddz.SuitSpade}},
		CardsB: []zjh.Card{{ID: 2, Rank: ddz.RankK, Suit: ddz.SuitHeart}},
		HandA:  "豹子", HandB: "顺子",
	}
	env, err := NewEnvelope(TypeZjhPeek, "host", ZjhPeekPayload{Peek: peek})
	if err != nil {
		t.Fatalf("构造报文失败: %v", err)
	}
	line, _ := Encode(env)
	decoded, err := DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var p ZjhPeekPayload
	if err := json.Unmarshal(decoded.Payload, &p); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if p.Peek.A != 0 || p.Peek.B != 2 || p.Peek.Loser != 2 {
		t.Fatalf("比牌记录丢失: %+v", p.Peek)
	}
	if len(p.Peek.CardsA) != 1 || len(p.Peek.CardsB) != 1 || p.Peek.HandA != "豹子" {
		t.Fatalf("比牌牌面丢失: %+v", p.Peek)
	}
}

func TestZjhActionAndLobbyRoundTrip(t *testing.T) {
	act := zjh.Action{Kind: zjh.ActRaise, Amount: 6}
	env, err := NewEnvelope(TypeZjhAction, "Bob", ZjhActionPayload{Action: act})
	if err != nil {
		t.Fatalf("构造动作失败: %v", err)
	}
	line, _ := Encode(env)
	decoded, err := DecodeLine(line[:len(line)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var ap ZjhActionPayload
	if err := json.Unmarshal(decoded.Payload, &ap); err != nil {
		t.Fatalf("动作载荷解析失败: %v", err)
	}
	if ap.Action.Kind != zjh.ActRaise || ap.Action.Amount != 6 {
		t.Fatalf("动作丢失: %+v", ap.Action)
	}

	cmp := zjh.Action{Kind: zjh.ActCompare, Target: 2}
	env2, err := NewEnvelope(TypeZjhAction, "Bob", ZjhActionPayload{Action: cmp})
	if err != nil {
		t.Fatalf("构造动作失败: %v", err)
	}
	line2, _ := Encode(env2)
	decoded2, err := DecodeLine(line2[:len(line2)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var ap2 ZjhActionPayload
	if err := json.Unmarshal(decoded2.Payload, &ap2); err != nil {
		t.Fatalf("动作载荷解析失败: %v", err)
	}
	if ap2.Action.Target != 2 {
		t.Fatalf("比牌目标丢失: %+v", ap2.Action)
	}

	lobby, err := NewEnvelope(TypeZjhLobby, "host", ZjhLobbyPayload{
		You: 1, Max: 4, Chips: 100, Ante: 1, MaxStake: 20, MaxRounds: 6,
		Seats: []ZjhSeatPayload{{Seat: 0, Name: "Alice", IsHost: true}},
	})
	if err != nil {
		t.Fatalf("构造大厅失败: %v", err)
	}
	line3, _ := Encode(lobby)
	decoded3, err := DecodeLine(line3[:len(line3)-1])
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	var lp ZjhLobbyPayload
	if err := json.Unmarshal(decoded3.Payload, &lp); err != nil {
		t.Fatalf("大厅载荷解析失败: %v", err)
	}
	if lp.You != 1 || lp.Ante != 1 || lp.MaxStake != 20 || lp.MaxRounds != 6 || len(lp.Seats) != 1 {
		t.Fatalf("大厅信息丢失: %+v", lp)
	}
}
