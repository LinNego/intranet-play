package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"intranet-play/internal/ddz"
)

// TestDdzEnvelopeRoundTrip 验证斗地主报文能原样往返，且公共报文里不含手牌内容。
func TestDdzEnvelopeRoundTrip(t *testing.T) {
	view := ddz.PublicView{
		Round:      2,
		Phase:      ddz.PhasePlaying,
		Turn:       1,
		Landlord:   1,
		HandCount:  [ddz.Seats]int{20, 12, 9},
		Bottom:     []ddz.Card{{ID: 7, Rank: ddz.RankA, Suit: ddz.SuitSpade}},
		BidScore:   2,
		Multiplier: 4,
		LastPlay: &ddz.PlayView{
			Seat: 0, Kind: ddz.KindBomb, Main: ddz.RankK, Size: 4,
			Cards: []ddz.Card{{ID: 1, Rank: ddz.RankK, Suit: ddz.SuitClub}},
			Label: "炸弹(K)",
		},
	}
	env, err := NewEnvelope(TypeDdzPublic, "host", DdzPublicPayload{
		View: view, Seats: []DdzSeatPayload{{Seat: 0, Name: "Alice", IsHost: true}}, Rounds: 10,
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
	if decoded.Type != TypeDdzPublic {
		t.Fatalf("类型 = %s, 期望 %s", decoded.Type, TypeDdzPublic)
	}
	var payload DdzPublicPayload
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if payload.View.HandCount != view.HandCount {
		t.Fatalf("手牌张数丢失: %v", payload.View.HandCount)
	}
	if payload.View.LastPlay == nil || payload.View.LastPlay.Kind != ddz.KindBomb {
		t.Fatalf("上一手牌信息丢失: %+v", payload.View.LastPlay)
	}
	if payload.Rounds != 10 || len(payload.Seats) != 1 || payload.Seats[0].Name != "Alice" {
		t.Fatalf("座位信息丢失: %+v", payload)
	}

	// 公共报文里绝不能出现任何 "hand" 字段
	if strings.Contains(string(decoded.Payload), `"hand"`) {
		t.Fatalf("公共报文里出现了手牌字段: %s", decoded.Payload)
	}
}

// TestDdzHandIsPerSeat 验证手牌报文带座位号，客户端可据此拒绝串台。
func TestDdzHandIsPerSeat(t *testing.T) {
	priv := ddz.PrivateView{
		Seat:   2,
		Hand:   []ddz.Card{{ID: 3, Rank: ddz.Rank5, Suit: ddz.SuitHeart}},
		IsTurn: true,
		MinBid: 2,
	}
	env, err := NewEnvelope(TypeDdzHand, "host", DdzHandPayload{View: priv})
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
	var payload DdzHandPayload
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if payload.View.Seat != 2 || len(payload.View.Hand) != 1 || !payload.View.IsTurn {
		t.Fatalf("手牌视图不正确: %+v", payload.View)
	}
}

// TestDdzActionRoundTrip 验证上行动作只带 cardID，不带牌面。
func TestDdzActionRoundTrip(t *testing.T) {
	act := ddz.Action{Kind: ddz.ActPlay, Cards: []int{5, 9, 12}}
	env, err := NewEnvelope(TypeDdzAction, "Bob", DdzActionPayload{Action: act})
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
	var payload DdzActionPayload
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("载荷解析失败: %v", err)
	}
	if payload.Action.Kind != ddz.ActPlay || len(payload.Action.Cards) != 3 {
		t.Fatalf("动作丢失: %+v", payload.Action)
	}
	if strings.Contains(string(decoded.Payload), `"rank"`) {
		t.Fatalf("上行动作不该携带牌面: %s", decoded.Payload)
	}
}
