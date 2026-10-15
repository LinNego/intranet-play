// Package zjh 是炸金花（三张牌）的纯规则引擎，不依赖网络与界面。
package zjh

import (
	"fmt"
	"sort"
	"strings"

	"intranet-play/internal/ddz"
)

// 牌模型复用斗地主那套（点数/花色/显示一致），用类型别名避免重复实现。
type (
	Card = ddz.Card
	Rank = ddz.Rank
)

// HandKind 是三张牌的牌型，从大到小。
type HandKind string

const (
	KindBaozi       HandKind = "BAOZI"       // 豹子：三张同点
	KindTonghuaShun HandKind = "TONGHUASHUN" // 同花顺：同花且连续
	KindJinhua      HandKind = "JINHUA"      // 金花：同花
	KindShunzi      HandKind = "SHUNZI"      // 顺子：连续
	KindDuizi       HandKind = "DUIZI"       // 对子
	KindSanpai      HandKind = "SANPAI"      // 散牌
)

var kindOrder = map[HandKind]int{
	KindSanpai:      0,
	KindDuizi:       1,
	KindShunzi:      2,
	KindJinhua:      3,
	KindTonghuaShun: 4,
	KindBaozi:       5,
}

var kindLabels = map[HandKind]string{
	KindBaozi:       "豹子",
	KindTonghuaShun: "同花顺",
	KindJinhua:      "金花",
	KindShunzi:      "顺子",
	KindDuizi:       "对子",
	KindSanpai:      "散牌",
}

func (k HandKind) Label() string {
	if s, ok := kindLabels[k]; ok {
		return s
	}
	return string(k)
}

// Hand 是识别出的三张牌：先比 Kind，再按 Key 从大到小逐个比。
// 花色不参与比较——完全一样大时 Compare 返回 0，由比牌规则裁决。
type Hand struct {
	Kind HandKind `json:"kind"`
	Key  []Rank   `json:"key"`
}

func (h Hand) Valid() bool { return h.Kind != "" }

func (h Hand) Label() string {
	if !h.Valid() {
		return "无牌"
	}
	return h.Kind.Label()
}

// pokerValue 把 ddz 的点数编码换算成 2..14 的比较值（A=14，2=2）。
// ddz 里 Rank2 的数值是 15，不能直接当数字用。
func pokerValue(r Rank) int {
	switch r {
	case ddz.RankA:
		return 14
	case ddz.Rank2:
		return 2
	case ddz.RankJ:
		return 11
	case ddz.RankQ:
		return 12
	case ddz.RankK:
		return 13
	default:
		return int(r) // 3..10
	}
}

// rankByValue 把 pokerValue 换回点数，用于 A23 这种按"3 最大"处理的顺子。
func rankByValue(v int) Rank {
	switch v {
	case 14:
		return ddz.RankA
	case 2:
		return ddz.Rank2
	case 11:
		return ddz.RankJ
	case 12:
		return ddz.RankQ
	case 13:
		return ddz.RankK
	default:
		return Rank(v)
	}
}

// Evaluate 识别三张牌的牌型。张数不对返回零值（Valid() == false）。
func Evaluate(cards []Card) Hand {
	if len(cards) != 3 {
		return Hand{}
	}
	values := []int{pokerValue(cards[0].Rank), pokerValue(cards[1].Rank), pokerValue(cards[2].Rank)}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))

	sameSuit := cards[0].Suit == cards[1].Suit && cards[1].Suit == cards[2].Suit
	sameRank := values[0] == values[1] && values[1] == values[2]

	if sameRank {
		return Hand{Kind: KindBaozi, Key: []Rank{rankByValue(values[0])}}
	}

	top, straight := straightTop(values)
	switch {
	case straight && sameSuit:
		return Hand{Kind: KindTonghuaShun, Key: []Rank{rankByValue(top)}}
	case sameSuit:
		return Hand{Kind: KindJinhua, Key: toRanks(values)}
	case straight:
		return Hand{Kind: KindShunzi, Key: []Rank{rankByValue(top)}}
	}

	// 对子：一对 + 单张（values 已降序，成对的一定在前面）
	if values[0] == values[1] {
		return Hand{Kind: KindDuizi, Key: []Rank{rankByValue(values[0]), rankByValue(values[2])}}
	}
	if values[1] == values[2] {
		return Hand{Kind: KindDuizi, Key: []Rank{rankByValue(values[1]), rankByValue(values[0])}}
	}
	return Hand{Kind: KindSanpai, Key: toRanks(values)}
}

// straightTop 判断三张是否连续，返回比较用的"最大张"点数。
// A23 视为顺子且最小（按 3 最大处理），QKA 最大。
func straightTop(values []int) (int, bool) {
	// values 已降序
	if values[0]-1 == values[1] && values[1]-1 == values[2] {
		return values[0], true
	}
	// A(14) 当 1 用：A23 → 2,3,14
	if values[0] == 14 && values[1] == 3 && values[2] == 2 {
		return 3, true
	}
	return 0, false
}

func toRanks(values []int) []Rank {
	out := make([]Rank, 0, len(values))
	for _, v := range values {
		out = append(out, rankByValue(v))
	}
	return out
}

// Compare 比较两手牌：a 大返回 1，b 大返回 -1，完全一样返回 0。
// 花色不参与比较，所以"一样大"是可能的。
func Compare(a, b Hand) int {
	if !a.Valid() || !b.Valid() {
		return 0
	}
	if ka, kb := kindOrder[a.Kind], kindOrder[b.Kind]; ka != kb {
		if ka > kb {
			return 1
		}
		return -1
	}
	for i := 0; i < len(a.Key) && i < len(b.Key); i++ {
		va, vb := pokerValue(a.Key[i]), pokerValue(b.Key[i])
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Describe 返回可读描述，如 "金花（A♠ K♥ 7♦）"，用于日志与界面。
func Describe(cards []Card) string {
	h := Evaluate(cards)
	if !h.Valid() {
		return "无牌"
	}
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		parts = append(parts, c.Label())
	}
	return fmt.Sprintf("%s（%s）", h.Label(), strings.Join(parts, " "))
}
