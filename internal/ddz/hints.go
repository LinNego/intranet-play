package ddz

import "sort"

// Hint 是一种能出的牌，Notation 就是可以直接敲进命令行的写法。
type Hint struct {
	Cards []Card
	Combo Combo
}

func (h Hint) Notation() string { return Notation(h.Cards) }

// hintOrder 决定提示的展示顺序：便宜的普通牌型在前，炸弹与王炸放最后。
var hintOrder = []ComboKind{
	KindSingle, KindPair, KindTriple, KindTripleSingle, KindTriplePair,
	KindStraight, KindStraightPair, KindPlane, KindPlaneSingle, KindPlanePair,
	KindFourTwo, KindFourTwoPair, KindBomb, KindRocket,
}

func hintPriority(k ComboKind) int {
	for i, kk := range hintOrder {
		if kk == k {
			return i
		}
	}
	return len(hintOrder)
}

// Hints 列出手里所有能压过 target 的出法；target 为 nil 时表示自由出牌权，
// 列出所有可以主动出的牌型。带翅膀的牌型固定取最小的翅膀，避免同一个牌型刷屏。
// 结果已按"先便宜后昂贵"排序并去重。
func Hints(hand []Card, target *Combo) []Hint {
	if len(hand) == 0 {
		return nil
	}
	groups := groupByRank(hand)
	out := make([]Hint, 0, 16)
	seen := make(map[string]bool, 16)

	add := func(cards []Card) {
		if len(cards) == 0 {
			return
		}
		combo, ok := Classify(cards)
		if !ok {
			return
		}
		if target != nil && !combo.Beats(*target) {
			return
		}
		key := string(combo.Kind) + ":" + Notation(cards)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Hint{Cards: CloneCards(cards), Combo: combo})
	}

	// 单张 / 对子 / 三张 / 炸弹
	for r := Rank3; r <= RankJokerBig; r++ {
		cards := groups[r]
		for n := 1; n <= len(cards); n++ {
			add(cards[:n])
		}
	}

	// 王炸
	small, big := groups[RankJokerSmall], groups[RankJokerBig]
	if len(small) > 0 && len(big) > 0 {
		add([]Card{small[0], big[0]})
	}

	// 三带一 / 三带二 / 四带两单 / 四带两对
	for r := Rank3; r <= Rank2; r++ {
		cards := groups[r]
		if len(cards) >= 3 {
			exclude := map[Rank]bool{r: true}
			if k := cheapestKickers(groups, exclude, 1, 1); k != nil {
				add(concat(cards[:3], k))
			}
			if k := cheapestKickers(groups, exclude, 2, 1); k != nil {
				add(concat(cards[:3], k))
			}
		}
		if len(cards) == 4 {
			exclude := map[Rank]bool{r: true}
			if k := cheapestKickers(groups, exclude, 1, 2); k != nil {
				add(concat(cards[:4], k))
			}
			if k := cheapestKickers(groups, exclude, 2, 2); k != nil {
				add(concat(cards[:4], k))
			}
		}
	}

	// 顺子（>=5 张）
	for length := 5; length <= 12; length++ {
		forEachChainStart(length, func(start Rank) {
			if cards, ok := chainCards(groups, start, length, 1); ok {
				add(cards)
			}
		})
	}

	// 连对（>=3 对）
	for length := 3; length <= 10; length++ {
		forEachChainStart(length, func(start Rank) {
			if cards, ok := chainCards(groups, start, length, 2); ok {
				add(cards)
			}
		})
	}

	// 飞机 / 飞机带单 / 飞机带对（>=2 个连续三张）
	for length := 2; length <= 6; length++ {
		forEachChainStart(length, func(start Rank) {
			cards, ok := chainCards(groups, start, length, 3)
			if !ok {
				return
			}
			add(cards)
			exclude := map[Rank]bool{}
			for i := 0; i < length; i++ {
				exclude[start+Rank(i)] = true
			}
			if k := cheapestKickers(groups, exclude, 1, length); k != nil {
				add(concat(cards, k))
			}
			if k := cheapestKickers(groups, exclude, 2, length); k != nil {
				add(concat(cards, k))
			}
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := hintPriority(out[i].Combo.Kind), hintPriority(out[j].Combo.Kind)
		if pi != pj {
			return pi < pj
		}
		if out[i].Combo.Size != out[j].Combo.Size {
			return out[i].Combo.Size < out[j].Combo.Size
		}
		return out[i].Combo.Main < out[j].Combo.Main
	})
	return out
}

func groupByRank(hand []Card) map[Rank][]Card {
	groups := make(map[Rank][]Card, len(hand))
	for _, c := range hand {
		groups[c.Rank] = append(groups[c.Rank], c)
	}
	return groups
}

// cheapestKickers 从链外挑 count 个点数、每个点数取 perRank 张，尽量挑小的，
// 并且尽量不拆炸弹（只有实在凑不出来才动炸弹）。
func cheapestKickers(groups map[Rank][]Card, exclude map[Rank]bool, perRank, count int) []Card {
	pick := func(skipBomb bool) []Card {
		out := make([]Card, 0, perRank*count)
		got := 0
		for r := Rank3; r <= RankJokerBig && got < count; r++ {
			if exclude[r] {
				continue
			}
			cards := groups[r]
			if len(cards) < perRank {
				continue
			}
			if skipBomb && len(cards) == 4 {
				continue
			}
			out = append(out, cards[:perRank]...)
			got++
		}
		if got < count {
			return nil
		}
		return out
	}
	if k := pick(true); k != nil {
		return k
	}
	return pick(false)
}

func chainCards(groups map[Rank][]Card, start Rank, length, perRank int) ([]Card, bool) {
	out := make([]Card, 0, length*perRank)
	for i := 0; i < length; i++ {
		cards := groups[start+Rank(i)]
		if len(cards) < perRank {
			return nil, false
		}
		out = append(out, cards[:perRank]...)
	}
	return out, true
}

// forEachChainStart 枚举所有能放下 length 连张的起点（连张不能含 2 和王）。
func forEachChainStart(length int, fn func(start Rank)) {
	for start := Rank3; int(start)+length-1 <= int(maxChainRank); start++ {
		fn(start)
	}
}

func concat(a, b []Card) []Card {
	out := make([]Card, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
