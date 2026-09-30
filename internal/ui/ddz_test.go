package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"intranet-play/internal/ddz"

	tea "github.com/charmbracelet/bubbletea"
)

func testHand(t *testing.T, labels ...string) []ddz.Card {
	t.Helper()
	deck := ddz.NewDeck()
	used := make(map[int]bool, len(labels))
	out := make([]ddz.Card, 0, len(labels))
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
	ddz.SortDesc(out)
	return out
}

func baseFrame() DdzFrame {
	return DdzFrame{
		Room:    "9988",
		You:     1,
		Round:   3,
		Rounds:  10,
		Base:    1,
		Started: true,
		Phase:   ddz.PhasePlaying,
		Turn:    1,
		Seats: [ddz.Seats]DdzSeatInfo{
			{Name: "Alice", HandCount: 11, Online: true, Ready: true, IsHost: true},
			{Name: "Bob", HandCount: 18, Online: true, Ready: true},
			{Name: "Cara", HandCount: 7, Online: true, Ready: true},
		},
		Landlord:   1,
		BidScore:   2,
		Multiplier: 4,
	}
}

func TestDdzViewRendersHandWithStableNumbering(t *testing.T) {
	f := baseFrame()
	f.Hand = testHand(t, "2", "A", "A", "K", "Q", "J", "10", "9", "8", "7", "6", "5", "4", "3", "3", "3", "w", "W")
	f.LastPlay = &ddz.PlayView{Seat: 0, Kind: ddz.KindBomb, Main: ddz.RankK, Size: 4, Label: "炸弹(K)",
		Cards: testHand(t, "K", "K", "K", "K")}
	f.PassSeat = [ddz.Seats]bool{false, false, true}

	out := (&ddzModel{frame: f}).View()

	for _, want := range []string{
		"nx-sync 0.2",
		"room=9988",
		"round=3/10",
		"phase=PLAYING",
		"Alice",
		"Bob(你)",
		"Cara",
		"地主",
		"炸弹(K)",
		"2:不要",
		"hand(18):",
		"last:",
		"state:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("界面缺少 %q\n%s", want, out)
		}
	}

	// 手牌编号必须从 #1 连续到张数（带 # 前缀，和 play 55 这种牌面写法区分开）
	for i := 1; i <= len(f.Hand); i++ {
		if !strings.Contains(out, fmt.Sprintf("#%d:", i)) {
			t.Fatalf("手牌编号 #%d 没有出现在界面上\n%s", i, out)
		}
	}
	if strings.Contains(out, fmt.Sprintf("#%d:", len(f.Hand)+1)) {
		t.Fatalf("手牌编号超出张数\n%s", out)
	}
	// 18 张牌按每行 6 张排布 → 3 行
	rows := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "#1:") || strings.Contains(line, "#7:") || strings.Contains(line, "#13:") {
			rows++
		}
	}
	if rows != 3 {
		t.Fatalf("手牌应当分 3 行显示，实际 %d 行\n%s", rows, out)
	}
	if !strings.Contains(out, "hint") {
		t.Fatalf("帮助行里应当提示 hint 命令\n%s", out)
	}
}

func TestDdzViewLobbyAndRoundEnd(t *testing.T) {
	lobby := DdzFrame{
		Room:     "9988",
		Rounds:   10,
		Landlord: -1, // 没开局就没有地主，绝不能是零值 0
		Winner:   -1,
		Seats: [ddz.Seats]DdzSeatInfo{
			{Name: "Alice", Online: true, Ready: true, IsHost: true},
			{Name: "Bob", Online: true, Ready: true},
			{Online: true, Ready: false},
		},
	}
	out := (&ddzModel{frame: lobby}).View()
	for _, want := range []string{"phase=LOBBY", "round=-/10", "空位", "大厅"} {
		if !strings.Contains(out, want) {
			t.Fatalf("大厅界面缺少 %q\n%s", want, out)
		}
	}
	for _, bad := range []string{"[地主]", "(离线)", "空位 空位"} {
		if strings.Contains(out, bad) {
			t.Fatalf("大厅界面不该出现 %q\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "[2]空位") {
		t.Fatalf("空位座位应当显示为 [2]空位\n%s", out)
	}

	end := baseFrame()
	end.Phase = ddz.PhaseRoundEnd
	end.Winner = 1
	end.LandlordWin = false
	end.RoundScore = [ddz.Seats]int{-4, 2, 2}
	end.Scores = [ddz.Seats]int{-4, 2, 2}
	end.Reveal = [ddz.Seats][]ddz.Card{
		testHand(t, "3", "4"),
		nil,
		testHand(t, "9"),
	}
	out = (&ddzModel{frame: end}).View()
	for _, want := range []string{"本局结算", "出完", "得分变化", "0:-4(总-4)", "1:+2(总+2)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("结算界面缺少 %q\n%s", want, out)
		}
	}
}

func TestDdzCardColorsAndBidding(t *testing.T) {
	f := baseFrame()
	f.Phase = ddz.PhaseBidding
	f.Landlord = -1
	f.Turn = 2
	f.Bids = [ddz.Seats]int{0, 2, 0}
	f.BidActed = [ddz.Seats]bool{true, true, false}
	f.Hand = testHand(t, "A", "4")

	out := (&ddzModel{frame: f}).View()
	for _, want := range []string{"bidding:", "0:不叫", "1:2分", "2:-", "最高 2 分", "等待 Cara 叫分"} {
		if !strings.Contains(out, want) {
			t.Fatalf("叫分界面缺少 %q\n%s", want, out)
		}
	}
}

// typeKeys 逐字敲入一行文本，模拟真实键盘输入。
func typeKeys(m *ddzModel, line string) *ddzModel {
	for _, r := range line {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace}
		}
		next, _ := m.Update(msg)
		m = next.(*ddzModel)
	}
	return m
}

func TestDdzModelInputProducesCommands(t *testing.T) {
	cmds := make(chan string, 8)
	m := &ddzModel{frame: baseFrame(), cmds: cmds}

	// 输入 "play 1 2" 再回车 → 命令原样交给房间层，输入框清空，程序不退出
	m = typeKeys(m, "play 1 2")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*ddzModel)
	if cmd != nil {
		t.Fatal("普通命令不该退出程序")
	}
	select {
	case got := <-cmds:
		if got != "play 1 2" {
			t.Fatalf("命令 = %q, 期望 %q", got, "play 1 2")
		}
	default:
		t.Fatal("回车没有把命令交给房间层")
	}
	if m.input != "" {
		t.Fatalf("回车后输入框应当清空，实际 %q", m.input)
	}

	// 空行不发命令
	if _, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); len(cmds) != 0 {
		t.Fatal("空行不该产生命令")
	}

	// 退格删一个字符
	m = typeKeys(m, "bid 3")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(*ddzModel)
	if m.input != "bid " {
		t.Fatalf("退格后输入框 = %q, 期望 %q", m.input, "bid ")
	}

	// quit 等价于 leave，并且退出
	m2 := typeKeys(&ddzModel{frame: baseFrame(), cmds: cmds}, "quit")
	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit 应当退出程序")
	}
	if got := <-cmds; got != "leave" {
		t.Fatalf("quit 应当转成 leave，实际 %q", got)
	}

	// Esc 也要能安全退出（先把缓冲清干净）
	for len(cmds) > 0 {
		<-cmds
	}
	m3 := &ddzModel{frame: baseFrame(), cmds: cmds}
	next, cmd = m3.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = next
	if cmd == nil {
		t.Fatal("Esc 应当退出程序")
	}
	if got := <-cmds; got != "leave" {
		t.Fatalf("Esc 应当发送 leave，实际 %q", got)
	}
}

// TestDdzAppUpdateFrameBeforeRunDoesNotBlock 锁死一个真的在终端里炸过的 bug：
// bubbletea 的 Program.Send 是无缓冲的，Run 之前调用会永久阻塞；
// 如果调用方正是那个马上要执行 Run 的 goroutine，程序会直接
// "all goroutines are asleep - deadlock"。
func TestDdzAppUpdateFrameBeforeRunDoesNotBlock(t *testing.T) {
	app := NewDdzApp(baseFrame())

	updated := baseFrame()
	updated.Round = 7
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

	// 没启动之前只缓存，不动模型
	if got := app.model.frame.Round; got != 3 {
		t.Fatalf("Run 之前不该改动模型: round=%d", got)
	}
	// Run 的第一步就是把缓存并进模型
	app.applyPending()
	if got := app.model.frame.Round; got != 7 {
		t.Fatalf("启动前缓存的帧没有生效: round=%d", got)
	}
	if len(app.model.frame.Log) != 1 || app.model.frame.Log[0] != "启动前的日志" {
		t.Fatalf("启动前的日志丢了: %v", app.model.frame.Log)
	}
	if out := app.model.View(); !strings.Contains(out, "round=7/10") {
		t.Fatalf("界面没有用上启动前的帧\n%s", out)
	}
}
