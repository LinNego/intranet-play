package ddz

import (
	"strings"
	"testing"
)

func TestParseRanks(t *testing.T) {
	cases := []struct {
		text string
		want []Rank
		bad  bool
	}{
		{text: "5", want: []Rank{Rank5}},
		{text: "55", want: []Rank{Rank5, Rank5}},
		{text: "34567", want: []Rank{Rank3, Rank4, Rank5, Rank6, Rank7}},
		{text: "T", want: []Rank{Rank10}},
		{text: "0", want: []Rank{Rank10}},
		{text: "10", want: []Rank{Rank10}},
		{text: "10 J Q K A", want: []Rank{Rank10, RankJ, RankQ, RankK, RankA}},
		{text: "TJQKA", want: []Rank{Rank10, RankJ, RankQ, RankK, RankA}},
		{text: "wW", want: []Rank{RankJokerSmall, RankJokerBig}},
		{text: "2", want: []Rank{Rank2}},
		{text: "33344456", want: []Rank{Rank3, Rank3, Rank3, Rank4, Rank4, Rank4, Rank5, Rank6}},
		{text: " j j ", want: []Rank{RankJ, RankJ}},
		{text: "", bad: true},
		{text: "1", bad: true},
		{text: "X", bad: true},
		{text: "3X", bad: true},
		{text: "1010", bad: true},
	}

	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseRanks(tc.text)
			if tc.bad {
				if err == nil {
					t.Fatalf("ParseRanks(%q) 应当报错，实际得到 %v", tc.text, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRanks(%q) 报错: %v", tc.text, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseRanks(%q) = %v, 期望 %v", tc.text, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("ParseRanks(%q) = %v, 期望 %v", tc.text, got, tc.want)
				}
			}
		})
	}
}

// TestNotationRoundTrip 保证"渲染出来的写法一定能被解析回同一手牌"，
// 这样 hint 给出的写法可以直接复制粘贴执行。
func TestNotationRoundTrip(t *testing.T) {
	hands := [][]string{
		{"5"},
		{"5", "5"},
		{"5", "5", "5", "6"},
		{"3", "4", "5", "6", "7"},
		{"10", "J", "Q", "K", "A"},
		{"3", "3", "4", "4", "5", "5"},
		{"3", "3", "3", "4", "4", "4", "5", "6"},
		{"K", "K", "K", "K"},
		{"w", "W"},
		{"3", "3", "3", "3", "5", "5", "6", "6"},
	}
	for _, labels := range hands {
		cards := mustCards(t, labels...)
		text := Notation(cards)
		ranks, err := ParseRanks(text)
		if err != nil {
			t.Fatalf("Notation(%v) = %q 解析失败: %v", labels, text, err)
		}
		if len(ranks) != len(cards) {
			t.Fatalf("%q 解析出 %d 张，原牌 %d 张", text, len(ranks), len(cards))
		}
		want := CloneCards(cards)
		SortAsc(want)
		for i := range want {
			if ranks[i] != want[i].Rank {
				t.Fatalf("%q 第 %d 张 = %s, 期望 %s", text, i, ranks[i].Label(), want[i].Rank.Label())
			}
		}
	}
}

func TestHintsFreeLead(t *testing.T) {
	hand := mustCards(t, "5", "5", "5", "6")

	hints := Hints(hand, nil)
	got := hintNotations(hints)

	for _, want := range []string{"5", "55", "555", "6", "5556"} {
		if !got[want] {
			t.Fatalf("提示里缺少 %q，实际: %v", want, hintKeys(hints))
		}
	}
	// 5556 必须是三带一，不能被当成别的
	for _, h := range hints {
		if h.Notation() == "5556" && h.Combo.Kind != KindTripleSingle {
			t.Fatalf("5556 应当是三带一，实际 %s", h.Combo)
		}
	}
}

func TestHintsMustBeatTarget(t *testing.T) {
	// 手里只有比 9 小的单张，压不过 9，应当没有任何提示
	hand := mustCards(t, "3", "4", "5", "6", "7")
	target := Combo{Kind: KindSingle, Main: Rank9, Size: 1, Chain: 1}
	if hints := Hints(hand, &target); len(hints) != 0 {
		t.Fatalf("压不过 9 时不该有提示，实际: %v", hintKeys(hints))
	}

	// 加上一张 10 就只有它压得过
	hand = mustCards(t, "3", "4", "5", "6", "7", "10")
	hints := Hints(hand, &target)
	if len(hints) != 1 || hints[0].Notation() != "T" {
		t.Fatalf("应当只有单张 T 能压过 9，实际: %v", hintKeys(hints))
	}
}

func TestHintsBombAndRocket(t *testing.T) {
	hand := mustCards(t, "5", "5", "5", "5", "w", "W", "3", "4")
	straight := Combo{Kind: KindStraight, Main: Rank7, Size: 5, Chain: 5}

	hints := Hints(hand, &straight)
	got := hintNotations(hints)
	if !got["5555"] {
		t.Fatalf("顺子应当被炸弹压过，实际: %v", hintKeys(hints))
	}
	if !got["wW"] {
		t.Fatalf("顺子应当被王炸压过，实际: %v", hintKeys(hints))
	}
	if got["3"] || got["4"] {
		t.Fatalf("单张压不过顺子，不该出现: %v", hintKeys(hints))
	}

	// 王炸只能被更大的……没有比王炸更大的，所以对王炸没有任何提示
	rocket := Combo{Kind: KindRocket, Main: RankJokerBig, Size: 2, Chain: 1}
	if hints := Hints(hand, &rocket); len(hints) != 0 {
		t.Fatalf("没有牌能压过王炸，实际: %v", hintKeys(hints))
	}
}

func TestHintsChainsAndPlanes(t *testing.T) {
	hand := mustCards(t, "3", "4", "5", "6", "7", "8", "3", "3", "4", "4", "4", "5", "5", "5")

	hints := Hints(hand, nil)
	got := hintNotations(hints)

	for _, want := range []string{"34567", "345678", "334455", "333444", "33344467", "5555"} {
		if !got[want] {
			t.Fatalf("提示里缺少 %q，实际: %v", want, hintKeys(hints))
		}
	}
	// 飞机带对要求"两个不同点数的对子"当翅膀：这手牌除了三张链只剩 5 是对子（而且是 4 张），
	// 凑不出第二个对子，所以不该出现 33344455。
	if got["33344455"] {
		t.Fatalf("翅膀只有一个点数成对，不该给出 33344455: %v", hintKeys(hints))
	}

	// 顺子不能含 2 和王，也不能短于 5 张
	for _, h := range hints {
		if h.Combo.Kind == KindStraight && (h.Combo.Chain < 5 || h.Combo.Main > maxChainRank) {
			t.Fatalf("非法顺子提示: %s %s", h.Combo, h.Notation())
		}
	}
}

// TestHintsPlaneWithPairs 单独验证飞机带对：链外确实有两个不同点数的对子时应当给出。
func TestHintsPlaneWithPairs(t *testing.T) {
	hand := mustCards(t, "3", "3", "3", "4", "4", "4", "6", "6", "7", "7")

	got := hintNotations(Hints(hand, nil))
	if !got["3334446677"] {
		t.Fatalf("应当给出飞机带对 3334446677，实际: %v", hintKeys(Hints(hand, nil)))
	}
}

// TestHintsCardsBelongToHand 提示里的牌必须真的在手里，且不能重复用同一张。
func TestHintsCardsBelongToHand(t *testing.T) {
	hand := mustCards(t, "3", "3", "3", "4", "4", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A", "2", "w", "W")
	owned := map[int]bool{}
	for _, c := range hand {
		owned[c.ID] = true
	}

	hints := Hints(hand, nil)
	if len(hints) == 0 {
		t.Fatal("自由出牌权下应当有提示")
	}
	for _, h := range hints {
		used := map[int]bool{}
		for _, c := range h.Cards {
			if !owned[c.ID] {
				t.Fatalf("提示 %s 用到了手里没有的牌 id=%d", h.Notation(), c.ID)
			}
			if used[c.ID] {
				t.Fatalf("提示 %s 重复使用了 id=%d", h.Notation(), c.ID)
			}
			used[c.ID] = true
		}
		if _, ok := Classify(h.Cards); !ok {
			t.Fatalf("提示 %s 不是合法牌型", h.Notation())
		}
	}
}

// TestHintsSortedCheapFirst 便宜的普通牌型要排在炸弹前面，便于玩家优先出小牌。
func TestHintsSortedCheapFirst(t *testing.T) {
	hand := mustCards(t, "5", "5", "5", "5", "6")
	hints := Hints(hand, nil)
	if len(hints) < 3 {
		t.Fatalf("提示太少: %v", hintKeys(hints))
	}
	if hints[0].Combo.Kind != KindSingle {
		t.Fatalf("第一条提示应当是单张，实际 %s", hints[0].Combo)
	}
	last := hints[len(hints)-1]
	if last.Combo.Kind != KindBomb && last.Combo.Kind != KindRocket {
		t.Fatalf("最后一条提示应当是炸弹/王炸，实际 %s", last.Combo)
	}
}

func hintNotations(hints []Hint) map[string]bool {
	out := make(map[string]bool, len(hints))
	for _, h := range hints {
		out[h.Notation()] = true
	}
	return out
}

func hintKeys(hints []Hint) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, h.Notation())
	}
	return strings.Join(parts, " ")
}
