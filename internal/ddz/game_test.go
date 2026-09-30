package ddz

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	return NewGame(rand.New(rand.NewSource(20240517)), 0, 1)
}

// dealSpec 从同一副牌里给三家发指定牌面，保证 ID 全局不重复。
func dealSpec(t *testing.T, specs [3][]string) [Seats][]Card {
	t.Helper()
	deck := NewDeck()
	used := make(map[int]bool, 54)
	var out [Seats][]Card
	for seat, spec := range specs {
		for _, label := range spec {
			found := false
			for _, c := range deck {
				if used[c.ID] || c.Rank.Label() != label {
					continue
				}
				used[c.ID] = true
				out[seat] = append(out[seat], c)
				found = true
				break
			}
			if !found {
				t.Fatalf("牌堆里取不出 %q", label)
			}
		}
		SortDesc(out[seat])
	}
	return out
}

// craftPlaying 直接把对局摆到出牌阶段，用于精确验证结算逻辑。
func craftPlaying(t *testing.T, g *Game, specs [3][]string, landlord, turn, bidScore int) {
	t.Helper()
	g.Hands = dealSpec(t, specs)
	g.Landlord = landlord
	g.Phase = PhasePlaying
	g.Turn = turn
	g.BidScore = bidScore
	g.Multiplier = 1
	g.LastPlay = nil
	g.passCount = 0
	g.PassSeat = [Seats]bool{}
	g.PlayCount = [Seats]int{}
}

func ids(cards []Card) []int {
	out := make([]int, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

func TestNewGameDeals(t *testing.T) {
	g := newTestGame(t)
	if g.Phase != PhaseBidding {
		t.Fatalf("初始阶段 = %s, 期望 %s", g.Phase, PhaseBidding)
	}
	if g.Landlord != -1 {
		t.Fatalf("初始地主 = %d, 期望 -1", g.Landlord)
	}
	if g.Turn != 0 {
		t.Fatalf("初始叫分座位 = %d, 期望 0", g.Turn)
	}
	if len(g.Bottom) != BottomSize {
		t.Fatalf("底牌 = %d 张, 期望 %d", len(g.Bottom), BottomSize)
	}

	all := make(map[int]bool, 54)
	for seat := 0; seat < Seats; seat++ {
		if len(g.Hands[seat]) != HandSize {
			t.Fatalf("座位 %d 手牌 = %d 张, 期望 %d", seat, len(g.Hands[seat]), HandSize)
		}
		for _, c := range g.Hands[seat] {
			if all[c.ID] {
				t.Fatalf("手牌 ID 重复: %d", c.ID)
			}
			all[c.ID] = true
		}
	}
	for _, c := range g.Bottom {
		if all[c.ID] {
			t.Fatalf("底牌与手牌重复: %d", c.ID)
		}
		all[c.ID] = true
	}
	if len(all) != 54 {
		t.Fatalf("三家手牌 + 底牌共 %d 张, 期望 54", len(all))
	}
}

func TestBidThreeBecomesLandlordImmediately(t *testing.T) {
	g := newTestGame(t)
	events, err := g.Apply(0, Action{Kind: ActBid, Bid: 3})
	if err != nil {
		t.Fatalf("叫 3 分失败: %v", err)
	}
	if g.Landlord != 0 {
		t.Fatalf("地主 = %d, 期望 0", g.Landlord)
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhasePlaying)
	}
	if got := len(g.Hands[0]); got != HandSize+BottomSize {
		t.Fatalf("地主手牌 = %d 张, 期望 %d（含底牌）", got, HandSize+BottomSize)
	}
	if g.Turn != 0 {
		t.Fatalf("出牌首个回合 = %d, 期望地主 0", g.Turn)
	}
	if len(events) == 0 {
		t.Fatal("定地主应当产生事件")
	}
}

func TestBidMustBeHigher(t *testing.T) {
	g := newTestGame(t)
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 2}); err != nil {
		t.Fatalf("叫 2 分失败: %v", err)
	}
	if _, err := g.Apply(1, Action{Kind: ActBid, Bid: 2}); err == nil {
		t.Fatal("平叫应当被拒")
	}
	if _, err := g.Apply(1, Action{Kind: ActBid, Bid: 1}); err == nil {
		t.Fatal("低叫应当被拒")
	}
	if _, err := g.Apply(1, Action{Kind: ActBid, Bid: 3}); err != nil {
		t.Fatalf("叫 3 分失败: %v", err)
	}
	if g.Landlord != 1 {
		t.Fatalf("地主 = %d, 期望 1", g.Landlord)
	}
}

func TestAllPassRedealsThenDrawsRound(t *testing.T) {
	g := newTestGame(t)
	allPass := func() {
		for i := 0; i < Seats && g.Phase == PhaseBidding; i++ {
			if _, err := g.Apply(g.Turn, Action{Kind: ActBid, Bid: 0}); err != nil {
				t.Fatalf("不叫失败: %v", err)
			}
		}
	}
	for i := 0; i < maxRedeal+2 && g.Phase == PhaseBidding; i++ {
		allPass()
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("连续不叫后阶段 = %s, 期望 %s", g.Phase, PhaseRoundEnd)
	}
	if g.Redeal != maxRedeal {
		t.Fatalf("重发次数 = %d, 期望 %d", g.Redeal, maxRedeal)
	}
	if len(g.Hands[0]) != HandSize {
		t.Fatalf("流局后手牌 = %d 张, 期望 %d", len(g.Hands[0]), HandSize)
	}
}

func TestTurnAndOwnershipValidation(t *testing.T) {
	g := newTestGame(t)
	if _, err := g.Apply(1, Action{Kind: ActBid, Bid: 1}); err == nil {
		t.Fatal("非当前座位叫分应当被拒")
	}
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 4}); err == nil {
		t.Fatal("叫 4 分应当被拒")
	}
	if _, err := g.Apply(3, Action{Kind: ActBid, Bid: 1}); err == nil {
		t.Fatal("非法座位应当被拒")
	}
	g.Online[0] = false
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 1}); err == nil {
		t.Fatal("离线座位应当被拒")
	}
}

func TestPlayValidation(t *testing.T) {
	g := newTestGame(t)
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 3}); err != nil {
		t.Fatalf("叫 3 分失败: %v", err)
	}
	hand := g.Hands[0]

	// 不是自己的牌
	other := g.Hands[1][0].ID
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{other}}); err == nil {
		t.Fatal("打别人的牌应当被拒")
	}
	// 非法牌型：随便挑两张不同点数（若恰好同点则换一张）
	bad := []int{hand[0].ID}
	for _, c := range hand[1:] {
		if c.Rank != hand[0].Rank {
			bad = append(bad, c.ID)
			break
		}
	}
	if len(bad) == 2 {
		if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: bad}); err == nil {
			t.Fatal("非法牌型应当被拒")
		}
	}
	// 自由出牌权不能 pass
	if _, err := g.Apply(0, Action{Kind: ActPass}); err == nil {
		t.Fatal("自由出牌权时 pass 应当被拒")
	}
	// 合法单张
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{hand[len(hand)-1].ID}}); err != nil {
		t.Fatalf("打最小单张失败: %v", err)
	}
	if g.Turn != 1 {
		t.Fatalf("出牌后回合 = %d, 期望 1", g.Turn)
	}
}

func TestPassAndFreeLead(t *testing.T) {
	g := newTestGame(t)
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 3}); err != nil {
		t.Fatalf("叫 3 分失败: %v", err)
	}
	if g.Landlord != 0 {
		t.Fatalf("地主 = %d, 期望 0", g.Landlord)
	}
	lead := g.Hands[0][len(g.Hands[0])-1]
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{lead.ID}}); err != nil {
		t.Fatalf("出牌失败: %v", err)
	}
	// 两家都不要 → 出牌权回到座位 0，且必须出牌
	for _, seat := range []int{1, 2} {
		if g.Turn != seat {
			t.Fatalf("回合 = %d, 期望 %d", g.Turn, seat)
		}
		if _, err := g.Apply(seat, Action{Kind: ActPass}); err != nil {
			t.Fatalf("座位 %d pass 失败: %v", seat, err)
		}
	}
	if g.Turn != 0 {
		t.Fatalf("两家不要后回合 = %d, 期望 0", g.Turn)
	}
	if g.LastPlay != nil {
		t.Fatal("两家不要后应当清空待压牌")
	}
	if _, err := g.Apply(0, Action{Kind: ActPass}); err == nil {
		t.Fatal("重新获得自由出牌权后 pass 应当被拒")
	}
}

func TestBombDoublesMultiplierAndSpring(t *testing.T) {
	g := newTestGame(t)
	craftPlaying(t, g, [3][]string{
		{"3", "3", "3", "3", "9"},
		{"5", "6"},
		{"7", "8"},
	}, 0, 0, 1)

	bomb := g.Hands[0][1:5] // 手牌降序：9 3 3 3 3
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: ids(bomb)}); err != nil {
		t.Fatalf("打炸弹失败: %v", err)
	}
	if g.Multiplier != 2 {
		t.Fatalf("炸弹后倍数 = %d, 期望 2", g.Multiplier)
	}
	// 两家都不要 → 自由出牌
	for _, seat := range []int{1, 2} {
		if _, err := g.Apply(seat, Action{Kind: ActPass}); err != nil {
			t.Fatalf("座位 %d pass 失败: %v", seat, err)
		}
	}
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{g.Hands[0][0].ID}}); err != nil {
		t.Fatalf("打最后一张失败: %v", err)
	}
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseRoundEnd)
	}
	if !g.LandlordWin || g.Winner != 0 {
		t.Fatalf("胜者 = %d, 地主赢 = %v, 期望座位 0 且地主赢", g.Winner, g.LandlordWin)
	}
	if !g.Spring {
		t.Fatal("农民一张未出，应当判春天")
	}
	if g.Multiplier != 4 {
		t.Fatalf("炸弹 + 春天后倍数 = %d, 期望 4", g.Multiplier)
	}
	// 底分 1 × 叫分 1 × 倍数 4 = 4；地主 +8，农民各 -4
	if g.RoundScore[0] != 8 || g.RoundScore[1] != -4 || g.RoundScore[2] != -4 {
		t.Fatalf("得分 = %v, 期望 [8 -4 -4]", g.RoundScore)
	}
	if g.Scores[0]+g.Scores[1]+g.Scores[2] != 0 {
		t.Fatalf("三家积分之和应当为 0, 实际 %v", g.Scores)
	}
}

func TestAntiSpringScoring(t *testing.T) {
	g := newTestGame(t)
	craftPlaying(t, g, [3][]string{
		{"3", "4"},
		{"5"},
		{"6", "7"},
	}, 0, 0, 1)

	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{g.Hands[0][1].ID}}); err != nil { // 打 3
		t.Fatalf("地主出牌失败: %v", err)
	}
	if _, err := g.Apply(1, Action{Kind: ActPlay, Cards: []int{g.Hands[1][0].ID}}); err != nil { // 打 5，出完
		t.Fatalf("农民出牌失败: %v", err)
	}
	if g.Winner != 1 || g.LandlordWin {
		t.Fatalf("胜者 = %d, 地主赢 = %v, 期望农民 1 获胜", g.Winner, g.LandlordWin)
	}
	if !g.AntiSpring {
		t.Fatal("地主只出过一手，应当判反春天")
	}
	if g.Multiplier != 2 {
		t.Fatalf("反春天倍数 = %d, 期望 2", g.Multiplier)
	}
	if g.RoundScore[0] != -4 || g.RoundScore[1] != 2 || g.RoundScore[2] != 2 {
		t.Fatalf("得分 = %v, 期望 [-4 2 2]", g.RoundScore)
	}
}

func TestCannotBeatBiggerCard(t *testing.T) {
	g := newTestGame(t)
	craftPlaying(t, g, [3][]string{
		{"K", "A"},
		{"3", "4"},
		{"5", "6"},
	}, 0, 0, 1)

	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{g.Hands[0][0].ID}}); err != nil {
		t.Fatalf("地主出 A 失败: %v", err)
	}
	if _, err := g.Apply(1, Action{Kind: ActPlay, Cards: []int{g.Hands[1][1].ID}}); err == nil {
		t.Fatal("用 3 压 A 应当被拒")
	}
	if g.Turn != 1 {
		t.Fatalf("被拒后回合 = %d, 期望仍是 1", g.Turn)
	}
}

func TestPublicViewHidesOpponentHands(t *testing.T) {
	g := newTestGame(t)
	if _, err := g.Apply(0, Action{Kind: ActBid, Bid: 2}); err != nil {
		t.Fatalf("叫分失败: %v", err)
	}
	view := g.PublicView()
	for seat := 0; seat < Seats; seat++ {
		if view.HandCount[seat] != len(g.Hands[seat]) {
			t.Fatalf("座位 %d 张数 = %d, 期望 %d", seat, view.HandCount[seat], len(g.Hands[seat]))
		}
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("序列化公共视图失败: %v", err)
	}
	text := string(raw)
	for seat := 1; seat < Seats; seat++ {
		for _, c := range g.Hands[seat] {
			if strings.Contains(text, fmt.Sprintf(`"id":%d,`, c.ID)) {
				t.Fatalf("公共视图泄漏了座位 %d 的手牌 %s（id=%d）", seat, c.Label(), c.ID)
			}
		}
	}

	// 本人视图必须包含自己的手牌
	priv := g.PrivateView(1)
	if len(priv.Hand) != len(g.Hands[1]) {
		t.Fatalf("私密视图手牌 = %d 张, 期望 %d", len(priv.Hand), len(g.Hands[1]))
	}
	// 座位 0 已叫分，现在轮到座位 1 叫
	if !priv.IsTurn {
		t.Fatal("座位 1 应当是当前叫分方")
	}
	if g.PrivateView(2).IsTurn {
		t.Fatal("座位 2 不该是当前回合方")
	}
	// 定地主前底牌对所有人保密
	g2 := newTestGame(t)
	if g2.PublicView().Bottom != nil {
		t.Fatal("定地主前公共视图不应包含底牌")
	}
}

func TestAutoPlaySingleStrategy(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(7)), 0, 2)
	if _, err := g.Apply(g.Turn, Action{Kind: ActBid, Bid: 3}); err != nil {
		t.Fatalf("叫 3 分失败: %v", err)
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhasePlaying)
	}

	for guard := 0; g.Phase == PhasePlaying; guard++ {
		if guard > 2000 {
			t.Fatal("对局未收敛，疑似死循环")
		}
		seat := g.Turn
		hand := g.Hands[seat]
		if len(hand) == 0 {
			t.Fatalf("座位 %d 手牌已空但本局未结束", seat)
		}

		var pick []int
		switch {
		case g.LastPlay == nil:
			// 自由出牌：出最小的单张（手牌降序，末尾最小）
			pick = []int{hand[len(hand)-1].ID}
		case g.LastPlay.Combo.Kind == KindSingle:
			for i := len(hand) - 1; i >= 0; i-- {
				if hand[i].Rank > g.LastPlay.Combo.Main {
					pick = []int{hand[i].ID}
					break
				}
			}
		}

		if pick == nil {
			if _, err := g.Apply(seat, Action{Kind: ActPass}); err != nil {
				t.Fatalf("座位 %d pass 失败: %v", seat, err)
			}
			continue
		}
		if _, err := g.Apply(seat, Action{Kind: ActPlay, Cards: pick}); err != nil {
			t.Fatalf("座位 %d 出牌失败: %v", seat, err)
		}
	}

	if g.Winner < 0 {
		t.Fatal("本局结束后应当有胜者")
	}
	sum := 0
	for _, s := range g.Scores {
		sum += s
	}
	if sum != 0 {
		t.Fatalf("三家积分之和 = %d, 期望 0（%v）", sum, g.Scores)
	}
	if len(g.Hands[g.Winner]) != 0 {
		t.Fatalf("胜者手牌应为空，实际 %d 张", len(g.Hands[g.Winner]))
	}
}

func TestNextRoundKeepsScores(t *testing.T) {
	g := newTestGame(t)
	craftPlaying(t, g, [3][]string{
		{"3"},
		{"4", "5"},
		{"6", "7"},
	}, 0, 0, 1)
	if _, err := g.Apply(0, Action{Kind: ActPlay, Cards: []int{g.Hands[0][0].ID}}); err != nil {
		t.Fatalf("出牌失败: %v", err)
	}
	before := g.Scores
	if g.Phase != PhaseRoundEnd {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseRoundEnd)
	}

	g.NextRound()
	if g.Phase != PhaseBidding {
		t.Fatalf("下一局阶段 = %s, 期望 %s", g.Phase, PhaseBidding)
	}
	if g.Round != 2 {
		t.Fatalf("局数 = %d, 期望 2（首局为第 1 局）", g.Round)
	}
	if g.Scores != before {
		t.Fatalf("累计积分被重置: %v → %v", before, g.Scores)
	}
	if len(g.Hands[0]) != HandSize || len(g.Bottom) != BottomSize {
		t.Fatalf("下一局发牌不对: 手牌 %d 张, 底牌 %d 张", len(g.Hands[0]), len(g.Bottom))
	}
	if g.Winner != -1 || g.Landlord != -1 {
		t.Fatalf("下一局应当重置胜者与地主: winner=%d landlord=%d", g.Winner, g.Landlord)
	}
}
