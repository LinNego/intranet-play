// Package bj 是 21 点（Blackjack）的纯规则引擎，不依赖网络与界面。
package bj

import (
	"fmt"
	"math/rand"

	"intranet-play/internal/ddz"
)

// 牌模型直接复用斗地主那套：点数、花色、显示完全一致，21 点只是不用大小王。
// 用类型别名而不是重新定义，显示与记法零重复；等第三款牌类游戏出现时再考虑抽公共包。
type (
	Card = ddz.Card
	Rank = ddz.Rank
)

const (
	// Target 是 21 点的目标点数。
	Target = 21
	// DealerStand 是庄家停牌线：点数 >= 17 就停（软 17 也停）。
	DealerStand = 17
	// 黑杰克赔率 3:2。
	blackjackNum = 3
	blackjackDen = 2
)

// NewShoe 返回洗好的 52 张牌（去掉大小王）。rng 为 nil 时使用默认随机源。
func NewShoe(rng *rand.Rand) []Card {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	deck := ddz.NewDeck()
	shoe := make([]Card, 0, 52)
	for _, c := range deck {
		if c.Rank == ddz.RankJokerSmall || c.Rank == ddz.RankJokerBig {
			continue
		}
		shoe = append(shoe, c)
	}
	rng.Shuffle(len(shoe), func(i, j int) { shoe[i], shoe[j] = shoe[j], shoe[i] })
	return shoe
}

// cardValue 是基础点数：A 先记 11（之后可能降为 1），J/Q/K 记 10。
//
// 注意 ddz 的点数编码不是面值：Rank2 的数值是 15，所以必须显式映射，
// 不能直接把 Rank 当数字用。
func cardValue(r Rank) int {
	switch r {
	case ddz.RankA:
		return 11
	case ddz.RankJ, ddz.RankQ, ddz.RankK:
		return 10
	case ddz.Rank2:
		return 2
	case ddz.RankJokerSmall, ddz.RankJokerBig:
		return 0 // 21 点的牌堆里没有王；真混进来也不该被当成大牌
	default:
		return int(r) // 3..10 与面值一致
	}
}

// Score 返回手牌点数与是否为软牌。A 先按 11 算，超过 21 就逐个降为 1。
// 软牌表示"还有一张 A 按 11 算"，例如 A+6 是软 17。
func Score(cards []Card) (int, bool) {
	total := 0
	aces := 0
	for _, c := range cards {
		v := cardValue(c.Rank)
		if c.Rank == ddz.RankA {
			aces++
		}
		total += v
	}
	for total > Target && aces > 0 {
		total -= 10
		aces--
	}
	return total, aces > 0
}

// IsBlackjack 判断是否起手两张即 21 点。
func IsBlackjack(cards []Card) bool {
	if len(cards) != 2 {
		return false
	}
	total, _ := Score(cards)
	return total == Target
}

// IsBust 判断是否爆牌。
func IsBust(cards []Card) bool {
	total, _ := Score(cards)
	return total > Target
}

// Describe 返回手牌的可读描述：黑杰克 / 软 17 点 / 18 点 / 爆牌(22 点)。
func Describe(cards []Card) string {
	if len(cards) == 0 {
		return "无牌"
	}
	if IsBlackjack(cards) {
		return "黑杰克"
	}
	total, soft := Score(cards)
	if total > Target {
		return fmt.Sprintf("爆牌(%d 点)", total)
	}
	if soft {
		return fmt.Sprintf("软 %d 点", total)
	}
	return fmt.Sprintf("%d 点", total)
}
