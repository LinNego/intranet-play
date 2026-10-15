package zjh

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

func newTestGame(t *testing.T, seats int) *Game {
	t.Helper()
	return NewGame(rand.New(rand.NewSource(20240517)), seats, 100, 2, 20, 6)
}

func totalChips(g *Game) int {
	sum := 0
	for _, c := range g.Chips {
		sum += c
	}
	return sum + g.Pot // 奖池里的钱也是玩家的
}

// forceHand 直接指定某人的三张牌，用于把比牌结果变成确定的。
func forceHand(t *testing.T, g *Game, seat int, labels ...string) {
	t.Helper()
	g.Players[seat].Cards = mustCards(t, labels...)
}

func TestNewGameAnteAndDeal(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(1)), 3, 100, 2, 20, 6)

	if g.Phase != PhaseBetting {
		t.Fatalf("初始阶段 = %s, 期望 %s", g.Phase, PhaseBetting)
	}
	if g.Stake != 2 || g.Ante != 2 {
		t.Fatalf("注额/底注 = %d/%d, 期望 2/2", g.Stake, g.Ante)
	}
	if g.Pot != 6 {
		t.Fatalf("奖池 = %d, 期望 6（3 人 × 底注 2）", g.Pot)
	}
	if g.Turn != 0 {
		t.Fatalf("第一个行动 = %d, 期望 0", g.Turn)
	}
	for i := range g.Players {
		if len(g.Players[i].Cards) != 3 {
			t.Fatalf("座位 %d 手牌 = %d 张, 期望 3", i, len(g.Players[i].Cards))
		}
		if g.Players[i].Status != StatusPlaying {
			t.Fatalf("座位 %d 状态 = %s, 期望 %s", i, g.Players[i].Status, StatusPlaying)
		}
		if g.Players[i].Looked {
			t.Fatalf("座位 %d 开局不该已经看过牌", i)
		}
		if g.Chips[i] != 98 {
			t.Fatalf("座位 %d 筹码 = %d, 期望 98（扣过底注）", i, g.Chips[i])
		}
	}
}

// TestCardsAreNotSentBeforeLook 是本作最硬的隐私要求：
// 没看牌之前，连自己的牌都不能下发（否则改个客户端就能偷看）。
func TestCardsAreNotSentBeforeLook(t *testing.T) {
	g := newTestGame(t, 3)

	pv := g.PrivateView(0)
	if pv.Looked {
		t.Fatal("开局不该是已看牌")
	}
	if len(pv.Cards) != 0 {
		t.Fatalf("没看牌就不该下发手牌，实际 %d 张", len(pv.Cards))
	}
	if pv.HandLabel != "" {
		t.Fatalf("没看牌不该下发牌型: %q", pv.HandLabel)
	}

	// 公共视图里一张牌都不能有
	raw, err := json.Marshal(g.PublicView())
	if err != nil {
		t.Fatalf("序列化公共视图失败: %v", err)
	}
	if strings.Contains(string(raw), `"rank"`) {
		t.Fatalf("公共视图泄漏了牌面: %s", raw)
	}

	// 看牌之后只发自己的
	if _, err := g.Apply(0, Action{Kind: ActLook}); err != nil {
		t.Fatalf("看牌失败: %v", err)
	}
	pv = g.PrivateView(0)
	if !pv.Looked || len(pv.Cards) != 3 || pv.HandLabel == "" {
		t.Fatalf("看牌后应当拿到自己的三张牌: %+v", pv)
	}
	if other := g.PrivateView(1); len(other.Cards) != 0 {
		t.Fatalf("别人的牌不该跟着下发: %d 张", len(other.Cards))
	}
	if _, err := g.Apply(0, Action{Kind: ActLook}); err == nil {
		t.Fatal("重复看牌应当被拒")
	}
}

func TestLookIsAllowedOutOfTurn(t *testing.T) {
	g := newTestGame(t, 3)
	if g.Turn != 0 {
		t.Fatalf("第一个行动 = %d, 期望 0", g.Turn)
	}
	if _, err := g.Apply(2, Action{Kind: ActLook}); err != nil {
		t.Fatalf("看牌应当随时可用: %v", err)
	}
	if !g.PrivateView(2).Looked {
		t.Fatal("座位 2 应当已看牌")
	}
	// 但行动还是要等轮到自己
	if _, err := g.Apply(2, Action{Kind: ActCall}); err == nil {
		t.Fatal("不是自己的回合不该能跟注")
	}
}

func TestCostBlindIsHalf(t *testing.T) {
	g := newTestGame(t, 3)
	g.Stake = 6

	if got := g.Cost(0); got != 3 {
		t.Fatalf("闷牌跟注 = %d, 期望 3（6 的一半）", got)
	}
	if _, err := g.Apply(0, Action{Kind: ActLook}); err != nil {
		t.Fatalf("看牌失败: %v", err)
	}
	if got := g.Cost(0); got != 6 {
		t.Fatalf("看牌后跟注 = %d, 期望 6", got)
	}

	// 注额是奇数时闷牌付 floor(stake/2)，最低 1
	g2 := newTestGame(t, 3)
	g2.Stake = 1
	if got := g2.Cost(0); got != 1 {
		t.Fatalf("注额 1 时闷牌跟注 = %d, 期望 1（最低 1）", got)
	}
	g2.Stake = 3
	if got := g2.Cost(0); got != 1 {
		t.Fatalf("注额 3 时闷牌跟注 = %d, 期望 1", got)
	}
}

func TestCallAndRaise(t *testing.T) {
	g := newTestGame(t, 3) // 底注 2，闷牌跟注 1
	before := g.Chips[0]

	if _, err := g.Apply(0, Action{Kind: ActCall}); err != nil {
		t.Fatalf("跟注失败: %v", err)
	}
	if g.Chips[0] != before-1 {
		t.Fatalf("闷牌跟注应当扣 1: %d → %d", before, g.Chips[0])
	}
	if g.Pot != 7 {
		t.Fatalf("奖池 = %d, 期望 7", g.Pot)
	}
	if g.Turn != 1 {
		t.Fatalf("跟注后轮到 %d, 期望 1", g.Turn)
	}

	// 加注到 8：闷牌付 4
	before = g.Chips[1]
	if _, err := g.Apply(1, Action{Kind: ActRaise, Amount: 8}); err != nil {
		t.Fatalf("加注失败: %v", err)
	}
	if g.Stake != 8 {
		t.Fatalf("注额 = %d, 期望 8", g.Stake)
	}
	if g.Chips[1] != before-4 {
		t.Fatalf("闷牌加注应当扣 4: %d → %d", before, g.Chips[1])
	}

	// 加注必须更高
	if _, err := g.Apply(2, Action{Kind: ActRaise, Amount: 8}); err == nil {
		t.Fatal("加注到当前注额应当被拒")
	}
	// 不能超过上限
	if _, err := g.Apply(2, Action{Kind: ActRaise, Amount: 21}); err == nil {
		t.Fatal("超过上限的加注应当被拒")
	}
	// 筹码不够就不能加注，而且注额要回滚
	poor := 2
	g.Chips[poor] = 1
	beforeStake := g.Stake
	if _, err := g.Apply(poor, Action{Kind: ActRaise, Amount: 20}); err == nil {
		t.Fatal("筹码不够的加注应当被拒")
	}
	if g.Stake != beforeStake {
		t.Fatalf("加注失败后注额应当回滚到 %d，实际 %d", beforeStake, g.Stake)
	}
	// 筹码不够跟注也不行
	if _, err := g.Apply(poor, Action{Kind: ActCall}); err == nil {
		t.Fatal("筹码不够跟注应当被拒")
	}
}

func TestFoldEndsHandWithTwoPlayers(t *testing.T) {
	g := newTestGame(t, 2)
	start := totalChips(g)
	pot := g.Pot

	if _, err := g.Apply(0, Action{Kind: ActFold}); err != nil {
		t.Fatalf("弃牌失败: %v", err)
	}
	if g.Phase != PhaseSettle {
		t.Fatalf("阶段 = %s, 期望 %s", g.Phase, PhaseSettle)
	}
	if g.Winner != 1 {
		t.Fatalf("赢家 = %d, 期望 1", g.Winner)
	}
	// 座位 1 手里还剩 100-底注，再加上整个奖池；净额是"奖池 − 自己的投入"
	if got, want := g.Chips[1], 100-g.Ante+pot; got != want {
		t.Fatalf("赢家筹码 = %d, 期望 %d", got, want)
	}
	if g.Pot != 0 {
		t.Fatalf("结算后奖池应当清零（已经派发），实际 %d", g.Pot)
	}
	if g.Chips[0] != 100-g.Ante {
		t.Fatalf("弃牌方筹码 = %d, 期望 %d", g.Chips[0], 100-g.Ante)
	}
	if got := totalChips(g); got != start {
		t.Fatalf("筹码应当守恒：%d, 期望 %d", got, start)
	}
	// 结算明细里赢家净额是"奖池 − 自己的投入"
	for _, r := range g.results {
		if r.Seat == 1 && r.Delta != pot-g.Ante {
			t.Fatalf("赢家净额 = %d, 期望 %d", r.Delta, pot-g.Ante)
		}
		if r.Seat == 0 && r.Delta != -g.Ante {
			t.Fatalf("弃牌方净额 = %d, 期望 %d", r.Delta, -g.Ante)
		}
	}
}

func TestCompareLoserFoldsAndPeekIsRecorded(t *testing.T) {
	g := newTestGame(t, 2)
	forceHand(t, g, 0, "A♠", "A♥", "A♦") // 豹子
	forceHand(t, g, 1, "K♠", "Q♥", "J♦") // 顺子

	events, err := g.Apply(0, Action{Kind: ActCompare, Target: 1})
	if err != nil {
		t.Fatalf("比牌失败: %v", err)
	}
	if !hasText(events, "比牌结果") {
		t.Fatalf("应当有比牌结果事件: %+v", events)
	}
	if g.Players[1].Status != StatusFolded {
		t.Fatalf("输的一方应当弃牌，实际 %s", g.Players[1].Status)
	}
	if g.Winner != 0 {
		t.Fatalf("赢家 = %d, 期望 0", g.Winner)
	}

	peeks := g.TakePeeks()
	if len(peeks) != 1 {
		t.Fatalf("应当记录 1 次比牌，实际 %d", len(peeks))
	}
	p := peeks[0]
	if p.A != 0 || p.B != 1 || p.Loser != 1 {
		t.Fatalf("比牌记录不对: %+v", p)
	}
	if len(p.CardsA) != 3 || len(p.CardsB) != 3 {
		t.Fatalf("比牌记录应当带双方牌面: %+v", p)
	}
	if p.HandA != "豹子" || p.HandB != "顺子" {
		t.Fatalf("比牌记录牌型不对: %s / %s", p.HandA, p.HandB)
	}
	// 取过就清空，避免重复单播
	if again := g.TakePeeks(); len(again) != 0 {
		t.Fatalf("TakePeeks 应当清空: %+v", again)
	}
}

// TestCompareTieRules 完全一样大时的裁决：闷牌占优；都明或都闷则主动比牌者输。
func TestCompareTieRules(t *testing.T) {
	// 都是散牌 A K 7，只是花色不同 → Compare 返回 0
	sameA := []string{"A♠", "K♥", "7♦"}
	sameB := []string{"A♥", "K♦", "7♣"}

	t.Run("闷牌方占优", func(t *testing.T) {
		g := newTestGame(t, 2)
		forceHand(t, g, 0, sameA...)
		forceHand(t, g, 1, sameB...)
		// 座位 0 闷牌，座位 1 看牌
		if _, err := g.Apply(1, Action{Kind: ActLook}); err != nil {
			t.Fatalf("看牌失败: %v", err)
		}
		if g.Turn != 0 {
			t.Fatalf("轮到 %d, 期望 0", g.Turn)
		}
		if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 1}); err != nil {
			t.Fatalf("比牌失败: %v", err)
		}
		if g.Players[1].Status != StatusFolded {
			t.Fatal("牌一样大时闷牌方应当赢")
		}
		if g.Winner != 0 {
			t.Fatalf("赢家 = %d, 期望 0", g.Winner)
		}
	})

	t.Run("都看牌则比牌者输", func(t *testing.T) {
		g := newTestGame(t, 2)
		forceHand(t, g, 0, sameA...)
		forceHand(t, g, 1, sameB...)
		for seat := 0; seat < 2; seat++ {
			if _, err := g.Apply(seat, Action{Kind: ActLook}); err != nil {
				t.Fatalf("看牌失败: %v", err)
			}
		}
		if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 1}); err != nil {
			t.Fatalf("比牌失败: %v", err)
		}
		if g.Players[0].Status != StatusFolded {
			t.Fatal("都看牌且一样大时，主动比牌的一方该输")
		}
		if g.Winner != 1 {
			t.Fatalf("赢家 = %d, 期望 1", g.Winner)
		}
	})

	t.Run("都闷则比牌者输", func(t *testing.T) {
		g := newTestGame(t, 2)
		forceHand(t, g, 0, sameA...)
		forceHand(t, g, 1, sameB...)
		if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 1}); err != nil {
			t.Fatalf("比牌失败: %v", err)
		}
		if g.Players[0].Status != StatusFolded {
			t.Fatal("都闷且一样大时，主动比牌的一方该输")
		}
	})
}

func TestCompareValidation(t *testing.T) {
	g := newTestGame(t, 3)
	if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 0}); err == nil {
		t.Fatal("不该能跟自己比")
	}
	if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 9}); err == nil {
		t.Fatal("非法座位应当被拒")
	}
	// 弃牌的人不能被比（弃牌只能在自己回合做）
	folder := g.Turn
	if _, err := g.Apply(folder, Action{Kind: ActFold}); err != nil {
		t.Fatalf("弃牌失败: %v", err)
	}
	if _, err := g.Apply(g.Turn, Action{Kind: ActCompare, Target: folder}); err == nil {
		t.Fatal("不该能跟已经弃牌的人比")
	}
}

func TestMaxRoundsForcesCompareOrFold(t *testing.T) {
	g := newTestGame(t, 3)
	g.RoundInHand = g.MaxRounds

	if _, err := g.Apply(0, Action{Kind: ActCall}); err == nil {
		t.Fatal("圈数到顶后不该还能跟注")
	}
	if _, err := g.Apply(0, Action{Kind: ActRaise, Amount: 4}); err == nil {
		t.Fatal("圈数到顶后不该还能加注")
	}
	// 比牌仍然可以
	if _, err := g.Apply(0, Action{Kind: ActCompare, Target: 1}); err != nil {
		t.Fatalf("圈数到顶后应当还能比牌: %v", err)
	}
	if pv := g.PrivateView(g.Turn); !pv.MustCompare {
		t.Fatal("私密视图应当提示必须比牌")
	}
}

func TestMaxStakeBlocksRaise(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(2)), 3, 200, 2, 6, 6)
	// 一路加注到顶
	if _, err := g.Apply(0, Action{Kind: ActRaise, Amount: 4}); err != nil {
		t.Fatalf("加注失败: %v", err)
	}
	if _, err := g.Apply(1, Action{Kind: ActRaise, Amount: 6}); err != nil {
		t.Fatalf("加注到顶失败: %v", err)
	}
	if _, err := g.Apply(2, Action{Kind: ActRaise, Amount: 8}); err == nil {
		t.Fatal("到顶之后不该还能加注")
	}
	if g.CanRaise(2) {
		t.Fatal("到顶之后 CanRaise 应当为 false")
	}
	// 跟注与比牌仍然可以
	if _, err := g.Apply(2, Action{Kind: ActCall}); err != nil {
		t.Fatalf("到顶后应当还能跟注: %v", err)
	}
}

// TestChipsConservedAndElimination 筹码只在玩家之间流动；输光出局；只剩一人整桌结束。
func TestChipsConservedAndElimination(t *testing.T) {
	g := NewGame(rand.New(rand.NewSource(7)), 3, 20, 2, 10, 3)
	start := totalChips(g)

	for round := 0; round < 30 && g.Phase != PhaseOver; round++ {
		playHand(t, g)
		if got := totalChips(g); got != start {
			t.Fatalf("第 %d 局后筹码总额 = %d, 期望 %d（不该凭空产生）", round+1, got, start)
		}
		if g.Phase == PhaseSettle {
			g.NextRound()
		}
	}

	if g.Phase != PhaseOver {
		t.Fatalf("打了很多局之后应当整桌结束，实际 %s", g.Phase)
	}
	if g.TableWinner < 0 {
		t.Fatal("整桌结束应当有赢家")
	}
	if g.Chips[g.TableWinner] <= 0 {
		t.Fatalf("整桌赢家的筹码应当为正: %d", g.Chips[g.TableWinner])
	}
	if _, err := g.Apply(g.TableWinner, Action{Kind: ActCall}); err == nil {
		t.Fatal("整桌结束后不该还能行动")
	}
}

// playHand 用"都看牌、一直跟注、到顶就比牌"的笨策略把一局打完。
func playHand(t *testing.T, g *Game) {
	t.Helper()
	for step := 0; g.Phase == PhaseBetting; step++ {
		if step > 500 {
			t.Fatal("本局没有收敛")
		}
		seat := g.Turn
		pv := g.PrivateView(seat)

		if !pv.Looked {
			if _, err := g.Apply(seat, Action{Kind: ActLook}); err != nil {
				t.Fatalf("座位 %d 看牌失败: %v", seat, err)
			}
			continue
		}
		if pv.MustCompare || !pv.CanRaise {
			target := -1
			for _, s := range g.playingSeats() {
				if s != seat {
					target = s
					break
				}
			}
			if target < 0 {
				t.Fatal("找不到可以比牌的对象")
			}
			if _, err := g.Apply(seat, Action{Kind: ActCompare, Target: target}); err != nil {
				t.Fatalf("座位 %d 比牌失败: %v", seat, err)
			}
			continue
		}
		if _, err := g.Apply(seat, Action{Kind: ActCall}); err != nil {
			t.Fatalf("座位 %d 跟注失败: %v", seat, err)
		}
	}
	if g.Winner < 0 {
		t.Fatalf("本局应当有赢家，事件: %s", g.LastEvent)
	}
	if len(g.Players[g.Winner].Cards) != 3 {
		t.Fatal("赢家手里该有牌")
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
