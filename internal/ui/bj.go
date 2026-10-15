package ui

import (
	"fmt"
	"strings"
	"sync"

	"intranet-play/internal/bj"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// 21 点界面样式。牌面颜色沿用斗地主那套（同一个包里的 ddzCardText）。
var (
	bjWinStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("150"))
	bjLoseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
	bjPushStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

const bjMaxLogLines = 5

// BjSeatInfo 是界面上一个座位的展示信息。
type BjSeatInfo struct {
	Name   string
	Online bool
	IsHost bool
	Bet    int
	Status bj.Status
	Cards  []bj.Card
	Score  int
	Soft   bool
	Chips  int
}

// BjDealerInfo 是庄家的公开信息。结算前只有一张明牌，暗牌只知道张数。
type BjDealerInfo struct {
	Up     []bj.Card
	Hidden int
	Score  int
	Soft   bool
	Final  bool
}

// BjFrame 是 21 点界面的一帧快照。房主与客户端都靠它渲染，保证两边长得一样。
type BjFrame struct {
	Room    string
	You     int
	Round   int
	Started bool
	Phase   bj.Phase
	Turn    int
	Seats   []BjSeatInfo
	Dealer  BjDealerInfo
	Results []bj.Result
	Log     []string
}

// BjFrameMsg 把新的一帧推给 TUI。
type BjFrameMsg struct {
	Frame BjFrame
}

// BjApp 是 21 点用的 bubbletea 外壳。与 DdzApp 一样，UpdateFrame 在 Run 之前
// 只缓存最新一帧：bubbletea 的 Program.Send 是无缓冲的，Run 之前调用会永久阻塞，
// 如果调用方正是将来要执行 Run 的那个 goroutine 就会 deadlock。
type BjApp struct {
	program *tea.Program
	model   *bjModel
	cmds    chan string
	done    chan struct{}

	mu      sync.Mutex
	started bool
	pending *BjFrame
}

func NewBjApp(initial BjFrame) *BjApp {
	cmds := make(chan string, 16)
	m := &bjModel{frame: initial, cmds: cmds}
	p := tea.NewProgram(m, tea.WithAltScreen())
	return &BjApp{program: p, model: m, cmds: cmds, done: make(chan struct{})}
}

func (a *BjApp) Run() error {
	defer close(a.done)
	a.applyPending()
	_, err := a.program.Run()
	return err
}

func (a *BjApp) applyPending() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = true
	if a.pending != nil {
		a.model.frame = *a.pending
		a.pending = nil
	}
}

func (a *BjApp) Done() <-chan struct{}   { return a.done }
func (a *BjApp) Commands() <-chan string { return a.cmds }

func (a *BjApp) UpdateFrame(f BjFrame) {
	a.mu.Lock()
	if !a.started {
		frame := f
		a.pending = &frame
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.program.Send(BjFrameMsg{Frame: f})
}

func (a *BjApp) Quit() { a.program.Quit() }

type bjModel struct {
	frame BjFrame
	input string
	cmds  chan string
}

func (m *bjModel) Init() tea.Cmd { return nil }

func (m *bjModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case BjFrameMsg:
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

func (m *bjModel) View() string {
	var b strings.Builder
	f := m.frame

	b.WriteString(titleStyle.Render("nx-sync 0.3  |  peer table"))
	b.WriteByte('\n')
	b.WriteString(metaStyle.Render(bjMetaLine(f)))
	b.WriteByte('\n')
	b.WriteString(bjSeatLine(f))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("-", 56))
	b.WriteByte('\n')
	b.WriteString(bjDealerLine(f))
	b.WriteByte('\n')
	b.WriteString(bjYourHandLine(f))
	b.WriteByte('\n')
	b.WriteString(bjOthersLine(f))
	b.WriteByte('\n')
	if f.Phase == bj.PhaseSettle {
		b.WriteString(bjSettleBlock(f))
	}
	b.WriteString(statusStyle.Render("state: " + bjStatusText(f)))
	b.WriteByte('\n')
	b.WriteString(helpStyle.Render(bjHelpLine(f)))
	b.WriteByte('\n')
	for _, line := range tail(f.Log, bjMaxLogLines) {
		b.WriteString(ddzLogStyle.Render("log: " + line))
		b.WriteByte('\n')
	}
	b.WriteString(inputStyle.Render("$ " + m.input + "_"))
	return b.String()
}

func (f BjFrame) seat(seat int) *BjSeatInfo {
	if seat < 0 || seat >= len(f.Seats) {
		return nil
	}
	return &f.Seats[seat]
}

func (f BjFrame) seatName(seat int) string {
	s := f.seat(seat)
	if s == nil {
		return "座位" + fmt.Sprint(seat)
	}
	if strings.TrimSpace(s.Name) == "" {
		return "空位"
	}
	return runewidth.Truncate(s.Name, 10, "")
}

func bjMetaLine(f BjFrame) string {
	chips := 0
	if s := f.seat(f.You); s != nil {
		chips = s.Chips
	}
	phase := string(f.Phase)
	round := fmt.Sprintf("%d", f.Round)
	if !f.Started {
		phase = "LOBBY"
		round = "-"
	}
	return fmt.Sprintf("room=%s  round=%s  you=%s  筹码 %d  phase=%s",
		f.Room, round, f.seatName(f.You), chips, phase)
}

func bjSeatLine(f BjFrame) string {
	parts := make([]string, 0, len(f.Seats))
	for i, s := range f.Seats {
		tag := fmt.Sprintf("[%d]%s", i, bjShortName(s.Name))
		if i == f.You {
			tag += "(你)"
		}
		if s.IsHost {
			tag += "^"
		}
		if s.Bet > 0 {
			tag += fmt.Sprintf(" 注%d", s.Bet)
		}
		if text := bjStandingText(s); text != "" {
			tag += " " + text
		}
		if !s.Online {
			tag += "(离线)"
		}
		if f.Started && f.Phase == bj.PhasePlaying && i == f.Turn {
			tag = ddzTurnStyle.Render(tag + " *")
		} else {
			tag = metaStyle.Render(tag)
		}
		parts = append(parts, tag)
	}
	return "seats: " + strings.Join(parts, "  ")
}

func bjShortName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "空位"
	}
	return runewidth.Truncate(name, 8, "")
}

// bjStandingText 描述一个座位当前的处境，措辞与引擎日志保持一致。
func bjStandingText(s BjSeatInfo) string {
	if len(s.Cards) == 0 {
		switch s.Status {
		case bj.StatusIdle:
			return "未下注"
		case bj.StatusBet:
			return "已下注"
		default:
			return string(s.Status)
		}
	}
	return bj.Describe(s.Cards)
}

func bjCardsText(cards []bj.Card) string {
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		parts = append(parts, ddzCardText(c))
	}
	return strings.Join(parts, " ")
}

func bjDealerLine(f BjFrame) string {
	if len(f.Dealer.Up) == 0 {
		return headerStyle.Render("庄家: ") + metaStyle.Render("（还没有牌）")
	}
	text := bjCardsText(f.Dealer.Up)
	if f.Dealer.Hidden > 0 {
		text += " " + headerStyle.Render("?")
	}
	line := headerStyle.Render("庄家: ") + text
	if f.Dealer.Final {
		total, soft := f.Dealer.Score, f.Dealer.Soft
		desc := fmt.Sprintf("%d 点", total)
		if total > bj.Target {
			desc = fmt.Sprintf("爆牌(%d 点)", total)
		} else if soft {
			desc = fmt.Sprintf("软 %d 点", total)
		}
		return line + "   " + statusStyle.Render(desc)
	}
	return line + "   " + metaStyle.Render(fmt.Sprintf("明牌 %d 点（暗牌未知）", f.Dealer.Score))
}

func bjYourHandLine(f BjFrame) string {
	s := f.seat(f.You)
	if s == nil || len(s.Cards) == 0 {
		return headerStyle.Render("你:   ") + metaStyle.Render("（还没有牌）")
	}
	return headerStyle.Render("你:   ") + bjCardsText(s.Cards) + "   " + statusStyle.Render(bj.Describe(s.Cards))
}

func bjOthersLine(f BjFrame) string {
	parts := make([]string, 0, len(f.Seats))
	for i, s := range f.Seats {
		if i == f.You || len(s.Cards) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", bjShortName(s.Name), bj.Describe(s.Cards)))
	}
	if len(parts) == 0 {
		return headerStyle.Render("其他: ") + metaStyle.Render("（无）")
	}
	return headerStyle.Render("其他: ") + metaStyle.Render(strings.Join(parts, " | "))
}

func bjSettleBlock(f BjFrame) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("---- 本局结算 ----  "))
	if f.Dealer.Final {
		b.WriteString(metaStyle.Render("庄家 " + bjCardsText(f.Dealer.Up)))
	}
	b.WriteByte('\n')
	for _, r := range f.Results {
		style := bjPushStyle
		sign := "±0"
		switch {
		case r.Delta > 0:
			style = bjWinStyle
			sign = fmt.Sprintf("+%d", r.Delta)
		case r.Delta < 0:
			style = bjLoseStyle
			sign = fmt.Sprintf("%d", r.Delta)
		}
		fmt.Fprintf(&b, "  %d %s  注%d  ", r.Seat, f.seatName(r.Seat), f.betOf(r.Seat))
		b.WriteString(style.Render(fmt.Sprintf("%s  筹码 %d", sign, r.Chips)))
		b.WriteString(metaStyle.Render("  " + r.Reason))
		b.WriteByte('\n')
	}
	return b.String()
}

func (f BjFrame) betOf(seat int) int {
	if s := f.seat(seat); s != nil {
		return s.Bet
	}
	return 0
}

func bjStatusText(f BjFrame) string {
	if !f.Started {
		return "大厅：人齐后由房主输入 start"
	}
	switch f.Phase {
	case bj.PhaseBetting:
		if s := f.seat(f.You); s != nil {
			if s.Status == bj.StatusIdle {
				return fmt.Sprintf("轮到你下注（你有 %d 筹码）", s.Chips)
			}
			return "你已下注，等其他人"
		}
	case bj.PhasePlaying:
		if f.Turn == f.You {
			s := f.seat(f.You)
			if s != nil {
				return fmt.Sprintf("轮到你：当前 %s，可以 hit / stand / double", bj.Describe(s.Cards))
			}
		}
		return fmt.Sprintf("等待 %s 行动", f.seatName(f.Turn))
	case bj.PhaseSettle:
		return "本局结束（输入 next 开下一轮）"
	}
	return ""
}

// bjHelpLine 只显示当前阶段能用的命令，省得对着一堆用不上的命令猜。
func bjHelpLine(f BjFrame) string {
	switch {
	case !f.Started:
		return "cmd: start（房主）| chat TEXT | help | leave"
	case f.Phase == bj.PhaseBetting:
		return "cmd: bet 10 | chat TEXT | help | leave"
	case f.Phase == bj.PhasePlaying:
		return "cmd: hit（要牌）| stand（停牌）| double（双倍）| help | leave"
	case f.Phase == bj.PhaseSettle:
		return "cmd: next（下一轮）| chat TEXT | help | leave"
	}
	return "cmd: help | leave"
}
