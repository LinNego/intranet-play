package ui

import (
	"fmt"
	"strings"
	"sync"

	"intranet-play/internal/zjh"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// 炸金花界面样式：与全局一致，只有"默认 / 暗灰 / 加粗"，不用彩色。
var (
	zjhBlindStyle = mutedStyle
	zjhWinStyle   = boldStyle
	zjhLoseStyle  = mutedStyle
	zjhWarnStyle  = boldStyle
)

const zjhMaxLogLines = 5

// ZjhSeatInfo 是界面上一个座位的展示信息。
type ZjhSeatInfo struct {
	Name   string
	Online bool
	IsHost bool
	Looked bool
	Status zjh.Status
	Bet    int
	Chips  int
}

// ZjhFrame 是炸金花界面的一帧快照。
type ZjhFrame struct {
	Room        string
	You         int
	Round       int
	Started     bool
	Phase       zjh.Phase
	Turn        int
	Pot         int
	Stake       int
	Ante        int
	MaxStake    int
	RoundInHand int
	MaxRounds   int
	Seats       []ZjhSeatInfo
	Looked      bool
	Cards       []zjh.Card
	HandLabel   string
	Cost        int
	CanRaise    bool
	MustCompare bool
	PeekText    string
	Results     []zjh.Result
	Winner      int
	TableWinner int
	Over        bool
	Log         []string
}

// ZjhFrameMsg 把新的一帧推给 TUI。
type ZjhFrameMsg struct {
	Frame ZjhFrame
}

// ZjhApp 是炸金花用的 bubbletea 外壳。与另外两款一样，UpdateFrame 在 Run 之前
// 只缓存最新一帧：Program.Send 在 Run 之前会永久阻塞，主 goroutine 调用会 deadlock。
type ZjhApp struct {
	program *tea.Program
	model   *zjhModel
	cmds    chan string
	done    chan struct{}

	mu      sync.Mutex
	started bool
	pending *ZjhFrame
}

func NewZjhApp(initial ZjhFrame) *ZjhApp {
	cmds := make(chan string, 16)
	m := &zjhModel{frame: initial, cmds: cmds}
	p := tea.NewProgram(m, tea.WithAltScreen())
	return &ZjhApp{program: p, model: m, cmds: cmds, done: make(chan struct{})}
}

func (a *ZjhApp) Run() error {
	defer close(a.done)
	a.applyPending()
	_, err := a.program.Run()
	return err
}

func (a *ZjhApp) applyPending() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = true
	if a.pending != nil {
		a.model.frame = *a.pending
		a.pending = nil
	}
}

func (a *ZjhApp) Done() <-chan struct{}   { return a.done }
func (a *ZjhApp) Commands() <-chan string { return a.cmds }

func (a *ZjhApp) UpdateFrame(f ZjhFrame) {
	a.mu.Lock()
	if !a.started {
		frame := f
		a.pending = &frame
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.program.Send(ZjhFrameMsg{Frame: f})
}

func (a *ZjhApp) Quit() { a.program.Quit() }

type zjhModel struct {
	frame ZjhFrame
	input string
	cmds  chan string
}

func (m *zjhModel) Init() tea.Cmd { return nil }

func (m *zjhModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ZjhFrameMsg:
		m.frame = msg.Frame
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			select {
			case m.cmds <- "leave":
			default:
			}
			return m, tea.Quit
		case tea.KeyEnter:
			line := strings.TrimSpace(m.input)
			m.input = ""
			if line == "" {
				return m, nil
			}
			if line == "quit" || line == "exit" {
				line = "leave"
			}
			select {
			case m.cmds <- line:
			default:
			}
			if line == "leave" {
				return m, tea.Quit
			}
			return m, nil
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				r := []rune(m.input)
				m.input = string(r[:len(r)-1])
			}
			return m, nil
		case tea.KeyRunes:
			m.input += string(msg.Runes)
			return m, nil
		case tea.KeySpace:
			m.input += " "
			return m, nil
		}
	}
	return m, nil
}

func (m *zjhModel) View() string {
	var b strings.Builder
	f := m.frame

	b.WriteString(LogoBlock(
		"nx-sync 0.4  |  peer table",
		zjhMetaLine(f),
		zjhTagline(f),
	))
	b.WriteByte('\n')
	b.WriteString(zjhSeatLine(f))
	b.WriteByte('\n')
	b.WriteString(Rule(56))
	b.WriteByte('\n')
	b.WriteString(zjhMyCardsLine(f))
	b.WriteByte('\n')
	b.WriteString(zjhTableLine(f))
	b.WriteByte('\n')
	if f.PeekText != "" {
		b.WriteString(Bullet(zjhWarnStyle.Render("peek: " + f.PeekText)))
		b.WriteByte('\n')
	}
	if f.Phase == zjh.PhaseSettle && len(f.Results) > 0 {
		b.WriteString(zjhSettleBlock(f))
	}
	b.WriteString(Bullet(statusStyle.Render(zjhStatusText(f))))
	b.WriteByte('\n')
	for _, line := range tail(f.Log, zjhMaxLogLines) {
		b.WriteString(Bullet(ddzLogStyle.Render(line)))
		b.WriteByte('\n')
	}
	b.WriteString(InputBox(m.input, 60))
	b.WriteByte('\n')
	b.WriteString(Footer("? " + zjhHelpLine(f)))
	return b.String()
}

// zjhTagline 是抬头第三行：这桌在玩什么、有什么规矩。
func zjhTagline(f ZjhFrame) string {
	people := len(f.Seats)
	if people == 0 {
		return "炸金花 · 等开局"
	}
	return fmt.Sprintf("炸金花 · %d 人 · 闷牌半价、闷牌平局占优", people)
}

func (f ZjhFrame) seat(seat int) *ZjhSeatInfo {
	if seat < 0 || seat >= len(f.Seats) {
		return nil
	}
	return &f.Seats[seat]
}

func (f ZjhFrame) seatName(seat int) string {
	s := f.seat(seat)
	if s == nil || strings.TrimSpace(s.Name) == "" {
		return fmt.Sprintf("座位%d", seat)
	}
	return runewidth.Truncate(s.Name, 10, "")
}

func zjhMetaLine(f ZjhFrame) string {
	chips := 0
	if s := f.seat(f.You); s != nil {
		chips = s.Chips
	}
	round := fmt.Sprintf("%d", f.Round)
	if !f.Started {
		round = "-"
	}
	return fmt.Sprintf("room=%s  round=%s  奖池 %d  注额 %d  轮 %d/%d  you=%s  筹码 %d",
		f.Room, round, f.Pot, f.Stake, f.RoundInHand, f.MaxRounds, f.seatName(f.You), chips)
}

func zjhSeatLine(f ZjhFrame) string {
	parts := make([]string, 0, len(f.Seats))
	for i, s := range f.Seats {
		tag := fmt.Sprintf("[%d]%s", i, zjhShortName(s.Name))
		if i == f.You {
			tag += "(你)"
		}
		if s.IsHost {
			tag += "^"
		}
		if s.Bet > 0 {
			tag += fmt.Sprintf(" 投%d", s.Bet)
		}
		if text := zjhStandingText(s); text != "" {
			tag += " " + text
		}
		if !s.Online {
			tag += "(离线)"
		}
		if f.Started && f.Phase == zjh.PhaseBetting && i == f.Turn {
			tag = ddzTurnStyle.Render(tag + " *")
		} else {
			tag = metaStyle.Render(tag)
		}
		parts = append(parts, tag)
	}
	return "seats: " + strings.Join(parts, "  ")
}

func zjhShortName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "空位"
	}
	return runewidth.Truncate(name, 8, "")
}

// zjhStandingText 描述座位处境：出局 / 弃牌 / 闷 / 明。
func zjhStandingText(s ZjhSeatInfo) string {
	switch s.Status {
	case zjh.StatusOut:
		return "[出局]"
	case zjh.StatusFolded:
		return "弃牌"
	}
	if s.Looked {
		return "明"
	}
	return "闷"
}

func zjhCardsText(cards []zjh.Card) string {
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		parts = append(parts, ddzCardText(c))
	}
	return strings.Join(parts, " ")
}

// zjhMyCardsLine 显示自己的牌：没看牌时连自己也不显示。
func zjhMyCardsLine(f ZjhFrame) string {
	if !f.Started {
		return headerStyle.Render("你的牌: ") + metaStyle.Render("（还没开局）")
	}
	if !f.Looked {
		return headerStyle.Render("你的牌: ") + zjhBlindStyle.Render("? ? ?（闷牌，输入 look 看牌）")
	}
	if len(f.Cards) == 0 {
		return headerStyle.Render("你的牌: ") + metaStyle.Render("（还没发牌）")
	}
	return headerStyle.Render("你的牌: ") + zjhCardsText(f.Cards) + "   " +
		statusStyle.Render(f.HandLabel)
}

// zjhTableLine 显示跟注要付多少、能不能加注。
func zjhTableLine(f ZjhFrame) string {
	if !f.Started || f.Phase != zjh.PhaseBetting {
		return headerStyle.Render("下注: ") + metaStyle.Render("（本局已结束）")
	}
	cost := fmt.Sprintf("跟注/比牌要付 %d", f.Cost)
	if !f.Looked {
		cost += "（闷牌半价）"
	}
	raise := fmt.Sprintf("加注上限 %d", f.MaxStake)
	if !f.CanRaise {
		raise = "已经不能加注"
	}
	if f.MustCompare {
		raise = "圈数到顶，只能比牌或弃牌"
	}
	return headerStyle.Render("下注: ") + metaStyle.Render(cost+"    "+raise)
}

func zjhSettleBlock(f ZjhFrame) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("---- 本局结算 ----  "))
	if f.Winner >= 0 {
		b.WriteString(statusStyle.Render(fmt.Sprintf("%s 赢下奖池", f.seatName(f.Winner))))
	}
	b.WriteByte('\n')
	for _, r := range f.Results {
		style, sign := zjhDeltaStyle(r.Delta)
		fmt.Fprintf(&b, "  %d %s  ", r.Seat, f.seatName(r.Seat))
		b.WriteString(style.Render(fmt.Sprintf("%s  筹码 %d", sign, r.Chips)))
		b.WriteString(metaStyle.Render("  " + r.Reason))
		b.WriteByte('\n')
	}
	return b.String()
}

func zjhDeltaStyle(delta int) (lipgloss.Style, string) {
	switch {
	case delta > 0:
		return zjhWinStyle, fmt.Sprintf("+%d", delta)
	case delta < 0:
		return zjhLoseStyle, fmt.Sprintf("%d", delta)
	default:
		return metaStyle, "±0"
	}
}

func zjhStatusText(f ZjhFrame) string {
	if !f.Started {
		return "大厅：人齐后由房主输入 start"
	}
	if f.Over {
		if f.TableWinner >= 0 {
			return fmt.Sprintf("整桌结束：%s 赢到最后（房主 restart 可重开）", f.seatName(f.TableWinner))
		}
		return "整桌结束（房主 restart 可重开）"
	}
	mine := f.seat(f.You)
	if mine != nil && mine.Status == zjh.StatusOut {
		return "你已出局（房主 restart 可重开）"
	}
	switch f.Phase {
	case zjh.PhaseBetting:
		if mine != nil && mine.Status == zjh.StatusFolded {
			return "你已弃牌，等本局结束"
		}
		if f.Turn == f.You {
			if f.MustCompare {
				return "轮到你：圈数到顶，只能比牌或弃牌"
			}
			if !f.Looked {
				return "轮到你：还没看牌（可以 look，也可以闷着跟）"
			}
			return "轮到你：跟注 / 加注 / 比牌 / 弃牌"
		}
		return fmt.Sprintf("等待 %s 行动", f.seatName(f.Turn))
	case zjh.PhaseSettle:
		return "本局结束（输入 next 开下一局）"
	}
	return ""
}

func zjhHelpLine(f ZjhFrame) string {
	switch {
	case !f.Started:
		return "cmd: start（房主）| chat TEXT | help | leave"
	case f.Over:
		return "cmd: restart（房主）| chat TEXT | help | leave"
	case f.Phase == zjh.PhaseSettle:
		return "cmd: next（下一局）| chat TEXT | help | leave"
	case f.Phase == zjh.PhaseBetting:
		mine := f.seat(f.You)
		if mine == nil || mine.Status != zjh.StatusPlaying {
			if f.Looked {
				return "cmd: chat TEXT | help | leave"
			}
			return "cmd: look（看牌）| chat TEXT | help | leave"
		}
		if f.Turn != f.You {
			if f.Looked {
				return "cmd: chat TEXT | help | leave（等别人行动）"
			}
			return "cmd: look（看牌随时可用）| chat TEXT | help | leave"
		}
		lookPart := "look | "
		if f.Looked {
			lookPart = ""
		}
		base := "cmd: " + lookPart + "call（跟注）| fold（弃牌）| compare 0（比牌）| help | leave"
		if f.CanRaise {
			base = fmt.Sprintf("cmd: %scall | raise %d（加注）| fold | compare 0 | help | leave", lookPart, f.Stake+1)
		}
		return base
	}
	return "cmd: help | leave"
}
