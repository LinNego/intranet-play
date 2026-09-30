package ui

import (
	"fmt"
	"strings"
	"sync"

	"intranet-play/internal/ddz"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// 斗地主界面使用的样式。沿用五子棋那套"像数据面板"的配色，只补牌面颜色。
var (
	ddzRedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	ddzBlackStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ddzJokerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	ddzTurnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Bold(true)
	ddzLandlordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("180"))
	ddzLogStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	ddzWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
)

const (
	ddzCardsPerRow = 6
	ddzMaxLogLines = 5
)

// DdzSeatInfo 是界面上一个座位的展示信息。
type DdzSeatInfo struct {
	Name      string
	HandCount int
	Online    bool
	Ready     bool
	IsHost    bool
}

// DdzFrame 是斗地主界面的一帧快照。房主与客户端都通过它渲染，保证两边长得一样。
type DdzFrame struct {
	Room        string
	You         int
	Round       int
	Rounds      int
	Base        int
	Started     bool
	Phase       ddz.Phase
	Turn        int
	Landlord    int
	BidScore    int
	Multiplier  int
	Bids        [ddz.Seats]int
	BidActed    [ddz.Seats]bool
	Bottom      []ddz.Card
	Seats       [ddz.Seats]DdzSeatInfo
	LastPlay    *ddz.PlayView
	PassSeat    [ddz.Seats]bool
	Scores      [ddz.Seats]int
	RoundScore  [ddz.Seats]int
	Hand        []ddz.Card
	Reveal      [ddz.Seats][]ddz.Card
	Winner      int
	LandlordWin bool
	Log         []string
}

// DdzFrameMsg 把新的一帧推给 TUI。
type DdzFrameMsg struct {
	Frame DdzFrame
}

// DdzApp 是斗地主用的 bubbletea 外壳，接口与五子棋的 App 一致。
//
// UpdateFrame 可以在任何 goroutine、任何时刻调用。注意 bubbletea 的 Program.Send
// 用的是无缓冲 channel：在 Run 之前调用它会永久阻塞，如果调用方正是将来要执行
// Run 的那个 goroutine，就会直接 deadlock。所以 Run 之前只缓存最新一帧，
// 等 Run 启动时再塞进模型（那时事件循环还没起来，改模型是安全的）。
type DdzApp struct {
	program *tea.Program
	model   *ddzModel
	cmds    chan string
	done    chan struct{}

	mu      sync.Mutex
	started bool
	pending *DdzFrame
}

func NewDdzApp(initial DdzFrame) *DdzApp {
	cmds := make(chan string, 16)
	m := &ddzModel{frame: initial, cmds: cmds}
	p := tea.NewProgram(m, tea.WithAltScreen())
	return &DdzApp{program: p, model: m, cmds: cmds, done: make(chan struct{})}
}

func (a *DdzApp) Run() error {
	defer close(a.done)
	a.applyPending()
	_, err := a.program.Run()
	return err
}

// applyPending 把 Run 之前缓存的最新一帧并入模型。
func (a *DdzApp) applyPending() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = true
	if a.pending != nil {
		a.model.frame = *a.pending
		a.pending = nil
	}
}

func (a *DdzApp) Done() <-chan struct{}   { return a.done }
func (a *DdzApp) Commands() <-chan string { return a.cmds }

// UpdateFrame 推送一帧。Run 之前调用只缓存，不会阻塞。
func (a *DdzApp) UpdateFrame(f DdzFrame) {
	a.mu.Lock()
	if !a.started {
		frame := f
		a.pending = &frame
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.program.Send(DdzFrameMsg{Frame: f})
}

func (a *DdzApp) Quit() { a.program.Quit() }

type ddzModel struct {
	frame DdzFrame
	input string
	cmds  chan string
}

func (m *ddzModel) Init() tea.Cmd { return nil }

func (m *ddzModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DdzFrameMsg:
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

func (m *ddzModel) View() string {
	var b strings.Builder
	f := m.frame

	b.WriteString(titleStyle.Render("nx-sync 0.2  |  peer table"))
	b.WriteByte('\n')
	b.WriteString(metaStyle.Render(ddzMetaLine(f)))
	b.WriteByte('\n')
	b.WriteString(ddzSeatLine(f))
	b.WriteByte('\n')
	b.WriteString(ddzTableBlock(f))
	b.WriteString(strings.Repeat("-", 56))
	b.WriteByte('\n')
	b.WriteString(ddzHandBlock(f))
	if f.Phase == ddz.PhaseRoundEnd {
		b.WriteString(ddzRevealBlock(f))
	}
	b.WriteString(statusStyle.Render("state: " + ddzStatusText(f)))
	b.WriteByte('\n')
	b.WriteString(helpStyle.Render("cmd: play 55 | play 34567 | play #1 #2 | bid 0-3 | pass | hint | chat | leave"))
	b.WriteByte('\n')
	for _, line := range tail(f.Log, ddzMaxLogLines) {
		b.WriteString(ddzLogStyle.Render("log: " + line))
		b.WriteByte('\n')
	}
	b.WriteString(inputStyle.Render("$ " + m.input + "_"))
	return b.String()
}

func ddzMetaLine(f DdzFrame) string {
	rounds := "?"
	if f.Rounds > 0 {
		rounds = fmt.Sprintf("%d", f.Rounds)
	}
	round := fmt.Sprintf("%d", f.Round)
	phase := string(f.Phase)
	if !f.Started {
		phase = "LOBBY"
		round = "-"
	}
	return fmt.Sprintf("room=%s  round=%s/%s  you=%s  base=%d  x=%d  phase=%s",
		f.Room, round, rounds, f.seatName(f.You), f.Base, f.Multiplier, phase)
}

func ddzSeatLine(f DdzFrame) string {
	parts := make([]string, 0, ddz.Seats)
	for seat := 0; seat < ddz.Seats; seat++ {
		info := f.Seats[seat]
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = "空位"
		}
		name = runewidth.Truncate(name, 10, "")
		tag := fmt.Sprintf("[%d]%s", seat, name)
		if seat == f.You {
			tag += "(你)"
		}
		if info.IsHost {
			tag += "^"
		}
		switch {
		case strings.TrimSpace(info.Name) == "":
			// 名字已经渲染成"空位"，这里不用再补状态，免得出现"空位 空位"
		case f.Started:
			tag += fmt.Sprintf(" %d张", info.HandCount)
		case info.Ready:
			tag += " 已就绪"
		default:
			tag += " 未就绪"
		}
		if seat == f.Landlord {
			tag += "[地主]"
		}
		// 只有"曾经有人、现在不在了"才值得标离线，空位不必
		if !info.Online && strings.TrimSpace(info.Name) != "" {
			tag += "(离线)"
		}
		if f.Started && seat == f.Turn && f.Phase != ddz.PhaseRoundEnd {
			tag = ddzTurnStyle.Render(tag + " *")
		} else {
			tag = metaStyle.Render(tag)
		}
		parts = append(parts, tag)
	}
	return "seats: " + strings.Join(parts, "  ")
}

// ddzTableBlock 渲染牌桌公共信息：底牌、叫分、上一手、不要标记。
func ddzTableBlock(f DdzFrame) string {
	var b strings.Builder

	if f.Landlord >= 0 && len(f.Bottom) > 0 {
		b.WriteString(headerStyle.Render("bottom: "))
		for i, c := range f.Bottom {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(ddzCardText(c))
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("   landlord=%s  bid=%d", f.seatName(f.Landlord), f.BidScore)))
		b.WriteByte('\n')
	}

	switch f.Phase {
	case ddz.PhaseBidding:
		b.WriteString(headerStyle.Render("bidding: "))
		parts := make([]string, 0, ddz.Seats)
		for seat := 0; seat < ddz.Seats; seat++ {
			mark := "-"
			switch {
			case f.Bids[seat] > 0:
				mark = fmt.Sprintf("%d分", f.Bids[seat])
			case f.BidActed[seat]:
				mark = "不叫"
			}
			parts = append(parts, fmt.Sprintf("%d:%s", seat, mark))
		}
		b.WriteString(metaStyle.Render(strings.Join(parts, "  ") + fmt.Sprintf("   最高 %d 分", f.BidScore)))
		b.WriteByte('\n')
	case ddz.PhasePlaying, ddz.PhaseRoundEnd:
		b.WriteString(headerStyle.Render("last: "))
		if f.LastPlay == nil {
			b.WriteString(metaStyle.Render("自由出牌"))
		} else {
			b.WriteString(metaStyle.Render(fmt.Sprintf("%s 打出 %s  ", f.seatName(f.LastPlay.Seat), f.LastPlay.Label)))
			for _, c := range f.LastPlay.Cards {
				b.WriteString(ddzCardText(c))
				b.WriteByte(' ')
			}
		}
		pass := make([]string, 0, ddz.Seats)
		for seat := 0; seat < ddz.Seats; seat++ {
			if f.PassSeat[seat] {
				pass = append(pass, fmt.Sprintf("%d:不要", seat))
			}
		}
		if len(pass) > 0 {
			b.WriteString(headerStyle.Render("  pass: " + strings.Join(pass, " ")))
		}
		b.WriteByte('\n')
	}
	if f.LastPlay == nil && f.Phase == ddz.PhasePlaying {
		b.WriteString(headerStyle.Render("tip: 当前是自由出牌权，必须出牌（不能 pass）"))
		b.WriteByte('\n')
	}
	return b.String()
}

func ddzHandBlock(f DdzFrame) string {
	var b strings.Builder
	if !f.Started {
		b.WriteString(metaStyle.Render("hand: 等待开局"))
		b.WriteByte('\n')
		return b.String()
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf("hand(%d):", len(f.Hand))))
	b.WriteByte('\n')
	for i, c := range f.Hand {
		col := i % ddzCardsPerRow
		if col == 0 {
			b.WriteString("  ")
		}
		b.WriteString(ddzCardCell(i+1, c))
		if col == ddzCardsPerRow-1 || i == len(f.Hand)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func ddzRevealBlock(f DdzFrame) string {
	var b strings.Builder
	result := "本局结束"
	if f.Winner >= 0 {
		side := "农民"
		if f.LandlordWin {
			side = "地主"
		}
		result = fmt.Sprintf("%s获胜（座位 %d 出完）", side, f.Winner)
	}
	b.WriteString(headerStyle.Render("---- 本局结算 ----  "))
	b.WriteString(statusStyle.Render(result))
	b.WriteByte('\n')
	for seat := 0; seat < ddz.Seats; seat++ {
		cards := f.Reveal[seat]
		b.WriteString(metaStyle.Render(fmt.Sprintf("  %d %s 剩 %d 张: ", seat, f.seatName(seat), len(cards))))
		if len(cards) == 0 {
			b.WriteString(metaStyle.Render("（出完）"))
		}
		for _, c := range cards {
			b.WriteString(ddzCardText(c))
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
	}
	parts := make([]string, 0, ddz.Seats)
	for seat := 0; seat < ddz.Seats; seat++ {
		parts = append(parts, fmt.Sprintf("%d:%+d(总%+d)", seat, f.RoundScore[seat], f.Scores[seat]))
	}
	style := statusStyle
	if f.RoundScore[f.You] < 0 {
		style = ddzWarnStyle
	}
	b.WriteString(style.Render("  得分变化 " + strings.Join(parts, "  ")))
	b.WriteByte('\n')
	return b.String()
}

func ddzStatusText(f DdzFrame) string {
	switch {
	case !f.Started:
		return "大厅：三人到齐后由房主输入 start"
	case f.Phase == ddz.PhaseBidding:
		if f.Turn == f.You {
			return "轮到你叫分（bid 1-3，或 bid 0 不叫）"
		}
		return fmt.Sprintf("等待 %s 叫分", f.seatName(f.Turn))
	case f.Phase == ddz.PhasePlaying:
		if f.Turn == f.You {
			if f.LastPlay == nil {
				return "自由出牌，必须出牌"
			}
			return "轮到你，需要压过 " + f.LastPlay.Label
		}
		return fmt.Sprintf("等待 %s 出牌", f.seatName(f.Turn))
	case f.Phase == ddz.PhaseRoundEnd:
		return "本局结束（等你或房主输入 next 开下一局）"
	}
	return ""
}

// ddzCardText 渲染一张牌，红/黑/王用不同颜色。
func ddzCardText(c ddz.Card) string {
	text := c.Label()
	switch {
	case c.Rank == ddz.RankJokerSmall || c.Rank == ddz.RankJokerBig:
		return ddzJokerStyle.Render(text)
	case c.Suit == ddz.SuitHeart || c.Suit == ddz.SuitDiamond:
		return ddzRedStyle.Render(text)
	default:
		return ddzBlackStyle.Render(text)
	}
}

// ddzCardCell 渲染带编号的手牌格子：编号 5 列（含 #）+ 牌面 + 补白 = 固定 9 列。
// 编号带 # 前缀是为了和"按牌面出牌"的写法区分开：play #3 是编号，play 55 是牌面。
func ddzCardCell(num int, c ddz.Card) string {
	prefix := "     "
	if num > 0 {
		prefix = padLeft(fmt.Sprintf("#%d:", num), 5)
	}
	label := c.Label()
	pad := 9 - runewidth.StringWidth(prefix) - runewidth.StringWidth(label)
	if pad < 1 {
		pad = 1
	}
	return headerStyle.Render(prefix) + ddzCardText(c) + strings.Repeat(" ", pad)
}

func (f DdzFrame) seatName(seat int) string {
	if seat < 0 || seat >= ddz.Seats {
		return "系统"
	}
	name := strings.TrimSpace(f.Seats[seat].Name)
	if name == "" {
		return fmt.Sprintf("座位%d", seat)
	}
	return runewidth.Truncate(name, 10, "")
}

func tail(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
