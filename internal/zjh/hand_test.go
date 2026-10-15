package zjh

import (
	"testing"

	"intranet-play/internal/ddz"
)

// mustCards 按 "A♠" 这样的标签取牌。炸金花要看花色（同花、同花顺），
// 所以这里不能像 21 点那样只按点数取。
func mustCards(t *testing.T, labels ...string) []Card {
	t.Helper()
	deck := ddz.NewDeck()
	used := make(map[int]bool, len(labels))
	out := make([]Card, 0, len(labels))
	for _, label := range labels {
		found := false
		for _, c := range deck {
			if used[c.ID] || c.Label() != label {
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

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name  string
		cards []string
		kind  HandKind
		key   []Rank
	}{
		{name: "豹子A", cards: []string{"A♠", "A♥", "A♦"}, kind: KindBaozi, key: []Rank{ddz.RankA}},
		{name: "豹子2最小", cards: []string{"2♠", "2♥", "2♦"}, kind: KindBaozi, key: []Rank{ddz.Rank2}},

		{name: "同花顺AKQ", cards: []string{"A♠", "K♠", "Q♠"}, kind: KindTonghuaShun, key: []Rank{ddz.RankA}},
		{name: "同花顺234", cards: []string{"2♠", "3♠", "4♠"}, kind: KindTonghuaShun, key: []Rank{ddz.Rank4}},
		{name: "同花顺A23最小", cards: []string{"A♠", "2♠", "3♠"}, kind: KindTonghuaShun, key: []Rank{ddz.Rank3}},

		{name: "金花", cards: []string{"A♥", "K♥", "7♥"}, kind: KindJinhua, key: []Rank{ddz.RankA, ddz.RankK, ddz.Rank7}},
		{name: "金花带2", cards: []string{"2♣", "9♣", "5♣"}, kind: KindJinhua, key: []Rank{ddz.Rank9, ddz.Rank5, ddz.Rank2}},

		{name: "顺子", cards: []string{"A♠", "K♥", "Q♦"}, kind: KindShunzi, key: []Rank{ddz.RankA}},
		{name: "顺子A23", cards: []string{"A♠", "2♥", "3♦"}, kind: KindShunzi, key: []Rank{ddz.Rank3}},

		{name: "对子", cards: []string{"K♠", "K♥", "7♦"}, kind: KindDuizi, key: []Rank{ddz.RankK, ddz.Rank7}},
		{name: "对子甲小", cards: []string{"4♠", "4♥", "A♦"}, kind: KindDuizi, key: []Rank{ddz.Rank4, ddz.RankA}},
		{name: "对2", cards: []string{"2♠", "2♥", "5♦"}, kind: KindDuizi, key: []Rank{ddz.Rank2, ddz.Rank5}},

		{name: "散牌", cards: []string{"A♠", "K♥", "7♦"}, kind: KindSanpai, key: []Rank{ddz.RankA, ddz.RankK, ddz.Rank7}},
		{name: "散牌带2", cards: []string{"2♠", "K♥", "7♦"}, kind: KindSanpai, key: []Rank{ddz.RankK, ddz.Rank7, ddz.Rank2}},

		// 反例：A、K、2 不连续
		{name: "AK2不是顺子", cards: []string{"A♠", "K♥", "2♦"}, kind: KindSanpai, key: []Rank{ddz.RankA, ddz.RankK, ddz.Rank2}},
		// 反例：同花但夹了不连续（这里是散牌里的同花判定）
		{name: "同花不连续仍是金花", cards: []string{"A♠", "Q♠", "9♠"}, kind: KindJinhua, key: []Rank{ddz.RankA, ddz.RankQ, ddz.Rank9}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(mustCards(t, tc.cards...))
			if !got.Valid() {
				t.Fatalf("Evaluate(%v) 没识别出牌型", tc.cards)
			}
			if got.Kind != tc.kind {
				t.Fatalf("Evaluate(%v).Kind = %s, 期望 %s", tc.cards, got.Kind, tc.kind)
			}
			if len(got.Key) != len(tc.key) {
				t.Fatalf("Evaluate(%v).Key = %v, 期望 %v", tc.cards, got.Key, tc.key)
			}
			for i := range tc.key {
				if got.Key[i] != tc.key[i] {
					t.Fatalf("Evaluate(%v).Key = %v, 期望 %v", tc.cards, got.Key, tc.key)
				}
			}
		})
	}
}

func TestEvaluateRejectsWrongCount(t *testing.T) {
	if Evaluate(nil).Valid() {
		t.Fatal("空手牌不该有效")
	}
	if Evaluate(mustCards(t, "A♠", "K♥")).Valid() {
		t.Fatal("两张牌不该有效")
	}
	if Evaluate(mustCards(t, "A♠", "K♥", "Q♦", "J♣")).Valid() {
		t.Fatal("四张牌不该有效")
	}
}

func TestCompareKindOrder(t *testing.T) {
	hand := func(labels ...string) Hand { return Evaluate(mustCards(t, labels...)) }

	cases := []struct {
		name string
		a, b []string
		want int
	}{
		{name: "豹子压同花顺", a: []string{"2♠", "2♥", "2♦"}, b: []string{"A♠", "K♠", "Q♠"}, want: 1},
		{name: "同花顺压金花", a: []string{"4♠", "5♠", "6♠"}, b: []string{"A♥", "K♥", "9♥"}, want: 1},
		{name: "金花压顺子", a: []string{"2♥", "5♥", "9♥"}, b: []string{"A♠", "K♥", "Q♦"}, want: 1},
		{name: "顺子压对子", a: []string{"4♠", "5♥", "6♦"}, b: []string{"A♠", "A♥", "2♦"}, want: 1},
		{name: "对子压散牌", a: []string{"2♠", "2♥", "3♦"}, b: []string{"A♠", "K♥", "J♦"}, want: 1},
		{name: "散牌最小", a: []string{"A♠", "K♥", "7♦"}, b: []string{"2♣", "2♦", "3♣"}, want: -1},

		{name: "豹子AAA压KKK", a: []string{"A♠", "A♥", "A♦"}, b: []string{"K♠", "K♥", "K♦"}, want: 1},
		{name: "豹子222最小", a: []string{"2♠", "2♥", "2♦"}, b: []string{"3♠", "3♥", "3♦"}, want: -1},
		{name: "顺子A23最小", a: []string{"A♠", "2♥", "3♦"}, b: []string{"2♠", "3♥", "4♦"}, want: -1},
		{name: "顺子QKA最大", a: []string{"Q♠", "K♥", "A♦"}, b: []string{"J♠", "Q♥", "K♦"}, want: 1},
		{name: "同花顺比最大张", a: []string{"3♠", "4♠", "5♠"}, b: []string{"2♠", "3♠", "4♠"}, want: 1},
		{name: "金花比到第二张", a: []string{"A♥", "K♥", "7♥"}, b: []string{"A♥", "Q♥", "J♥"}, want: 1},
		{name: "对子先比对子", a: []string{"9♠", "9♥", "3♦"}, b: []string{"8♠", "8♥", "A♦"}, want: 1},
		{name: "对子再看单张", a: []string{"9♠", "9♥", "A♦"}, b: []string{"9♣", "9♦", "K♦"}, want: 1},
		{name: "散牌比到第三张", a: []string{"A♠", "K♥", "9♦"}, b: []string{"A♣", "K♦", "8♦"}, want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compare(hand(tc.a...), hand(tc.b...)); got != tc.want {
				t.Fatalf("Compare(%v, %v) = %d, 期望 %d", tc.a, tc.b, got, tc.want)
			}
			// 反过来应当是相反的结论
			if got := Compare(hand(tc.b...), hand(tc.a...)); got != -tc.want {
				t.Fatalf("反向 Compare(%v, %v) = %d, 期望 %d", tc.b, tc.a, got, -tc.want)
			}
		})
	}
}

// TestCompareTiesWithoutSuit 花色不参与比较：点数完全相同就是平局，
// 由比牌规则（闷牌占优 / 主动比牌者输）裁决。
func TestCompareTiesWithoutSuit(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
	}{
		{name: "散牌同点数不同花", a: []string{"A♠", "K♥", "7♦"}, b: []string{"A♥", "K♦", "7♣"}},
		{name: "金花同点数不同花", a: []string{"A♠", "K♠", "7♠"}, b: []string{"A♥", "K♥", "7♥"}},
		{name: "对子同点数", a: []string{"9♠", "9♥", "3♦"}, b: []string{"9♣", "9♦", "3♠"}},
		{name: "顺子同点数", a: []string{"5♠", "6♥", "7♦"}, b: []string{"5♥", "6♦", "7♣"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Evaluate(mustCards(t, tc.a...)), Evaluate(mustCards(t, tc.b...))
			if got := Compare(a, b); got != 0 {
				t.Fatalf("Compare(%v, %v) = %d, 期望 0（花色不该参与比较）", tc.a, tc.b, got)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	got := Describe(mustCards(t, "A♠", "K♥", "7♦"))
	if got != "散牌（A♠ K♥ 7♦）" {
		t.Fatalf("Describe = %q", got)
	}
	if got := Describe(mustCards(t, "A♥", "K♥", "7♥")); got != "金花（A♥ K♥ 7♥）" {
		t.Fatalf("Describe = %q", got)
	}
	if got := Describe(nil); got != "无牌" {
		t.Fatalf("Describe(nil) = %q", got)
	}
}
