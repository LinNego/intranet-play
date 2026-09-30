package ddz

import (
	"fmt"
	"sort"
)

// ComboKind 是牌型。斗地主共 14 种。
type ComboKind string

const (
	KindSingle       ComboKind = "SINGLE"        // 单张
	KindPair         ComboKind = "PAIR"          // 对子
	KindTriple       ComboKind = "TRIPLE"        // 三张
	KindTripleSingle ComboKind = "TRIPLE_SINGLE" // 三带一
	KindTriplePair   ComboKind = "TRIPLE_PAIR"   // 三带二
	KindStraight     ComboKind = "STRAIGHT"      // 顺子，>=5 张连续单牌
	KindStraightPair ComboKind = "STRAIGHT_PAIR" // 连对，>=3 对连续
	KindPlane        ComboKind = "PLANE"         // 飞机，>=2 个连续三张
	KindPlaneSingle  ComboKind = "PLANE_SINGLE"  // 飞机带单
	KindPlanePair    ComboKind = "PLANE_PAIR"    // 飞机带对
	KindFourTwo      ComboKind = "FOUR_TWO"      // 四带两单
	KindFourTwoPair  ComboKind = "FOUR_TWO_PAIR" // 四带两对
	KindBomb         ComboKind = "BOMB"          // 炸弹
	KindRocket       ComboKind = "ROCKET"        // 王炸
)

var kindLabels = map[ComboKind]string{
	KindSingle:       "单张",
	KindPair:         "对子",
	KindTriple:       "三张",
	KindTripleSingle: "三带一",
	KindTriplePair:   "三带二",
	KindStraight:     "顺子",
	KindStraightPair: "连对",
	KindPlane:        "飞机",
	KindPlaneSingle:  "飞机带单",
	KindPlanePair:    "飞机带对",
	KindFourTwo:      "四带二",
	KindFourTwoPair:  "四带两对",
	KindBomb:         "炸弹",
	KindRocket:       "王炸",
}

func (k ComboKind) Label() string {
	if s, ok := kindLabels[k]; ok {
		return s
	}
	return string(k)
}

// Combo 是识别出的牌型。
type Combo struct {
	Kind  ComboKind `json:"kind"`
	Main  Rank      `json:"main"`  // 主牌点数；顺子/连对/飞机取最大张
	Size  int       `json:"size"`  // 总张数
	Chain int       `json:"chain"` // 连张数（顺子长度 / 对子数 / 三张数），非连牌为 1
}

// Valid 表示这是一个已识别出的合法牌型。
func (c Combo) Valid() bool { return c.Kind != "" }

// String 用于日志，如 "炸弹(K)"。
func (c Combo) String() string {
	if !c.Valid() {
		return "非法牌型"
	}
	return fmt.Sprintf("%s(%s)", c.Kind.Label(), c.Main.Label())
}

// Beats 判断 c 能否压过 other（other 为合法牌型）。
func (c Combo) Beats(other Combo) bool {
	if !c.Valid() || !other.Valid() {
		return false
	}
	if c.Kind == KindRocket {
		return other.Kind != KindRocket // 王炸不能压王炸
	}
	if other.Kind == KindRocket {
		return false
	}
	if c.Kind == KindBomb {
		if other.Kind == KindBomb {
			return c.Main > other.Main
		}
		return true // 炸弹压一切普通牌型
	}
	if other.Kind == KindBomb {
		return false
	}
	if c.Kind != other.Kind || c.Size != other.Size {
		return false
	}
	return c.Main > other.Main
}

// Classify 识别牌型。任何一张牌点数非法或组合不成立都返回 false。
func Classify(cards []Card) (Combo, bool) {
	n := len(cards)
	if n == 0 {
		return Combo{}, false
	}
	for _, c := range cards {
		if !c.Rank.valid() {
			return Combo{}, false
		}
	}
	counts, ranks := rankCounts(cards)

	// 王炸必须最先判：它也是 2 张牌。
	if n == 2 && counts[RankJokerSmall] == 1 && counts[RankJokerBig] == 1 {
		return Combo{Kind: KindRocket, Main: RankJokerBig, Size: 2, Chain: 1}, true
	}
	// 炸弹要先于"三带一"判定，否则四张同点会被误判成三带一。
	if n == 4 && len(ranks) == 1 {
		return Combo{Kind: KindBomb, Main: ranks[0], Size: 4, Chain: 1}, true
	}
	if n == 1 {
		return Combo{Kind: KindSingle, Main: ranks[0], Size: 1, Chain: 1}, true
	}
	if n == 2 && len(ranks) == 1 {
		return Combo{Kind: KindPair, Main: ranks[0], Size: 2, Chain: 1}, true
	}
	if n == 3 && len(ranks) == 1 {
		return Combo{Kind: KindTriple, Main: ranks[0], Size: 3, Chain: 1}, true
	}
	if n == 4 {
		if trio := ranksWithCount(counts, 3); len(trio) == 1 {
			return Combo{Kind: KindTripleSingle, Main: trio[0], Size: 4, Chain: 1}, true
		}
	}
	if n == 5 {
		if trio := ranksWithCount(counts, 3); len(trio) == 1 {
			if pair := ranksWithCount(counts, 2); len(pair) == 1 && pair[0] != trio[0] {
				return Combo{Kind: KindTriplePair, Main: trio[0], Size: 5, Chain: 1}, true
			}
		}
	}
	// 顺子：>=5 张，全部单张，点数连续且不含 2/王。
	if n >= 5 && len(ranks) == n && consecutive(ranks) && chainable(ranks) {
		return Combo{Kind: KindStraight, Main: ranks[len(ranks)-1], Size: n, Chain: n}, true
	}
	// 连对：>=3 对，全部对子，点数连续且不含 2/王。
	if n >= 6 && n%2 == 0 && len(ranks) == n/2 && allCounts(counts, 2) && consecutive(ranks) && chainable(ranks) {
		return Combo{Kind: KindStraightPair, Main: ranks[len(ranks)-1], Size: n, Chain: len(ranks)}, true
	}
	// 飞机（不带翅膀）：>=2 个连续三张。
	if n >= 6 && n%3 == 0 && len(ranks) == n/3 && allCounts(counts, 3) && consecutive(ranks) && chainable(ranks) {
		return Combo{Kind: KindPlane, Main: ranks[len(ranks)-1], Size: n, Chain: len(ranks)}, true
	}
	if c, ok := classifyPlaneWithWings(counts, n); ok {
		return c, true
	}
	if c, ok := classifyFourWithWings(counts, n); ok {
		return c, true
	}
	return Combo{}, false
}

// classifyPlaneWithWings 处理"飞机带单"与"飞机带对"。
// 规则：三张链点数必须连续且 <= A；翅膀点数与链不重叠，带对时每个翅膀点数恰好一对且互不相同。
func classifyPlaneWithWings(counts map[Rank]int, n int) (Combo, bool) {
	triples := make([]Rank, 0, len(counts))
	for r, c := range counts {
		if c >= 3 && r <= maxChainRank {
			triples = append(triples, r)
		}
	}
	if len(triples) < 2 {
		return Combo{}, false
	}
	sortRanks(triples)

	for _, run := range consecutiveRuns(triples) {
		for k := len(run); k >= 2; k-- {
			for start := 0; start+k <= len(run); start++ {
				chain := run[start : start+k]
				if n == 4*k {
					if c, ok := wingsSingle(counts, chain, k); ok {
						return c, true
					}
				}
				if n == 5*k {
					if c, ok := wingsPair(counts, chain, k); ok {
						return c, true
					}
				}
			}
		}
	}
	return Combo{}, false
}

// wingsSingle 检查"飞机带单"的翅膀：链外恰好剩 k 张，且不含四张同点（炸弹不能当翅膀）。
func wingsSingle(counts map[Rank]int, chain []Rank, k int) (Combo, bool) {
	rest, total := wingCounts(counts, chain)
	if total != k {
		return Combo{}, false
	}
	for _, c := range rest {
		if c >= 4 {
			return Combo{}, false
		}
	}
	return Combo{Kind: KindPlaneSingle, Main: chain[len(chain)-1], Size: 4 * k, Chain: k}, true
}

// wingsPair 检查"飞机带对"的翅膀：链外恰好是 k 个互不相同点数的对子。
func wingsPair(counts map[Rank]int, chain []Rank, k int) (Combo, bool) {
	rest, total := wingCounts(counts, chain)
	if total != 2*k || len(rest) != k {
		return Combo{}, false
	}
	for _, c := range rest {
		if c != 2 {
			return Combo{}, false
		}
	}
	return Combo{Kind: KindPlanePair, Main: chain[len(chain)-1], Size: 5 * k, Chain: k}, true
}

// wingCounts 返回链外各点数的张数与总张数。链上点数的牌一律不算翅膀。
func wingCounts(counts map[Rank]int, chain []Rank) (map[Rank]int, int) {
	inChain := make(map[Rank]bool, len(chain))
	for _, r := range chain {
		inChain[r] = true
	}
	rest := make(map[Rank]int, len(counts))
	total := 0
	for r, c := range counts {
		if inChain[r] {
			continue
		}
		rest[r] = c
		total += c
	}
	return rest, total
}

// classifyFourWithWings 处理"四带两单"(6 张) 与"四带两对"(8 张)。
// 不处理 {4,4}（两个炸弹），那不能当四带两对。
func classifyFourWithWings(counts map[Rank]int, n int) (Combo, bool) {
	if n != 6 && n != 8 {
		return Combo{}, false
	}
	quads := ranksWithCount(counts, 4)
	if len(quads) != 1 {
		return Combo{}, false
	}
	quad := quads[0]

	rest := make([]int, 0, len(counts))
	total := 0
	for r, c := range counts {
		if r == quad {
			continue
		}
		rest = append(rest, c)
		total += c
	}

	if n == 6 {
		// 两张带牌可以是任意两张（含同点的一对）。
		if total != 2 {
			return Combo{}, false
		}
		return Combo{Kind: KindFourTwo, Main: quad, Size: 6, Chain: 1}, true
	}
	// n == 8：必须是两个不同点数的对子。
	if total != 4 || len(rest) != 2 {
		return Combo{}, false
	}
	for _, c := range rest {
		if c != 2 {
			return Combo{}, false
		}
	}
	return Combo{Kind: KindFourTwoPair, Main: quad, Size: 8, Chain: 1}, true
}

// --- 小工具 ---

func rankCounts(cards []Card) (map[Rank]int, []Rank) {
	counts := make(map[Rank]int, len(cards))
	for _, c := range cards {
		counts[c.Rank]++
	}
	ranks := make([]Rank, 0, len(counts))
	for r := range counts {
		ranks = append(ranks, r)
	}
	sortRanks(ranks)
	return counts, ranks
}

func sortRanks(ranks []Rank) {
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] < ranks[j] })
}

// ranksWithCount 返回恰好出现 n 次的点数（升序）。n 张同点即精确匹配。
func ranksWithCount(counts map[Rank]int, n int) []Rank {
	out := make([]Rank, 0, len(counts))
	for r, c := range counts {
		if c == n {
			out = append(out, r)
		}
	}
	sortRanks(out)
	return out
}

func allCounts(counts map[Rank]int, n int) bool {
	for _, c := range counts {
		if c != n {
			return false
		}
	}
	return true
}

// consecutive 判断已升序去重的点数是否严格连续。
func consecutive(ranks []Rank) bool {
	for i := 1; i < len(ranks); i++ {
		if ranks[i] != ranks[i-1]+1 {
			return false
		}
	}
	return true
}

// chainable 判断点数是否都能进顺子/连对/飞机（即不含 2 与大小王）。
func chainable(ranks []Rank) bool {
	for _, r := range ranks {
		if r > maxChainRank {
			return false
		}
	}
	return true
}

// consecutiveRuns 把升序去重的点数切成若干"长度 >= 2 的连续段"。
func consecutiveRuns(ranks []Rank) [][]Rank {
	var runs [][]Rank
	start := 0
	for i := 1; i <= len(ranks); i++ {
		if i == len(ranks) || ranks[i] != ranks[i-1]+1 {
			if i-start >= 2 {
				run := make([]Rank, i-start)
				copy(run, ranks[start:i])
				runs = append(runs, run)
			}
			start = i
		}
	}
	return runs
}
