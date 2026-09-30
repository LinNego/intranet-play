package ddz

import (
	"fmt"
	"strings"
)

// 出牌支持两套写法：
//
//	按牌面：play 55 / play 5556 / play 34567 / play wW / play T J Q K A
//	按编号：play #3 #7 / play #3-#7（编号就是界面上手牌前面的 #n）
//
// 牌面写法用单字符表示点数：3-9 照写，T（或 0）表示 10，J/Q/K/A/2 照写，
// w 是小王、W 是大王（大小王必须区分大小写）。10 也可以单独写成 "10"。
var notationToRank = map[rune]Rank{
	'3': Rank3, '4': Rank4, '5': Rank5, '6': Rank6, '7': Rank7,
	'8': Rank8, '9': Rank9,
	'T': Rank10, 't': Rank10, '0': Rank10,
	'J': RankJ, 'j': RankJ,
	'Q': RankQ, 'q': RankQ,
	'K': RankK, 'k': RankK,
	'A': RankA, 'a': RankA,
	'2': Rank2,
	'w': RankJokerSmall,
	'W': RankJokerBig,
}

// Notation 返回点数的单字符记法（10 写作 T）。
func (r Rank) Notation() string {
	switch r {
	case Rank10:
		return "T"
	case RankJokerSmall:
		return "w"
	case RankJokerBig:
		return "W"
	default:
		return r.Label()
	}
}

// ParseRankToken 解析单个点数记号，额外接受 "10"。
func ParseRankToken(token string) (Rank, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	if token == "10" {
		return Rank10, true
	}
	runes := []rune(token)
	if len(runes) != 1 {
		return 0, false
	}
	r, ok := notationToRank[runes[0]]
	return r, ok
}

// ParseRanks 解析一串点数，支持紧凑写法（34567、55、33344456、wW）
// 与空格分隔写法（10 J Q K A，方便把 10 单独写出来）。
func ParseRanks(text string) ([]Rank, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, fmt.Errorf("没有指定牌面")
	}
	out := make([]Rank, 0, len(fields))
	for _, field := range fields {
		if r, ok := ParseRankToken(field); ok {
			out = append(out, r)
			continue
		}
		for _, ch := range field {
			r, ok := notationToRank[ch]
			if !ok {
				return nil, fmt.Errorf("认不出牌面 %q（3-9、T=10、J Q K A 2、w=小王、W=大王）", string(ch))
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// Notation 把一串牌写成点数记法（升序），如 "34567"。同点数的牌会重复出现，
// 例如对 5 写作 "55"、三带一写作 "5556"。
func Notation(cards []Card) string {
	sorted := CloneCards(cards)
	SortAsc(sorted)
	var b strings.Builder
	for _, c := range sorted {
		b.WriteString(c.Rank.Notation())
	}
	return b.String()
}
