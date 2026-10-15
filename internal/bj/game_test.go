package bj

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func newTestGame(t *testing.T, seats int) *Game {
	t.Helper()
	return NewGame(rand.New(rand.NewSource(20240517)), seats, 100, DealerVirtual)
}

// newRotateGame 建一桌轮流坐庄。
func newRotateGame(t *testing.T, seats, chips int) *Game {
	t.Helper()
	return NewGame(rand.New(rand.NewSource(20240517)), seats, chips, DealerRotate)
}

// playRound 让所有闲家押 1 注、按"17 点以下要牌"打完一轮（只推进状态，不校验结果）。
func playRound(t *testing.T, g *Game) {
	t.Helper()
	for i := range g.Hands {
		if g.Phase != PhaseBetting {
			break
		}
		if g.Hands[i].Status == StatusIdle {
			if _, err := g.Apply(i, Action{Kind: ActBet, Amount: 1}); err != nil {
				t.Fatalf("座位 %d 下注失败: %v", i, err)
			}
		}
	}
	for step := 0; g.Phase == PhasePlaying; step++ {
		if step > 200 {
			t.Fatal("行动阶段没有收敛")
		}
		seat := g.Turn
		total, _ := Score(g.Hands[seat].Cards)
		kind := ActHit
		if total >= 17 {
			kind = ActStand
		}
		if _, err := g.Apply(seat, Action{Kind: kind}); err != nil {
			t.Fatalf("座位 %d 行动失败: %v", seat, err)
		}
	}
}

func totalChips(g *Game) int {
	sum := 0
	for _, c := range g.Chips {
		sum += c
	}
	return sum
}

// stackShoe 让接下来的 draw() 依次返回这些牌，用于把随机发牌变成确定的过程。
func stackShoe(t *testing.T, g *Game, labels ...string) {
	t.Helper()
	g.shoe = mustCards(t, labels...)
}

// forceHand 覆盖某个座位的手牌并把行动权交给它，避免测试被随机发牌（比如起手黑杰克）干扰。
func forceHand(t *testing.T, g *Game, seat int, labels ...string) {
	t.Helper()
	g.Hands[seat].Cards = mustCards(t, labels...)
	g.Hands[seat].Status = StatusWaiting
	g.Phase = PhasePlaying
	g.Turn = seat
}

// craftSettle 直接把对局摆到结算前：手工指定各家手牌、注额与庄家手牌。
func craftSettle(t *testing.T, g *Game, hands [][]string, bets []int, dealer []string) {
	t.Helper()
	for i := range g.Hands {
		g.Hands[i].Cards = mustCards(t, hands[i]...)
		g.Hands[i].Bet = bets[i]
		g.Hands[i].Status = StatusStood
		total, _ := Score(g.Hands[i].Cards)
		if total > Target {
			g.Hands[i].Status = StatusBust
		} else if IsBlackjack(g.Hands[i].Cards) {
			g.Hands[i].Status = StatusBlackjack
		}
		g.Chips[i] = 100 - bets[i] // 下注时已扣
	}
	g.Dealer = mustCards(t, dealer...)
	g.Phase = PhasePlaying
}

func TestBettingThenDeal(t *testing.T) {
	g := newTestGame(t, 2)
	if g.Phase != PhaseBetting {
		t.Fatalf("初始阶段 = %s, 期望 %s", g.Phase, PhaseBetting)
	}

	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("座位 0 下注失败: %v", err)
	}
	if g.Phase != PhaseBetting {
		t.Fatal("还有人没下注就不该发牌")
	}
	if len(g.Hands[0].Cards) != 0 {
		t.Fatal("下注阶段不该有牌")
	}
	if g.Chips[0] != 90 {
		t.Fatalf("下注后筹码 = %d, 期望 90", g.Chips[0])
	}

	if _, err := g.Apply(1, Action{Kind: ActBet, Amount: 20}); err != nil {
		t.Fatalf("座位 1 下注失败: %v", err)
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("全员下注后阶段 = %s, 期望 %s", g.Phase, PhasePlaying)
	}
	for i := range g.Hands {
		if len(g.Hands[i].Cards) != 2 {
			t.Fatalf("座位 %d 起手 = %d 张, 期望 2", i, len(g.Hands[i].Cards))
		}
	}
	if len(g.Dealer) != 2 {
		t.Fatalf("庄家起手 = %d 张, 期望 2", len(g.Dealer))
	}
	if !g.hidden {
		t.Fatal("发牌后庄家暗牌应当是盖着的")
	}
}

func TestBetValidation(t *testing.T) {
	g := newTestGame(t, 2)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 0}); err == nil {
		t.Fatal("注额 0 应当被拒")
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: -5}); err == nil {
		t.Fatal("负注额应当被拒")
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 101}); err == nil {
		t.Fatal("超过筹码的注额应当被拒")
	}
	if _, err := g.Apply(0, Action{Kind: ActHit}); err == nil {
		t.Fatal("下注阶段不该允许要牌")
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("正常下注失败: %v", err)
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err == nil {
		t.Fatal("重复下注应当被拒")
	}
	if _, err := g.Apply(5, Action{Kind: ActBet, Amount: 1}); err == nil {
		t.Fatal("非法座位应当被拒")
	}
}

// TestHiddenCardNeverLeavesHost 是隐私红线：结算前，庄家暗牌不能出现在公共视图里。
func TestHiddenCardNeverLeavesHost(t *testing.T) {
	g := newTestGame(t, 3)
	for seat := 0; seat < 3; seat++ {
		if _, err := g.Apply(seat, Action{Kind: ActBet, Amount: 10}); err != nil {
			t.Fatalf("座位 %d 下注失败: %v", seat, err)
		}
	}
	hole := g.Dealer[1] // 暗牌

	view := g.PublicView()
	if view.Dealer.Hidden != 1 {
		t.Fatalf("暗牌张数 = %d, 期望 1", view.Dealer.Hidden)
	}
	if len(view.Dealer.Up) != 1 {
		t.Fatalf("明牌 = %d 张, 期望 1", len(view.Dealer.Up))
	}
	if view.Dealer.Final {
		t.Fatal("结算前 Final 不该为 true")
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("序列化公共视图失败: %v", err)
	}
	if strings.Contains(string(raw), fmt.Sprintf(`"id":%d,`, hole.ID)) {
		t.Fatalf("公共视图泄漏了庄家暗牌 %s（id=%d）: %s", hole.Label(), hole.ID, raw)
	}

	// 结算之后必须能看到
	g.Dealer = append(g.Dealer, g.draw())
	g.hidden = false
	after, _ := json.Marshal(g.PublicView())
	if !strings.Contains(string(after), fmt.Sprintf(`"id":%d,`, hole.ID)) {
		t.Fatal("结算后应当能看到庄家暗牌")
	}
}

func TestTurnValidation(t *testing.T) {
	g := newTestGame(t, 2)
	for seat := 0; seat < 2; seat++ {
		if _, err := g.Apply(seat, Action{Kind: ActBet, Amount: 10}); err != nil {
			t.Fatalf("座位 %d 下注失败: %v", seat, err)
		}
	}
	forceHand(t, g, 0, "10", "7")
	g.Hands[1].Cards = mustCards(t, "9", "8")
	g.Hands[1].Status = StatusWaiting

	if _, err := g.Apply(1, Action{Kind: ActStand}); err == nil {
		t.Fatal("不是自己的回合不该能停牌")
	}
	if _, err := g.Apply(1, Action{Kind: ActHit}); err == nil {
		t.Fatal("不是自己的回合不该能要牌")
	}
	if _, err := g.Apply(0, Action{Kind: ActStand}); err != nil {
		t.Fatalf("座位 0 停牌失败: %v", err)
	}
	if g.Turn != 1 {
		t.Fatalf("座位 0 停牌后应当轮到座位 1，实际 %d", g.Turn)
	}
	if _, err := g.Apply(0, Action{Kind: ActStand}); err == nil {
		t.Fatal("停过牌之后不该还能再动")
	}
}

func TestDoubleDoublesBetAndDrawsOne(t *testing.T) {
	g := newTestGame(t, 1)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 20}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	forceHand(t, g, 0, "5", "6") // 11 点，安全
	stackShoe(t, g, "5")         // 双倍只补这一张
	before := g.Chips[0]

	if _, err := g.Apply(0, Action{Kind: ActDouble}); err != nil {
		t.Fatalf("双倍失败: %v", err)
	}
	if g.Hands[0].Bet != 40 {
		t.Fatalf("双倍后注额 = %d, 期望 40", g.Hands[0].Bet)
	}
	if len(g.Hands[0].Cards) != 3 {
		t.Fatalf("双倍后手牌 = %d 张, 期望 3（只能补一张）", len(g.Hands[0].Cards))
	}
	if g.Chips[0] != before-20 {
		t.Fatalf("双倍应再扣一份注额: %d → %d", before, g.Chips[0])
	}
	if g.Hands[0].Status != StatusStood {
		t.Fatalf("双倍后应当自动停牌，实际 %s（%s）", g.Hands[0].Status, Describe(g.Hands[0].Cards))
	}
	if g.Phase != PhaseSettle {
		t.Fatalf("只有一个座位时，双倍完应当直接结算，实际阶段 %s", g.Phase)
	}
}

func TestDoubleNeedsChips(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(3)), 1, 30, DealerVirtual)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 30}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	if _, err := g.Apply(0, Action{Kind: ActDouble}); err == nil {
		t.Fatal("筹码不够时双倍应当被拒")
	}
}

func TestHitBustAdvancesToNextSeat(t *testing.T) {
	g := newTestGame(t, 2)
	for seat := 0; seat < 2; seat++ {
		if _, err := g.Apply(seat, Action{Kind: ActBet, Amount: 10}); err != nil {
			t.Fatalf("下注失败: %v", err)
		}
	}
	forceHand(t, g, 0, "2", "3") // 5 点，要两张必定爆
	g.Hands[1].Cards = mustCards(t, "9", "8")
	g.Hands[1].Status = StatusWaiting
	stackShoe(t, g, "K", "Q") // 5+10=15 不爆，再要一张 25 爆
	if _, err := g.Apply(0, Action{Kind: ActHit}); err != nil {
		t.Fatalf("要牌失败: %v", err)
	}
	if g.Turn != 0 {
		t.Fatalf("没爆牌就该继续由座位 0 行动，实际轮到 %d", g.Turn)
	}
	events, err := g.Apply(0, Action{Kind: ActHit})
	if err != nil {
		t.Fatalf("要牌失败: %v", err)
	}
	if g.Hands[0].Status != StatusBust {
		t.Fatalf("要两张 10 点牌应当爆牌，实际 %s（手牌 %s）", g.Hands[0].Status, Describe(g.Hands[0].Cards))
	}
	if g.Turn != 1 {
		t.Fatalf("爆牌后应当轮到座位 1，实际 %d", g.Turn)
	}
	if !hasText(events, "爆牌") {
		t.Fatalf("应当有爆牌事件: %+v", events)
	}
}

func TestDealerDrawsTo17(t *testing.T) {
	g := newTestGame(t, 1)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	forceHand(t, g, 0, "10", "8") // 18 点，停牌
	// 手工把庄家摆成 10 点 → 庄家必须补到 >=17
	g.Dealer = mustCards(t, "10")
	stackShoe(t, g, "5", "3") // 依次补出 15 → 18
	if _, err := g.Apply(0, Action{Kind: ActStand}); err != nil {
		t.Fatalf("停牌失败: %v", err)
	}
	total, _ := Score(g.Dealer)
	if total != 18 {
		t.Fatalf("庄家点数 = %d, 期望补到 18（10+5+3）", total)
	}
	if g.Phase != PhaseSettle {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseSettle)
	}
	if g.hidden {
		t.Fatal("结算后暗牌应当翻开")
	}
}

func TestDealerStandsOnSoft17(t *testing.T) {
	g := newTestGame(t, 1)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	forceHand(t, g, 0, "10", "8")     // 18 点，停牌
	g.Dealer = mustCards(t, "A", "6") // 软 17
	stackShoe(t, g, "5")
	if _, err := g.Apply(0, Action{Kind: ActStand}); err != nil {
		t.Fatalf("停牌失败: %v", err)
	}
	if len(g.Dealer) != 2 {
		t.Fatalf("软 17 应当停牌，庄家却补到了 %d 张", len(g.Dealer))
	}
}

func TestDealerSkipsDrawWhenAllBust(t *testing.T) {
	g := newTestGame(t, 1)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	g.Dealer = mustCards(t, "10")
	g.Hands[0].Cards = mustCards(t, "K", "Q", "2") // 22 点爆牌
	g.Hands[0].Status = StatusBust
	stackShoe(t, g, "5")
	events := g.playDealer()
	if len(g.Dealer) != 1 {
		t.Fatalf("闲家全爆时庄家不该补牌，实际 %d 张", len(g.Dealer))
	}
	if !hasText(events, "庄家") {
		t.Fatalf("应当有庄家事件: %+v", events)
	}
}

func TestSettleTable(t *testing.T) {
	cases := []struct {
		name         string
		player       []string
		dealer       []string
		bet          int
		wantDelta    int
		wantContains string
	}{
		{name: "爆牌输", player: []string{"K", "Q", "2"}, dealer: []string{"10", "9"}, bet: 10, wantDelta: -10, wantContains: "爆牌"},
		{name: "黑杰克赔3比2", player: []string{"A", "K"}, dealer: []string{"10", "9"}, bet: 10, wantDelta: 15, wantContains: "黑杰克"},
		{name: "双方黑杰克平局", player: []string{"A", "K"}, dealer: []string{"A", "Q"}, bet: 10, wantDelta: 0, wantContains: "平局"},
		{name: "庄家黑杰克通吃", player: []string{"10", "K"}, dealer: []string{"A", "Q"}, bet: 10, wantDelta: -10, wantContains: "庄家黑杰克"},
		{name: "庄家爆牌闲家赢", player: []string{"10", "8"}, dealer: []string{"K", "Q", "2"}, bet: 10, wantDelta: 10, wantContains: "庄家爆牌"},
		{name: "点数大过庄家", player: []string{"10", "9"}, dealer: []string{"10", "8"}, bet: 10, wantDelta: 10, wantContains: "大过"},
		{name: "点数打平", player: []string{"10", "8"}, dealer: []string{"9", "9"}, bet: 10, wantDelta: 0, wantContains: "打平"},
		{name: "点数小于庄家", player: []string{"10", "7"}, dealer: []string{"10", "9"}, bet: 10, wantDelta: -10, wantContains: "小于"},
		{name: "庄家爆牌但闲家黑杰克仍赔3比2", player: []string{"A", "Q"}, dealer: []string{"K", "Q", "2"}, bet: 20, wantDelta: 30, wantContains: "黑杰克"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGame(t, 1)
			craftSettle(t, g, [][]string{tc.player}, []int{tc.bet}, tc.dealer)
			g.settle()

			res := g.results[0]
			if res.Delta != tc.wantDelta {
				t.Fatalf("净输赢 = %d, 期望 %d（原因：%s）", res.Delta, tc.wantDelta, res.Reason)
			}
			if !strings.Contains(res.Reason, tc.wantContains) {
				t.Fatalf("结算原因 = %q, 期望包含 %q", res.Reason, tc.wantContains)
			}
			// 下注时已扣款，结算后应当回到 100 + 净输赢
			if want := 100 + tc.wantDelta; g.Chips[0] != want {
				t.Fatalf("结算后筹码 = %d, 期望 %d", g.Chips[0], want)
			}
			if res.Chips != g.Chips[0] {
				t.Fatalf("结果里的筹码 = %d, 实际 %d", res.Chips, g.Chips[0])
			}
		})
	}
}

func TestApplyRejectedAfterSettle(t *testing.T) {
	g := newTestGame(t, 1)
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err != nil {
		t.Fatalf("下注失败: %v", err)
	}
	forceHand(t, g, 0, "10", "8")
	if _, err := g.Apply(0, Action{Kind: ActStand}); err != nil {
		t.Fatalf("停牌失败: %v", err)
	}
	if g.Phase != PhaseSettle {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseSettle)
	}
	if _, err := g.Apply(0, Action{Kind: ActHit}); err == nil {
		t.Fatal("结算后不该还能要牌")
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 10}); err == nil {
		t.Fatal("结算后不该还能下注")
	}
}

// TestBrokeSeatIsEliminated 筹码是"真"的：输光就出局，不再自动补满。
func TestBrokeSeatIsEliminated(t *testing.T) {
	g := newTestGame(t, 3)
	g.Chips[0] = 0
	g.Chips[1] = 7
	events := g.NextRound()

	if g.Round != 2 || g.Phase != PhaseBetting {
		t.Fatalf("下一轮状态不对: round=%d phase=%s", g.Round, g.Phase)
	}
	if g.Hands[0].Status != StatusOut {
		t.Fatalf("输光的座位应当出局，实际 %s", g.Hands[0].Status)
	}
	if !hasText(events, "输光，出局") {
		t.Fatalf("出局应当有日志: %+v", events)
	}
	if g.Chips[1] != 7 {
		t.Fatalf("还有筹码的座位不该被补，实际 %d", g.Chips[1])
	}
	// 出局的人不能再动作
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 1}); err == nil {
		t.Fatal("出局的人不该还能下注")
	}
	// 其他人照常下注
	if _, err := g.Apply(1, Action{Kind: ActBet, Amount: 1}); err != nil {
		t.Fatalf("未出局的座位下注失败: %v", err)
	}
	if g.results != nil {
		t.Fatal("下一轮应当清空上一轮结算")
	}
}

// TestAutoPlayRounds 让引擎自己把好几轮打完：只验证不崩、状态自洽、筹码按净输赢变化。
func TestAutoPlayRounds(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(99)), 4, 100, DealerVirtual)
	for round := 0; round < 20 && g.Phase != PhaseOver; round++ {
		before := append([]int(nil), g.Chips...)
		beforeTotal := totalChips(g)

		playRound(t, g)

		if g.Phase != PhaseSettle {
			t.Fatalf("轮次 %d: 阶段 = %s, 期望结算", round, g.Phase)
		}
		// 虚拟庄家：筹码按净输赢变化
		for _, res := range g.results {
			if got, want := g.Chips[res.Seat], before[res.Seat]+res.Delta; got != want {
				t.Fatalf("轮次 %d 座位 %d: 筹码 %d, 期望 %d（净输赢 %d）", round, res.Seat, got, want, res.Delta)
			}
		}
		_ = beforeTotal
		g.NextRound()
	}
}

// --- 轮流坐庄 ---

// TestRotateDealerOrder 验证庄家按座位轮换，并且跳过已经出局的人。
func TestRotateDealerOrder(t *testing.T) {
	g := newRotateGame(t, 3, 100)
	if g.DealerSeat != 0 {
		t.Fatalf("第一轮庄家 = %d, 期望 0", g.DealerSeat)
	}
	if g.Hands[0].Status != StatusDealer {
		t.Fatalf("庄家座位状态 = %s, 期望 %s", g.Hands[0].Status, StatusDealer)
	}

	order := []int{g.DealerSeat}
	for i := 0; i < 4; i++ {
		playRound(t, g)
		if g.Phase == PhaseOver {
			t.Fatalf("不该这么快结束：%s", g.LastEvent)
		}
		g.NextRound()
		order = append(order, g.DealerSeat)
	}
	want := []int{0, 1, 2, 0, 1}
	if len(order) != len(want) {
		t.Fatalf("轮换记录 = %v, 期望 %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("庄家轮换顺序 = %v, 期望 %v", order, want)
		}
	}

	// 让座位 2 出局，下一轮庄家应当跳过它
	g.Chips[2] = 0
	g.Hands[2].Status = StatusOut
	g.NextRound()
	if g.DealerSeat == 2 {
		t.Fatal("出局的座位不该再坐庄")
	}
}

func TestRotateDealerDoesNotBet(t *testing.T) {
	g := newRotateGame(t, 3, 100)
	dealer := g.DealerSeat
	if dealer < 0 {
		t.Fatal("轮庄模式应当有庄家座位")
	}
	if _, err := g.Apply(dealer, Action{Kind: ActBet, Amount: 5}); err == nil {
		t.Fatal("坐庄的人不该能下注")
	}
	// 闲家下注后应当发牌（庄家不用下注也算齐）
	for i := range g.Hands {
		if i == dealer {
			continue
		}
		if _, err := g.Apply(i, Action{Kind: ActBet, Amount: 5}); err != nil {
			t.Fatalf("座位 %d 下注失败: %v", i, err)
		}
	}
	if g.Phase != PhasePlaying {
		t.Fatalf("全员下注后阶段 = %s, 期望 %s", g.Phase, PhasePlaying)
	}
	if len(g.Dealer) != 2 {
		t.Fatalf("庄家起手 = %d 张, 期望 2", len(g.Dealer))
	}
	if len(g.Hands[dealer].Cards) != 0 {
		t.Fatal("庄家的牌不该出现在座位视图里（否则暗牌会从那里漏出去）")
	}
	// 庄家不参与行动：Turn 永远不该是庄家
	for step := 0; g.Phase == PhasePlaying && step < 50; step++ {
		if g.Turn == dealer {
			t.Fatal("庄家不该被要求行动")
		}
		total, _ := Score(g.Hands[g.Turn].Cards)
		kind := ActHit
		if total >= 17 {
			kind = ActStand
		}
		if _, err := g.Apply(g.Turn, Action{Kind: kind}); err != nil {
			t.Fatalf("行动失败: %v", err)
		}
	}
}

// TestRotateChipsAreConserved 轮庄模式下筹码只在玩家之间流动，总额不变。
func TestRotateChipsAreConserved(t *testing.T) {
	g := newRotateGame(t, 4, 60)
	start := totalChips(g)
	for i := 0; i < 6 && g.Phase != PhaseOver; i++ {
		playRound(t, g)
		if g.Phase == PhaseSettle && g.DealerSeat >= 0 {
			// 庄家的净输赢应当等于闲家净输赢之和的相反数
			sum := 0
			for _, r := range g.results {
				sum += r.Delta
			}
			if g.DealerDelta != -sum {
				t.Fatalf("庄家净输赢 = %d, 期望 %d", g.DealerDelta, -sum)
			}
		}
		if got := totalChips(g); got != start {
			t.Fatalf("第 %d 轮后筹码总额 = %d, 期望 %d（轮庄不该凭空造筹码）", i+1, got, start)
		}
		g.NextRound()
	}
}

// TestBetCapByDealerChips 下注上限要保证庄家最坏情况（闲家全黑杰克）也赔得起。
func TestBetCapByDealerChips(t *testing.T) {
	g := newRotateGame(t, 3, 100)
	dealer := g.DealerSeat
	g.Chips[dealer] = 9 // 2 个闲家 → 最多每人押 9*2/(3*2)=3
	farmers := []int{}
	for i := range g.Hands {
		if i != dealer {
			farmers = append(farmers, i)
		}
	}
	if got := g.MaxBet(farmers[0]); got != 3 {
		t.Fatalf("下注上限 = %d, 期望 3", got)
	}
	if _, err := g.Apply(farmers[0], Action{Kind: ActBet, Amount: 4}); err == nil {
		t.Fatal("超过庄家赔付能力的注额应当被拒")
	}
	if _, err := g.Apply(farmers[0], Action{Kind: ActBet, Amount: 3}); err != nil {
		t.Fatalf("等于上限的注额应当被接受: %v", err)
	}
	// 再验一次"最坏情况刚好赔得起"：两个闲家都押 3 且都是黑杰克
	g2 := newRotateGame(t, 3, 100)
	d := g2.DealerSeat
	g2.Chips[d] = 9
	fs := []int{}
	for i := range g2.Hands {
		if i != d {
			fs = append(fs, i)
		}
	}
	for _, f := range fs {
		if _, err := g2.Apply(f, Action{Kind: ActBet, Amount: g2.MaxBet(f)}); err != nil {
			t.Fatalf("座位 %d 下注失败: %v", f, err)
		}
	}
	for _, f := range fs {
		g2.Hands[f].Cards = mustCards(t, "A", "K")
		g2.Hands[f].Status = StatusBlackjack
	}
	g2.Dealer = mustCards(t, "10", "9") // 20 点，黑杰克闲家赢
	g2.hidden = false
	g2.settle()
	if g2.Chips[d] < 0 {
		t.Fatalf("庄家最坏情况赔成了负数: %d", g2.Chips[d])
	}
	if g2.Chips[d] > 2 {
		t.Fatalf("两个黑杰克应当几乎把庄家赔光，实际剩 %d", g2.Chips[d])
	}
}

// TestDealerEliminatedWhenCannotAfford 赔不起就没资格坐庄，直接出局。
func TestDealerEliminatedWhenCannotAfford(t *testing.T) {
	g := newRotateGame(t, 3, 100)
	// 让轮转下一轮回到座位 0，而它只剩 2 个筹码（3 人局坐庄需要 3 个）
	g.lastDealer = 2
	g.Chips[0] = 2
	events := g.NextRound()

	if g.Hands[0].Status != StatusOut {
		t.Fatalf("筹码不足以坐庄应当出局，实际 %s", g.Hands[0].Status)
	}
	if !hasText(events, "坐不起庄，出局") {
		t.Fatalf("应当有坐不起庄的日志: %+v", events)
	}
	if g.DealerSeat == 0 {
		t.Fatal("出局的人不该坐庄")
	}
	if g.Phase == PhaseOver {
		t.Fatal("还有两个人，不该结束")
	}
}

// TestTableOverWhenOneRemains 只剩一个人没出局时整桌结束。
func TestTableOverWhenOneRemains(t *testing.T) {
	g := newTestGame(t, 2)
	g.Chips[1] = 0
	events := g.NextRound()

	if g.Phase != PhaseOver {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseOver)
	}
	if g.Winner != 0 {
		t.Fatalf("赢家 = %d, 期望 0", g.Winner)
	}
	if !hasText(events, "赢到最后") {
		t.Fatalf("应当宣布赢家: %+v", events)
	}
	if _, err := g.Apply(0, Action{Kind: ActBet, Amount: 1}); err == nil {
		t.Fatal("整桌结束后不该还能下注")
	}
	if view := g.PublicView(); !view.Over || view.Winner != 0 {
		t.Fatalf("公共视图没体现整桌结束: over=%v winner=%d", view.Over, view.Winner)
	}
}

func hasText(events []Event, want string) bool {
	for _, e := range events {
		if strings.Contains(e.Text, want) {
			return true
		}
	}
	return false
}
