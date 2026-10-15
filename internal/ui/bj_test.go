package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"intranet-play/internal/bj"
	"intranet-play/internal/ddz"

	tea "github.com/charmbracelet/bubbletea"
)

// bjHand 按点数标签取牌（顺序即手牌顺序）。
func bjHand(t *testing.T, labels ...string) []bj.Card {
	t.Helper()
	deck := ddz.NewDeck()
	used := make(map[int]bool, len(labels))
	out := make([]bj.Card, 0, len(labels))
	for _, label := range labels {
		found := false
		for _, c := range deck {
			if used[c.ID] || c.Rank.Label() != label {
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

func bjBaseFrame(t *testing.T) BjFrame {
	t.Helper()
	return BjFrame{
		Room:    "9988",
		You:     1,
		Round:   3,
		Started: true,
		Phase:   bj.PhasePlaying,
		Turn:    1,
		Seats: []BjSeatInfo{
			{Name: "Alice", Online: true, IsHost: true, Bet: 10, Status: bj.StatusStood, Chips: 90,
				Cards: bjHand(t, "K", "9"), Score: 19},
			{Name: "Bob", Online: true, Bet: 20, Status: bj.StatusWaiting, Chips: 80,
				Cards: bjHand(t, "10", "5"), Score: 15},
			{Name: "Cara", Online: true, Bet: 10, Status: bj.StatusBust, Chips: 90,
				Cards: bjHand(t, "K", "Q", "2"), Score: 22},
		},
		Dealer: BjDealerInfo{Up: bjHand(t, "6"), Hidden: 1, Score: 6},
	}
}

func TestBjViewRendersTable(t *testing.T) {
	f := bjBaseFrame(t)
	f.Log = []string{"Bob 下注 20", "Cara 爆牌"}

	out := (&bjModel{frame: f}).View()

	for _, want := range []string{
		"nx-sync 0.3",
		"room=9988",
		"round=3",
		"筹码 80", // 我的筹码
		"phase=PLAYING",
		"[0]Alice",
		"[1]Bob(你)",
		"注20",
		"19 点",
		"爆牌(22 点)",
		"庄家:",
		"?",      // 暗牌占位
		"明牌 6 点", // 结算前只报明牌点数
		"你:",
		"15 点",
		"其他:",
		"轮到你",
		"hit",
		"stand",
		"double",
		"log: Bob 下注 20",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("界面缺少 %q\n%s", want, out)
		}
	}
	// 结算前不能把庄家点数当成最终点数
	if strings.Contains(out, "庄家") && strings.Contains(out, "爆牌") && strings.Contains(out, "庄家 爆牌") {
		t.Fatalf("结算前不该给出庄家最终结论\n%s", out)
	}
}

func TestBjViewLobbyAndSettle(t *testing.T) {
	lobby := BjFrame{
		Room: "9988",
		Seats: []BjSeatInfo{
			{Name: "Alice", Online: true, IsHost: true, Chips: 100},
			{Name: "Bob", Online: true, Chips: 100},
		},
	}
	out := (&bjModel{frame: lobby}).View()
	for _, want := range []string{"phase=LOBBY", "round=-", "start（房主）", "大厅", "还没有牌"} {
		if !strings.Contains(out, want) {
			t.Fatalf("大厅界面缺少 %q\n%s", want, out)
		}
	}

	settle := bjBaseFrame(t)
	settle.Phase = bj.PhaseSettle
	settle.Turn = -1
	settle.Dealer = BjDealerInfo{Up: bjHand(t, "6", "10"), Score: 16, Final: true}
	settle.Results = []bj.Result{
		{Seat: 0, Delta: 10, Chips: 110, Reason: "19 点大过庄家 16 点"},
		{Seat: 1, Delta: -20, Chips: 80, Reason: "15 点小于庄家 16 点"},
		{Seat: 2, Delta: -10, Chips: 90, Reason: "爆牌（22 点）"},
	}
	settle.Seats[0].Chips = 110
	settle.Seats[2].Chips = 90

	out = (&bjModel{frame: settle}).View()
	for _, want := range []string{
		"本局结算",
		"+10",
		"-20",
		"筹码 110",
		"19 点大过庄家 16 点",
		"next（下一轮）",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("结算界面缺少 %q\n%s", want, out)
		}
	}
	// 结算后庄家应当翻开：不能再出现暗牌占位
	if strings.Contains(out, "?") {
		t.Fatalf("结算后庄家应当已翻开\n%s", out)
	}
}

func TestBjHelpLineFollowsPhase(t *testing.T) {
	cases := []struct {
		name  string
		frame BjFrame
		want  string
	}{
		{name: "大厅", frame: BjFrame{}, want: "start（房主）"},
		{name: "下注", frame: BjFrame{Started: true, Phase: bj.PhaseBetting}, want: "bet 10"},
		{name: "行动", frame: BjFrame{Started: true, Phase: bj.PhasePlaying}, want: "hit（要牌）"},
		{name: "结算", frame: BjFrame{Started: true, Phase: bj.PhaseSettle}, want: "next（下一轮）"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bjHelpLine(tc.frame); !strings.Contains(got, tc.want) {
				t.Fatalf("帮助行 = %q, 期望包含 %q", got, tc.want)
			}
		})
	}
}

func TestBjModelInputProducesCommands(t *testing.T) {
	cmds := make(chan string, 8)
	m := &bjModel{frame: bjBaseFrame(t), cmds: cmds}

	typeKeys := func(m *bjModel, line string) *bjModel {
		for _, r := range line {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
			if r == ' ' {
				msg = tea.KeyMsg{Type: tea.KeySpace}
			}
			next, _ := m.Update(msg)
			m = next.(*bjModel)
		}
		return m
	}

	m = typeKeys(m, "bet 10")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*bjModel)
	if cmd != nil {
		t.Fatal("普通命令不该退出程序")
	}
	if got := <-cmds; got != "bet 10" {
		t.Fatalf("命令 = %q, 期望 %q", got, "bet 10")
	}
	if m.input != "" {
		t.Fatalf("回车后输入框应当清空，实际 %q", m.input)
	}

	// quit 等价于 leave 并退出
	m2 := typeKeys(&bjModel{frame: bjBaseFrame(t), cmds: cmds}, "quit")
	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit 应当退出程序")
	}
	if got := <-cmds; got != "leave" {
		t.Fatalf("quit 应当转成 leave，实际 %q", got)
	}
}

// TestBjAppUpdateFrameBeforeRunDoesNotBlock 与斗地主同款防护：
// bubbletea 的 Program.Send 在 Run 之前会永久阻塞，主 goroutine 调用会 deadlock。
func TestBjAppUpdateFrameBeforeRunDoesNotBlock(t *testing.T) {
	app := NewBjApp(bjBaseFrame(t))

	updated := bjBaseFrame(t)
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

	if got := app.model.frame.Round; got != 3 {
		t.Fatalf("Run 之前不该改动模型: round=%d", got)
	}
	app.applyPending()
	if got := app.model.frame.Round; got != 9 {
		t.Fatalf("启动前缓存的帧没有生效: round=%d", got)
	}
	if out := app.model.View(); !strings.Contains(out, "round=9") || !strings.Contains(out, fmt.Sprint("启动前的日志")) {
		t.Fatalf("界面没有用上启动前的帧\n%s", out)
	}
}
