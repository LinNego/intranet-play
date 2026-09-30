package ddz

import "testing"

// mustCards 从一副新牌里按点数标签取牌（忽略花色，按牌堆顺序取第一张可用的）。
// 标签用 "3".."10" "J" "Q" "K" "A" "2" "w"(小王) "W"(大王)。
func mustCards(t *testing.T, labels ...string) []Card {
	t.Helper()
	deck := NewDeck()
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

func TestNewDeck(t *testing.T) {
	deck := NewDeck()
	if len(deck) != 54 {
		t.Fatalf("牌堆张数 = %d, 期望 54", len(deck))
	}
	seen := make(map[int]bool, 54)
	rankCount := make(map[Rank]int)
	for _, c := range deck {
		if seen[c.ID] {
			t.Fatalf("牌 ID 重复: %d", c.ID)
		}
		seen[c.ID] = true
		rankCount[c.Rank]++
	}
	for id := 0; id < 54; id++ {
		if !seen[id] {
			t.Fatalf("缺少牌 ID %d", id)
		}
	}
	for r := Rank3; r <= Rank2; r++ {
		if rankCount[r] != 4 {
			t.Fatalf("点数 %s 张数 = %d, 期望 4", r.Label(), rankCount[r])
		}
	}
	if rankCount[RankJokerSmall] != 1 || rankCount[RankJokerBig] != 1 {
		t.Fatalf("王的数量不对: 小=%d 大=%d", rankCount[RankJokerSmall], rankCount[RankJokerBig])
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		cards []string
		want  ComboKind
		main  Rank
		size  int
		chain int
		valid bool
	}{
		{name: "单张", cards: []string{"3"}, want: KindSingle, main: Rank3, size: 1, chain: 1, valid: true},
		{name: "单张大王", cards: []string{"W"}, want: KindSingle, main: RankJokerBig, size: 1, chain: 1, valid: true},
		{name: "对子", cards: []string{"9", "9"}, want: KindPair, main: Rank9, size: 2, chain: 1, valid: true},
		{name: "对2", cards: []string{"2", "2"}, want: KindPair, main: Rank2, size: 2, chain: 1, valid: true},
		{name: "三张", cards: []string{"5", "5", "5"}, want: KindTriple, main: Rank5, size: 3, chain: 1, valid: true},
		{name: "三带一", cards: []string{"5", "5", "5", "K"}, want: KindTripleSingle, main: Rank5, size: 4, chain: 1, valid: true},
		{name: "三带二", cards: []string{"5", "5", "5", "K", "K"}, want: KindTriplePair, main: Rank5, size: 5, chain: 1, valid: true},
		{name: "炸弹", cards: []string{"7", "7", "7", "7"}, want: KindBomb, main: Rank7, size: 4, chain: 1, valid: true},
		{name: "炸弹2", cards: []string{"2", "2", "2", "2"}, want: KindBomb, main: Rank2, size: 4, chain: 1, valid: true},
		{name: "王炸", cards: []string{"w", "W"}, want: KindRocket, main: RankJokerBig, size: 2, chain: 1, valid: true},
		{name: "顺子5张", cards: []string{"3", "4", "5", "6", "7"}, want: KindStraight, main: Rank7, size: 5, chain: 5, valid: true},
		{name: "顺子到A", cards: []string{"10", "J", "Q", "K", "A"}, want: KindStraight, main: RankA, size: 5, chain: 5, valid: true},
		{name: "连对3对", cards: []string{"3", "3", "4", "4", "5", "5"}, want: KindStraightPair, main: Rank5, size: 6, chain: 3, valid: true},
		{name: "纯飞机2连", cards: []string{"3", "3", "3", "4", "4", "4"}, want: KindPlane, main: Rank4, size: 6, chain: 2, valid: true},
		{name: "纯飞机3连", cards: []string{"3", "3", "3", "4", "4", "4", "5", "5", "5"}, want: KindPlane, main: Rank5, size: 9, chain: 3, valid: true},
		{name: "飞机带单", cards: []string{"3", "3", "3", "4", "4", "4", "5", "6"}, want: KindPlaneSingle, main: Rank4, size: 8, chain: 2, valid: true},
		{name: "飞机带单含2", cards: []string{"3", "3", "3", "4", "4", "4", "2", "w"}, want: KindPlaneSingle, main: Rank4, size: 8, chain: 2, valid: true},
		{name: "飞机带对", cards: []string{"3", "3", "3", "4", "4", "4", "5", "5", "6", "6"}, want: KindPlanePair, main: Rank4, size: 10, chain: 2, valid: true},
		{name: "四带两单", cards: []string{"3", "3", "3", "3", "5", "6"}, want: KindFourTwo, main: Rank3, size: 6, chain: 1, valid: true},
		{name: "四带一对", cards: []string{"3", "3", "3", "3", "5", "5"}, want: KindFourTwo, main: Rank3, size: 6, chain: 1, valid: true},
		{name: "四带两对", cards: []string{"3", "3", "3", "3", "5", "5", "6", "6"}, want: KindFourTwoPair, main: Rank3, size: 8, chain: 1, valid: true},

		// 反例
		{name: "空", cards: []string{}, valid: false},
		{name: "两张不同点", cards: []string{"3", "4"}, valid: false},
		{name: "三张不齐", cards: []string{"3", "3", "4"}, valid: false},
		{name: "顺子只有4张", cards: []string{"3", "4", "5", "6"}, valid: false},
		{name: "顺子含2", cards: []string{"J", "Q", "K", "A", "2"}, valid: false},
		{name: "顺子不连续", cards: []string{"3", "4", "5", "6", "8"}, valid: false},
		{name: "连对只有2对", cards: []string{"3", "3", "4", "4"}, valid: false},
		{name: "连对含2", cards: []string{"K", "K", "A", "A", "2", "2"}, valid: false},
		{name: "连对不连续", cards: []string{"3", "3", "4", "4", "6", "6"}, valid: false},
		{name: "飞机不连续", cards: []string{"3", "3", "3", "5", "5", "5"}, valid: false},
		{name: "飞机含2", cards: []string{"A", "A", "A", "2", "2", "2"}, valid: false},
		{name: "两个炸弹不算四带两对", cards: []string{"3", "3", "3", "3", "4", "4", "4", "4"}, valid: false},
		{name: "飞机带单翅膀与链重叠", cards: []string{"3", "3", "3", "3", "4", "4", "4", "5"}, valid: false},
		{name: "飞机带对翅膀张数不对", cards: []string{"3", "3", "3", "4", "4", "4", "5", "5", "5", "6"}, valid: false},
		{name: "四带一", cards: []string{"3", "3", "3", "3", "5"}, valid: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Classify(mustCards(t, tc.cards...))
			if ok != tc.valid {
				t.Fatalf("Classify(%v) 合法 = %v, 期望 %v (得到 %+v)", tc.cards, ok, tc.valid, got)
			}
			if !tc.valid {
				return
			}
			if got.Kind != tc.want || got.Main != tc.main || got.Size != tc.size || got.Chain != tc.chain {
				t.Fatalf("Classify(%v) = %+v, 期望 kind=%s main=%s size=%d chain=%d",
					tc.cards, got, tc.want, tc.main.Label(), tc.size, tc.chain)
			}
		})
	}
}

func TestBeats(t *testing.T) {
	combo := func(labels ...string) Combo {
		c, ok := Classify(mustCards(t, labels...))
		if !ok {
			t.Fatalf("构造牌型失败: %v", labels)
		}
		return c
	}

	cases := []struct {
		name string
		a, b Combo
		want bool
	}{
		{"大单张压小单张", combo("9"), combo("3"), true},
		{"小单张压不过大单张", combo("3"), combo("9"), false},
		{"王压2", combo("W"), combo("2"), true},
		{"类型不同不可压", combo("9", "9"), combo("3"), false},
		{"张数不同不可压", combo("3", "4", "5", "6", "7", "8"), combo("3", "4", "5", "6", "7"), false},
		{"顺子比最大张", combo("4", "5", "6", "7", "8"), combo("3", "4", "5", "6", "7"), true},
		{"炸弹压顺子", combo("3", "3", "3", "3"), combo("10", "J", "Q", "K", "A"), true},
		{"炸弹压对子", combo("3", "3", "3", "3"), combo("A", "A"), true},
		{"大炸弹压小炸弹", combo("A", "A", "A", "A"), combo("K", "K", "K", "K"), true},
		{"小炸弹压不过大炸弹", combo("3", "3", "3", "3"), combo("4", "4", "4", "4"), false},
		{"王炸压炸弹", combo("w", "W"), combo("2", "2", "2", "2"), true},
		{"王炸不压王炸", combo("w", "W"), combo("w", "W"), false},
		{"普通牌型压不过炸弹", combo("10", "J", "Q", "K", "A"), combo("3", "3", "3", "3"), false},
		{"三带一比主牌", combo("9", "9", "9", "3"), combo("8", "8", "8", "A"), true},
		{"飞机比最大三张", combo("4", "4", "4", "5", "5", "5"), combo("3", "3", "3", "4", "4", "4"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Beats(tc.b); got != tc.want {
				t.Fatalf("%s.Beats(%s) = %v, 期望 %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestFindAndRemoveByIDs(t *testing.T) {
	hand := mustCards(t, "3", "4", "5")
	ids := []int{hand[0].ID, hand[2].ID}

	picked, ok := FindByIDs(hand, ids)
	if !ok || len(picked) != 2 {
		t.Fatalf("FindByIDs 失败: ok=%v len=%d", ok, len(picked))
	}
	if picked[0].ID != hand[0].ID || picked[1].ID != hand[2].ID {
		t.Fatalf("FindByIDs 取牌顺序不对: %+v", picked)
	}
	if _, ok := FindByIDs(hand, []int{hand[0].ID, hand[0].ID}); ok {
		t.Fatal("重复 ID 应当失败")
	}
	if _, ok := FindByIDs(hand, []int{999}); ok {
		t.Fatal("不存在的 ID 应当失败")
	}

	rest, ok := RemoveByIDs(hand, ids)
	if !ok || len(rest) != 1 || rest[0].ID != hand[1].ID {
		t.Fatalf("RemoveByIDs 结果不对: ok=%v rest=%+v", ok, rest)
	}
	if len(hand) != 3 {
		t.Fatal("RemoveByIDs 不应修改原手牌")
	}
}
