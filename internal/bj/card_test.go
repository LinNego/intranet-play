package bj

import (
	"math/rand"
	"testing"

	"intranet-play/internal/ddz"
)

// mustCards 按点数标签从一副新牌里取牌（忽略花色，按牌堆顺序取第一张可用的）。
func mustCards(t *testing.T, labels ...string) []Card {
	t.Helper()
	deck := ddz.NewDeck()
	used := make(map[int]bool, len(labels))
	out := make([]Card, 0, len(labels))
	for _, label := range labels {
		found := false
		for _, c := range deck {
			if used[c.ID] || c.Rank.Label() != label {
				continue
			}
			used[c.ID] = true
			out = append(out, c)
			found = true
			break
		}
		if !found {
			t.Fatalf("牌堆里取不出 %q", label)
		}
	}
	return out
}

func TestNewShoe(t *testing.T) {
	shoe := NewShoe(rand.New(rand.NewSource(1)))
	if len(shoe) != 52 {
		t.Fatalf("牌靴张数 = %d, 期望 52（不要大小王）", len(shoe))
	}
	seen := make(map[int]bool, 52)
	rankCount := make(map[Rank]int)
	for _, c := range shoe {
		if seen[c.ID] {
			t.Fatalf("牌 ID 重复: %d", c.ID)
		}
		seen[c.ID] = true
		if c.Rank == ddz.RankJokerSmall || c.Rank == ddz.RankJokerBig {
			t.Fatalf("21 点不该有王: %s", c.Label())
		}
		rankCount[c.Rank]++
	}
	// 每个点数各 4 张
	for _, r := range []Rank{ddz.Rank2, ddz.Rank5, ddz.Rank10, ddz.RankJ, ddz.RankQ, ddz.RankK, ddz.RankA} {
		if rankCount[r] != 4 {
			t.Fatalf("点数 %s 张数 = %d, 期望 4", r.Label(), rankCount[r])
		}
	}
}

func TestScore(t *testing.T) {
	cases := []struct {
		name string
		hand []string
		want int
		soft bool
	}{
		{name: "空手牌", hand: nil, want: 0, soft: false},
		{name: "单张2", hand: []string{"2"}, want: 2, soft: false}, // Rank2 的数值是 15，必须显式映射
		{name: "单张3", hand: []string{"3"}, want: 3, soft: false},
		{name: "单张10", hand: []string{"10"}, want: 10, soft: false},
		{name: "单张J", hand: []string{"J"}, want: 10, soft: false},
		{name: "单张Q", hand: []string{"Q"}, want: 10, soft: false},
		{name: "单张K", hand: []string{"K"}, want: 10, soft: false},
		{name: "单张A是软11", hand: []string{"A"}, want: 11, soft: true},
		{name: "K加A", hand: []string{"K", "A"}, want: 21, soft: true},
		{name: "两张A", hand: []string{"A", "A"}, want: 12, soft: true},
		{name: "A A 9", hand: []string{"A", "A", "9"}, want: 21, soft: true},
		{name: "A A A 8", hand: []string{"A", "A", "A", "8"}, want: 21, soft: true},
		{name: "A 9 9 硬牌", hand: []string{"A", "9", "9"}, want: 19, soft: false},
		{name: "10加5", hand: []string{"10", "5"}, want: 15, soft: false},
		{name: "K Q J 爆牌", hand: []string{"K", "Q", "J"}, want: 30, soft: false},
		{name: "A K Q 爆牌", hand: []string{"A", "K", "Q"}, want: 21, soft: false}, // 11+10+10=31 → A 降 1
		{name: "2 2 2 软", hand: []string{"2", "2", "2"}, want: 6, soft: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, soft := Score(mustCards(t, tc.hand...))
			if got != tc.want || soft != tc.soft {
				t.Fatalf("Score(%v) = (%d, soft=%v), 期望 (%d, soft=%v)", tc.hand, got, soft, tc.want, tc.soft)
			}
		})
	}
}

func TestIsBlackjackAndBust(t *testing.T) {
	if !IsBlackjack(mustCards(t, "A", "K")) {
		t.Fatal("A+K 应当是黑杰克")
	}
	if IsBlackjack(mustCards(t, "A", "A", "9")) {
		t.Fatal("三张凑够 21 点不算黑杰克")
	}
	if IsBlackjack(mustCards(t, "10", "10")) {
		t.Fatal("20 点不是黑杰克")
	}
	if !IsBlackjack(mustCards(t, "A", "10")) {
		t.Fatal("A+10 应当是黑杰克")
	}
	if IsBust(mustCards(t, "K", "Q")) {
		t.Fatal("20 点没爆")
	}
	if !IsBust(mustCards(t, "K", "Q", "2")) {
		t.Fatal("22 点应当爆牌")
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		hand []string
		want string
	}{
		{hand: []string{"A", "K"}, want: "黑杰克"},
		{hand: []string{"A", "6"}, want: "软 17 点"},
		{hand: []string{"10", "8"}, want: "18 点"},
		{hand: []string{"K", "Q", "2"}, want: "爆牌(22 点)"},
		{hand: nil, want: "无牌"},
	}
	for _, tc := range cases {
		if got := Describe(mustCards(t, tc.hand...)); got != tc.want {
			t.Fatalf("Describe(%v) = %q, 期望 %q", tc.hand, got, tc.want)
		}
	}
}
