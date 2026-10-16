package ui

import (
	"fmt"
	"strings"

	"intranet-play/internal/game"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// cellW=2 keeps the matrix compact but readable (glyph + gap).
const cellW = 2
const gutterW = 3 // row label + gap before matrix

// 全局样式：刻意压成"默认 / 暗灰 / 加粗"三档，没有任何彩色。
// 这是个摸鱼工具——屏幕上一眼看过去要像在跑工具、看日志，而不是在打牌。
var (
	titleStyle  = plainStyle
	metaStyle   = mutedStyle
	statusStyle = plainStyle
	msgStyle    = hintStyle
	helpStyle   = hintStyle
	inputStyle  = plainStyle
	headerStyle = faintStyle
	p1Style     = plainStyle
	p2Style     = mutedStyle
	winStyle    = boldStyle
	emptyStyle  = faintStyle
)

// Frame is one full UI snapshot.
type Frame struct {
	Board    *game.Board
	Status   game.GameStatus
	Turn     int
	You      int
	YouName  string
	Opponent string
	LastMsg  string
}

// FrameMsg pushes a new frame into the TUI.
type FrameMsg struct {
	Frame Frame
}

// App wraps a bubbletea program for host/client.
type App struct {
	program *tea.Program
	cmds    chan string
	done    chan struct{}
}

// NewApp creates the TUI. Call Run on the main goroutine.
func NewApp(initial Frame) *App {
	cmds := make(chan string, 16)
	m := model{
		frame: initial,
		cmds:  cmds,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	return &App{
		program: p,
		cmds:    cmds,
		done:    make(chan struct{}),
	}
}

func (a *App) Run() error {
	defer close(a.done)
	_, err := a.program.Run()
	return err
}

func (a *App) Done() <-chan struct{} { return a.done }

func (a *App) Commands() <-chan string { return a.cmds }

func (a *App) UpdateFrame(f Frame) {
	a.program.Send(FrameMsg{Frame: f})
}

func (a *App) Quit() {
	a.program.Quit()
}

type model struct {
	frame Frame
	input string
	cmds  chan string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case FrameMsg:
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

func (m model) View() string {
	var b strings.Builder
	f := m.frame

	localTag, peerTag := "t1", "t2"
	if f.You == 2 {
		localTag, peerTag = "t2", "t1"
	}
	youName, oppName := f.YouName, f.Opponent
	if youName == "" {
		youName = "-"
	}
	if oppName == "" {
		oppName = "-"
	}
	b.WriteString(LogoBlock(
		"nx-sync 0.1  |  peer matrix",
		fmt.Sprintf("local=%s/%s  peer=%s/%s  n=%d", youName, localTag, oppName, peerTag, boardSize(f.Board)),
		"五子棋 · 2 人 · 输入 x,y 落子",
	))
	b.WriteByte('\n')

	if f.Board == nil || f.Board.Size == 0 {
		b.WriteString(metaStyle.Render("buf=nil"))
		b.WriteByte('\n')
	} else {
		b.WriteString(renderBoard(f.Board))
	}

	b.WriteString(Bullet(statusStyle.Render(statusText(f))))
	b.WriteByte('\n')
	if msg := strings.TrimSpace(f.LastMsg); msg != "" {
		b.WriteString(Bullet(msgStyle.Render(msg)))
		b.WriteByte('\n')
	}

	b.WriteString(InputBox(m.input, 60))
	b.WriteByte('\n')
	b.WriteString(Footer("? cmd: x,y | chat TEXT | resign | restart | leave"))
	return b.String()
}

func boardSize(b *game.Board) int {
	if b == nil {
		return 0
	}
	return b.Size
}

func statusText(f Frame) string {
	switch f.Status {
	case game.GameStatusWin:
		if f.Board != nil && f.Board.Winner == f.You {
			return "state=DONE  result=local"
		}
		return "state=DONE  result=peer"
	case game.GameStatusDraw:
		return "state=DONE  result=tie"
	case game.GameStatusContinue:
		if f.Turn == f.You {
			return fmt.Sprintf("state=RUN  turn=local/%d", f.You)
		}
		return fmt.Sprintf("state=RUN  turn=peer/%d", f.Turn)
	case game.GameStatusPending:
		return "state=WAIT"
	default:
		return fmt.Sprintf("state=%s", f.Status)
	}
}

// renderBoard draws a compact hex-addressed matrix (looks like a dump, not a board game).
func renderBoard(board *game.Board) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(padRight("", gutterW)))
	for x := 0; x < board.Size; x++ {
		b.WriteString(headerStyle.Render(padRight(axisLabel(x), cellW)))
	}
	b.WriteByte('\n')

	for y := 0; y < board.Size; y++ {
		// padRight so label is left-aligned and trailing spaces separate it from cells
		b.WriteString(headerStyle.Render(padRight(axisLabel(y), gutterW)))
		for x := 0; x < board.Size; x++ {
			b.WriteString(formatCell(board.Cells[x][y]))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func axisLabel(i int) string {
	if i < 0 {
		return "?"
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	if i < 36 {
		return string(rune('a' + i - 10))
	}
	return "?"
}

func formatCell(v int) string {
	// Digits blend into a hex/matrix dump; trailing space keeps columns readable.
	switch v {
	case 1:
		return p1Style.Render(padRight("1", cellW))
	case 2:
		return p2Style.Render(padRight("2", cellW))
	case 3:
		return winStyle.Render(padRight("A", cellW))
	case 4:
		return winStyle.Render(padRight("B", cellW))
	default:
		return emptyStyle.Render(padRight("0", cellW))
	}
}

func padLeft(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return strings.Repeat(" ", width-w) + s
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}
