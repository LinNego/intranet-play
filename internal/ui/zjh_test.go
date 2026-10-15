package ui

import (
	"strings"
	"testing"
	"time"

	"intranet-play/internal/ddz"
	"intranet-play/internal/zjh"

	tea "github.com/charmbracelet/bubbletea"
)

// zjhHand 按 "A♠" 取牌（炸金花要看花色）。
func zjhHand(t *testing.T, labels ...string) []zjh.Card {
	t.Helper()
	deck := ddz.NewDeck()
	used := make(map[int]bool, len(labels))
	out := make([]zjh.Card, 0, len(labels))
	for _, label := range labels {
		found := false
		for _, c := range deck {
			if used[c.ID] || c.Label() != label {
				continue
			}
			used[c.ID] = true
			out = append(out, c)
			found = true
			break
		}
		if !found {
			t.Fatalf("牌堆里取不出 %q", label)
		}
	}
	return out
}

func zjhBaseFrame(t *testing.T) ZjhFrame {
	t.Helper()
	return ZjhFrame{
		Room:        "9988",
		You:         1,
		Round:       3,
		Started:     true,
		Phase:       zjh.PhaseBetting,
		Turn:        1,
		Pot:         26,
		Stake:       4,
		Ante:        1,
		MaxStake:    20,
		RoundInHand: 2,
		MaxRounds:   6,
		Cost:        2,
		CanRaise:    true,
		Seats: []ZjhSeatInfo{
			{Name: "Alice", Online: true, IsHost: true, Looked: true, Status: zjh.StatusPlaying, Bet: 8, Chips: 92},
			{Name: "Bob", Online: true, Looked: false, Status: zjh.StatusPlaying, Bet: 6, Chips: 94},
			{Name: "Cara", Online: true, Looked: false, Status: zjh.StatusFolded, Bet: 1, Chips: 99},
		},
	}
}

func TestZjhViewBlindAndLooked(t *testing.T) {
	f := zjhBaseFrame(t)

	// 没看牌：连自己的牌都不显示
	out := (&zjhModel{frame: f}).View()
	for _, want := range []string{"nx-sync 0.4", "奖池 26", "注额 4", "轮 2/6", "筹码 94",
		"? ? ?", "闷牌", "[0]Alice", "明", "弃牌", "轮到你：还没看牌"} {
		if !strings.Contains(out, want) {
			t.Fatalf("界面缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "♠") || strings.Contains(out, "♥") {
		t.Fatalf("没看牌时界面上不该出现牌面\n%s", out)
	}

	// 看牌之后显示牌面与牌型
	f.Looked = true
	f.Cards = zjhHand(t, "A♠", "K♥", "7♦")
	f.HandLabel = "散牌"
	out = (&zjhModel{frame: f}).View()
	for _, want := range []string{"A♠", "K♥", "7♦", "散牌", "跟注/比牌要付 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("看牌后界面缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "? ? ?") {
		t.Fatalf("看牌后不该还有 ? ? ?\n%s", out)
	}
}

func TestZjhViewSeatMarkersAndSettle(t *testing.T) {
	f := zjhBaseFrame(t)
	f.Looked = true
	f.Cards = zjhHand(t, "2♠", "5♥", "9♦")
	f.HandLabel = "散牌"
	f.Seats[2].Status = zjh.StatusOut
	f.Seats[2].Chips = 0
	out := (&zjhModel{frame: f}).View()
	for _, want := range []string{"[出局]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("界面缺少 %q\n%s", want, out)
		}
	}

	// 结算面板
	f.Phase = zjh.PhaseSettle
	f.Turn = -1
	f.Winner = 1
	f.Results = []zjh.Result{
		{Seat: 0, Delta: -8, Chips: 92, Reason: "本局投入"},
		{Seat: 1, Delta: 20, Chips: 114, Reason: "赢下奖池"},
		{Seat: 2, Delta: -1, Chips: 99, Reason: "本局投入"},
	}
	out = (&zjhModel{frame: f}).View()
	for _, want := range []string{"本局结算", "赢下奖池", "+20", "-8", "筹码 114"} {
		if !strings.Contains(out, want) {
			t.Fatalf("结算界面缺少 %q\n%s", want, out)
		}
	}

	// 整桌结束
	f.Over = true
	f.TableWinner = 1
	out = (&zjhModel{frame: f}).View()
	if !strings.Contains(out, "整桌结束") || !strings.Contains(out, "Bob") {
		t.Fatalf("整桌结束提示不对\n%s", out)
	}
}

func TestZjhViewPeekText(t *testing.T) {
	f := zjhBaseFrame(t)
	f.Looked = true
	f.PeekText = "散牌 A♠ K♥ 7♦ vs 座位0 散牌 A♥ K♦ 7♣ → 你输"
	out := (&zjhModel{frame: f}).View()
	if !strings.Contains(out, "peek:") || !strings.Contains(out, "你输") {
		t.Fatalf("比牌结果没显示\n%s", out)
	}
}

func TestZjhHelpLineFollowsPhase(t *testing.T) {
	cases := []struct {
		name  string
		frame ZjhFrame
		want  string
	}{
		{name: "大厅", frame: ZjhFrame{}, want: "start（房主）"},
		{name: "不是我的回合", frame: ZjhFrame{Started: true, Phase: zjh.PhaseBetting, Turn: 0, You: 1,
			Seats: []ZjhSeatInfo{{Status: zjh.StatusPlaying}, {Status: zjh.StatusPlaying}}}, want: "look（看牌随时可用）"},
		{name: "轮到我", frame: ZjhFrame{Started: true, Phase: zjh.PhaseBetting, Turn: 1, You: 1, Stake: 4, CanRaise: true,
			Seats: []ZjhSeatInfo{{Status: zjh.StatusPlaying}, {Status: zjh.StatusPlaying}}}, want: "raise 5"},
		{name: "不能加注", frame: ZjhFrame{Started: true, Phase: zjh.PhaseBetting, Turn: 1, You: 1, Stake: 20, CanRaise: false,
			Seats: []ZjhSeatInfo{{Status: zjh.StatusPlaying}, {Status: zjh.StatusPlaying}}}, want: "cmd: look | call（跟注）"},
		{name: "结算", frame: ZjhFrame{Started: true, Phase: zjh.PhaseSettle}, want: "next（下一局）"},
		{name: "整桌结束", frame: ZjhFrame{Started: true, Over: true}, want: "restart（房主）"},
		{name: "已弃牌", frame: ZjhFrame{Started: true, Phase: zjh.PhaseBetting, Turn: 0,
			Seats: []ZjhSeatInfo{{Status: zjh.StatusPlaying}, {Status: zjh.StatusFolded}}}, want: "look"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := zjhHelpLine(tc.frame); !strings.Contains(got, tc.want) {
				t.Fatalf("帮助行 = %q, 期望包含 %q", got, tc.want)
			}
		})
	}
}

// TestZjhAppUpdateFrameBeforeRunDoesNotBlock 与另外两款同款防护：
// bubbletea 的 Program.Send 在 Run 之前会永久阻塞，主 goroutine 调用会 deadlock。
func TestZjhAppUpdateFrameBeforeRunDoesNotBlock(t *testing.T) {
	app := NewZjhApp(zjhBaseFrame(t))

	updated := zjhBaseFrame(t)
	updated.Round = 9
	updated.Log = []string{"启动前的日志"}

	done := make(chan struct{})
	go func() {
		app.UpdateFrame(updated)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 之前调用 UpdateFrame 阻塞了：在真实终端里会 deadlock")
	}

	app.applyPending()
	if got := app.model.frame.Round; got != 9 {
		t.Fatalf("启动前缓存的帧没有生效: round=%d", got)
	}
	if out := app.model.View(); !strings.Contains(out, "round=9") {
		t.Fatalf("界面没有用上启动前的帧\n%s", out)
	}
}

func TestZjhModelInputProducesCommands(t *testing.T) {
	cmds := make(chan string, 8)
	m := &zjhModel{frame: zjhBaseFrame(t), cmds: cmds}

	typeKeys := func(m *zjhModel, line string) *zjhModel {
		for _, r := range line {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
			if r == ' ' {
				msg = tea.KeyMsg{Type: tea.KeySpace}
			}
			next, _ := m.Update(msg)
			m = next.(*zjhModel)
		}
		return m
	}

	m = typeKeys(m, "raise 8")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*zjhModel)
	if cmd != nil {
		t.Fatal("普通命令不该退出程序")
	}
	if got := <-cmds; got != "raise 8" {
		t.Fatalf("命令 = %q, 期望 %q", got, "raise 8")
	}
	if m.input != "" {
		t.Fatalf("回车后输入框应当清空，实际 %q", m.input)
	}

	m2 := typeKeys(&zjhModel{frame: zjhBaseFrame(t), cmds: cmds}, "quit")
	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit 应当退出程序")
	}
	if got := <-cmds; got != "leave" {
		t.Fatalf("quit 应当转成 leave，实际 %q", got)
	}
}
