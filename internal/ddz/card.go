package ddz

import (
	"math/rand"
	"sort"
)

// Rank 是牌点，数值越大越强：3 < 4 < ... < K < A < 2 < 小王 < 大王。
type Rank int

const (
	Rank3          Rank = 3
	Rank4          Rank = 4
	Rank5          Rank = 5
	Rank6          Rank = 6
	Rank7          Rank = 7
	Rank8          Rank = 8
	Rank9          Rank = 9
	Rank10         Rank = 10
	RankJ          Rank = 11
	RankQ          Rank = 12
	RankK          Rank = 13
	RankA          Rank = 14
	Rank2          Rank = 15
	RankJokerSmall Rank = 16
	RankJokerBig   Rank = 17
)

// RankA 及以上（2、大小王）不能参与顺子/连对/飞机。
const maxChainRank = RankA

var rankLabels = map[Rank]string{
	Rank3: "3", Rank4: "4", Rank5: "5", Rank6: "6", Rank7: "7",
	Rank8: "8", Rank9: "9", Rank10: "10", RankJ: "J", RankQ: "Q",
	RankK: "K", RankA: "A", Rank2: "2",
	RankJokerSmall: "w", RankJokerBig: "W",
}

func (r Rank) Label() string {
	if s, ok := rankLabels[r]; ok {
		return s
	}
	return "?"
}

// valid 判断点数是否在 3..大王 之间。
func (r Rank) valid() bool { return r >= Rank3 && r <= RankJokerBig }

// Suit 是花色，斗地主不比较花色，仅用于显示。
type Suit int

const (
	SuitSpade Suit = iota
	SuitHeart
	SuitClub
	SuitDiamond
	SuitJoker
)

var suitSymbols = map[Suit]string{
	SuitSpade:   "♠",
	SuitHeart:   "♥",
	SuitClub:    "♣",
	SuitDiamond: "♦",
	SuitJoker:   "",
}

func (s Suit) Symbol() string {
	if sym, ok := suitSymbols[s]; ok {
		return sym
	}
	return "?"
}

// Card 是一张牌。ID 是 0..53 的唯一编号，客户端只能上报 ID，Host 据此校验。
type Card struct {
	ID   int  `json:"id"`
	Rank Rank `json:"rank"`
	Suit Suit `json:"suit"`
}

// Label 返回可读牌面，如 "A♠" / "10♥" / "w" / "W"。
func (c Card) Label() string {
	if c.Rank == RankJokerSmall || c.Rank == RankJokerBig {
		return c.Rank.Label()
	}
	return c.Rank.Label() + c.Suit.Symbol()
}

// NewDeck 返回一副有序的 54 张牌。
func NewDeck() []Card {
	deck := make([]Card, 0, 54)
	id := 0
	suits := []Suit{SuitSpade, SuitHeart, SuitClub, SuitDiamond}
	for _, s := range suits {
		for r := Rank3; r <= Rank2; r++ {
			deck = append(deck, Card{ID: id, Rank: r, Suit: s})
			id++
		}
	}
	deck = append(deck, Card{ID: id, Rank: RankJokerSmall, Suit: SuitJoker})
	id++
	deck = append(deck, Card{ID: id, Rank: RankJokerBig, Suit: SuitJoker})
	return deck
}

// Shuffle 返回洗好的牌堆。rng 为 nil 时使用默认源。
func Shuffle(rng *rand.Rand) []Card {
	deck := NewDeck()
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	rng.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	return deck
}

// SortDesc 按牌力从大到小排序；同点数时按花色稳定排列，保证编号可复现。
func SortDesc(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank > cards[j].Rank
		}
		return cards[i].Suit < cards[j].Suit
	})
}

// SortAsc 按牌力从小到大排序（用于顺子等连续牌型的可读显示）。
func SortAsc(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank < cards[j].Rank
		}
		return cards[i].Suit < cards[j].Suit
	})
}

// CloneCards 深拷贝一份牌切片，避免调用方持有内部引用。
func CloneCards(cards []Card) []Card {
	out := make([]Card, len(cards))
	copy(out, cards)
	return out
}

// FindByIDs 按 ID 从 cards 中取牌，用于校验客户端上报的出牌。
// 任一 ID 不存在或重复出现都返回 false。
func FindByIDs(cards []Card, ids []int) ([]Card, bool) {
	if len(ids) == 0 {
		return nil, false
	}
	byID := make(map[int]Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}
	picked := make([]Card, 0, len(ids))
	used := make(map[int]bool, len(ids))
	for _, id := range ids {
		if used[id] {
			return nil, false
		}
		c, ok := byID[id]
		if !ok {
			return nil, false
		}
		used[id] = true
		picked = append(picked, c)
	}
	return picked, true
}

// RemoveByIDs 返回从 cards 中移除 ids 后的剩余手牌；任一 ID 不存在时返回 false。
func RemoveByIDs(cards []Card, ids []int) ([]Card, bool) {
	if _, ok := FindByIDs(cards, ids); !ok {
		return nil, false
	}
	drop := make(map[int]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	rest := make([]Card, 0, len(cards))
	for _, c := range cards {
		if !drop[c.ID] {
			rest = append(rest, c)
		}
	}
	return rest, true
}
