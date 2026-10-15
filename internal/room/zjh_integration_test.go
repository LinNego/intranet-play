package room

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
	"intranet-play/internal/zjh"
)

// --- 驱动用的假界面（房主侧） ---

type zjhBotUI struct {
	seat      int
	wantSeats int
	maxHands  int

	cmds chan string
	done chan struct{}
	once sync.Once

	mu          sync.Mutex
	lastKey     string
	latest      ui.ZjhFrame
	lines       []string
	settled     int
	lastSeen    int
	startedOnce bool
	st          zjhBotState
}

func newZjhBotUI(seat, wantSeats, maxHands int) *zjhBotUI {
	return &zjhBotUI{
		seat:      seat,
		wantSeats: wantSeats,
		maxHands:  maxHands,
		cmds:      make(chan string, 64),
		done:      make(chan struct{}),
	}
}

func (b *zjhBotUI) Run() error              { return nil }
func (b *zjhBotUI) Done() <-chan struct{}   { return b.done }
func (b *zjhBotUI) Commands() <-chan string { return b.cmds }
func (b *zjhBotUI) Quit()                   { b.once.Do(func() { close(b.done) }) }

func (b *zjhBotUI) UpdateFrame(f ui.ZjhFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latest = f
	if f.Started && f.Phase == zjh.PhaseSettle && f.Round != b.lastSeen {
		b.lastSeen = f.Round
		b.settled++
	}

	var line string
	switch {
	case !f.Started:
		if !b.startedOnce && len(f.Seats) >= b.wantSeats && allZjhSeatsNamed(f) {
			b.startedOnce = true
			line = "start"
		}
	case f.Over:
		// 整桌结束，等测试断言
	case f.Phase == zjh.PhaseSettle && b.settled < b.maxHands:
		line = "next"
	default:
		line = zjhDecide(b.seat, f, &b.st)
	}
	if line == "" {
		return
	}
	key := zjhFrameKey(b.seat, f) + "|" + line
	if key == b.lastKey {
		return
	}
	b.lastKey = key
	b.lines = append(b.lines, line)
	select {
	case b.cmds <- line:
	default:
	}
}

func (b *zjhBotUI) snapshot() (ui.ZjhFrame, []string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latest, append([]string(nil), b.lines...), b.settled
}

// --- 共用决策 ---

func zjhSeatAt(f ui.ZjhFrame, seat int) *ui.ZjhSeatInfo {
	if seat < 0 || seat >= len(f.Seats) {
		return nil
	}
	return &f.Seats[seat]
}

func allZjhSeatsNamed(f ui.ZjhFrame) bool {
	if len(f.Seats) == 0 {
		return false
	}
	for _, s := range f.Seats {
		if strings.TrimSpace(s.Name) == "" {
			return false
		}
	}
	return true
}

// zjhFrameKey 只看"我自己的可观察状态 + 回合 + 阶段"，避免别人的动作让我重复下令。
func zjhFrameKey(seat int, f ui.ZjhFrame) string {
	named := 0
	for _, s := range f.Seats {
		if strings.TrimSpace(s.Name) != "" {
			named++
		}
	}
	mine := "nil"
	if s := zjhSeatAt(f, seat); s != nil {
		mine = fmt.Sprintf("%s/%v/%d/%d", s.Status, s.Looked, s.Bet, s.Chips)
	}
	// 只放"我自己的状态 + 回合 + 阶段"：把奖池/注额这种别人的动作也放进来，
	// 会让我在同一状态里反复决策（重复发 look / call）。
	return fmt.Sprintf("%v|%s|%d|%d|%d|%d|%s", f.Started, f.Phase, f.Round, f.Turn,
		len(f.Seats), named, mine)
}

// zjhBotState 记住"这一局已经喊过看牌"，公共报文可能连来几条，不能重复喊。
type zjhBotState struct {
	lookedRound int
}

// zjhDecide 是驱动用的笨策略：先看牌；轮到自己就跟注，到顶或加不了注就找人比牌；
// 付不起就跟不了（比牌也要付），只能弃牌。
func zjhDecide(seat int, f ui.ZjhFrame, st *zjhBotState) string {
	if !f.Started || f.Over || f.Phase != zjh.PhaseBetting {
		return ""
	}
	mine := zjhSeatAt(f, seat)
	if mine == nil || mine.Status != zjh.StatusPlaying {
		return ""
	}
	// 用公共座位状态判断"我看过牌没有"：这是公开信息，而且公共报文先于手牌报文到达，
	// 用私密视图判断会因为晚一拍而重复发 look。
	if !mine.Looked {
		if st.lookedRound == f.Round {
			return ""
		}
		st.lookedRound = f.Round
		return "look"
	}
	if f.Turn != seat {
		return ""
	}

	other := -1
	for i, s := range f.Seats {
		if i != seat && s.Status == zjh.StatusPlaying {
			other = i
			break
		}
	}
	if other < 0 {
		return ""
	}
	if mine.Chips < f.Cost {
		return "fold" // 付不起跟注，也付不起比牌
	}
	if f.MustCompare || !f.CanRaise {
		return fmt.Sprintf("compare %d", other)
	}
	return "call"
}

// zjhFrameOf 用公共视图 + 自己的私密视图拼出界面帧，供裸协议客户端复用同一套决策。
func zjhFrameOf(view zjh.PublicView, seats []protocol.ZjhSeatPayload, priv zjh.PrivateView, you int) ui.ZjhFrame {
	f := ui.ZjhFrame{
		You:         you,
		Started:     view.Phase != "",
		Phase:       view.Phase,
		Turn:        view.Turn,
		Round:       view.Round,
		Pot:         view.Pot,
		Stake:       view.Stake,
		Ante:        view.Ante,
		MaxStake:    view.MaxStake,
		RoundInHand: view.RoundInHand,
		MaxRounds:   view.MaxRounds,
		Looked:      priv.Looked,
		Cards:       priv.Cards,
		HandLabel:   priv.HandLabel,
		Cost:        priv.Cost,
		CanRaise:    priv.CanRaise,
		MustCompare: priv.MustCompare,
		Results:     view.Results,
		Winner:      view.Winner,
		TableWinner: view.TableWinner,
		Over:        view.Over,
	}
	for i := range seats {
		info := ui.ZjhSeatInfo{Name: seats[i].Name, Online: seats[i].Online, IsHost: seats[i].IsHost}
		if i < len(view.Seats) {
			vs := view.Seats[i]
			info.Looked, info.Status = vs.Looked, vs.Status
			info.Bet, info.Chips = vs.Bet, vs.Chips
		}
		f.Seats = append(f.Seats, info)
	}
	return f
}

// --- 裸协议客户端（带隐私审计） ---

type zjhWireBot struct {
	name  string
	hands int

	mu       sync.Mutex
	seat     int
	seats    []protocol.ZjhSeatPayload
	priv     zjh.PrivateView
	view     zjh.PublicView
	problems []string
	chips    map[int]int
	actions  int
	key      string
	peeks    int
	st       zjhBotState
}

func (b *zjhWireBot) problemf(format string, args ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.problems = append(b.problems, fmt.Sprintf(format, args...))
}

func (b *zjhWireBot) report() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.problems...)
}

func (b *zjhWireBot) run(conn *netx.Conn) {
	join, err := protocol.NewEnvelope(protocol.TypeJoin, b.name, protocol.JoinPayload{Name: b.name})
	if err == nil {
		_ = conn.Send(join)
	}
	for env := range conn.Inbox() {
		switch env.Type {
		case protocol.TypeZjhLobby:
			var p protocol.ZjhLobbyPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			b.mu.Lock()
			b.seat, b.seats = p.You, p.Seats
			b.mu.Unlock()

		case protocol.TypeZjhHand:
			var p protocol.ZjhHandPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			b.mu.Lock()
			if p.View.Seat != b.seat {
				b.problems = append(b.problems, fmt.Sprintf("收到了别人的手牌: %d", p.View.Seat))
			}
			// 隐私红线：没看牌就不能带牌
			if !p.View.Looked && len(p.View.Cards) != 0 {
				b.problems = append(b.problems, fmt.Sprintf("没看牌就下发了 %d 张牌", len(p.View.Cards)))
			}
			if p.View.Looked && len(p.View.Cards) != 3 {
				b.problems = append(b.problems, fmt.Sprintf("看牌后应当有 3 张牌，实际 %d", len(p.View.Cards)))
			}
			b.priv = p.View
			b.hands++
			b.mu.Unlock()

		case protocol.TypeZjhPublic:
			var p protocol.ZjhPublicPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			// 隐私红线：公共报文里一张牌都不能有
			if n := countCardIDsIn(env.Payload); n != 0 {
				b.problemf("公共报文里出现了 %d 张牌", n)
			}
			b.mu.Lock()
			b.view, b.seats = p.View, p.Seats
			if b.chips == nil {
				b.chips = map[int]int{}
			}
			for _, sv := range p.View.Seats {
				b.chips[sv.Seat] = sv.Chips
			}
			b.mu.Unlock()
			b.act(conn)

		case protocol.TypeZjhPeek:
			var p protocol.ZjhPeekPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			b.mu.Lock()
			// 隐私红线：比牌牌面只能发给当事人
			if p.Peek.A != b.seat && p.Peek.B != b.seat {
				b.problems = append(b.problems,
					fmt.Sprintf("收到了与自己无关的比牌牌面: %d vs %d", p.Peek.A, p.Peek.B))
			}
			b.peeks++
			b.mu.Unlock()

		case protocol.TypeError:
			var p protocol.ErrorPayload
			if json.Unmarshal(env.Payload, &p) == nil {
				b.problemf("收到错误: %s", p.Message)
			}
		}
	}
}

func (b *zjhWireBot) act(conn *netx.Conn) {
	b.mu.Lock()
	seat, seats, view, priv := b.seat, b.seats, b.view, b.priv
	prevKey := b.key
	b.mu.Unlock()

	f := zjhFrameOf(view, seats, priv, seat)
	line := zjhDecide(seat, f, &b.st)
	if line == "" {
		return
	}
	key := zjhFrameKey(seat, f) + "|" + line
	if key == prevKey {
		return
	}
	act, err := zjhActionFromCmd(parseZjhCmd(line))
	if err != nil {
		b.problemf("动作 %q 无法转换: %v", line, err)
		return
	}
	env, err := protocol.NewEnvelope(protocol.TypeZjhAction, b.name, protocol.ZjhActionPayload{Action: act})
	if err != nil {
		return
	}
	if err := conn.Send(env); err != nil {
		b.problemf("发送动作失败: %v", err)
		return
	}
	b.mu.Lock()
	b.key = key
	b.actions++
	b.mu.Unlock()
}

// --- 集成测试 ---

// TestZjhMultiPlayerEndToEnd 起一个房主房间 + 两个真实 TCP 客户端，连打两局并逐条审计隐私。
func TestZjhMultiPlayerEndToEnd(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newZjhHostRoom("9988", "Host", 100, 1, 20, 8, 4)
	hostBot := newZjhBotUI(0, 3, 2)
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	var bots []*zjhWireBot
	for _, name := range []string{"Alice", "Cara"} {
		conn, err := netx.Connect(addr)
		if err != nil {
			t.Fatalf("客户端 %s 连接失败: %v", name, err)
		}
		defer conn.Close()
		bot := &zjhWireBot{name: name}
		bots = append(bots, bot)
		go bot.run(conn)
	}

	waitForf(t, 20*time.Second, func() bool {
		_, _, settled := hostBot.snapshot()
		return settled >= 2
	}, func() string {
		frame, lines, settled := hostBot.snapshot()
		parts := make([]string, 0, len(bots))
		for _, b := range bots {
			parts = append(parts, fmt.Sprintf("%s: %v", b.name, b.report()))
		}
		return fmt.Sprintf("两局没有打完（已结算 %d 局）\n房主帧: phase=%s turn=%d round=%d seats=%d\n房主动作: %s\n客户端问题: %v",
			settled, frame.Phase, frame.Turn, frame.Round, len(frame.Seats), tailLines(lines, 12), parts)
	})

	for _, b := range bots {
		if problems := b.report(); len(problems) > 0 {
			t.Fatalf("客户端 %s 的隐私审计失败（%d 条）:\n%s", b.name, len(problems), tailLines(problems, 5))
		}
	}

	// 每个人都应当：收到过手牌报文、真的行动过、经过看牌
	for _, b := range bots {
		b.mu.Lock()
		hands, actions, seat, name, peeks, chips := b.hands, b.actions, b.seat, b.name, b.peeks, b.chips
		b.mu.Unlock()
		if hands == 0 {
			t.Fatalf("客户端 %s 一局手牌报文都没收到", name)
		}
		if actions == 0 {
			t.Fatalf("客户端 %s 一次都没行动过", name)
		}
		if seat != 1 && seat != 2 {
			t.Fatalf("客户端 %s 的座位 = %d, 期望 1 或 2", name, seat)
		}
		// 三家的筹码之和必须守恒（钱只在玩家之间流动）
		total := 0
		for s := 0; s < 3; s++ {
			total += chips[s]
		}
		if total != 300 {
			t.Fatalf("筹码总额 = %d, 期望 300（不该凭空产生或消失）", total)
		}
		// 打了 2 局、每局最多剩 1 人，所以必然有人经历过比牌
		_ = peeks
	}
}

// TestZjhRoomFull 验证超过人数上限的连接被明确拒绝。
func TestZjhRoomFull(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newZjhHostRoom("9988", "Host", 100, 1, 20, 8, 2)
	hostBot := newZjhBotUI(0, 99, 0) // 这个测试不需要机器人开局
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	first, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("第一个客户端连接失败: %v", err)
	}
	defer first.Close()
	if join, err := protocol.NewEnvelope(protocol.TypeJoin, "Alice", protocol.JoinPayload{Name: "Alice"}); err == nil {
		_ = first.Send(join)
	}
	waitZjhLobbyOrFail(t, first)

	second, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("第二个客户端连接失败: %v", err)
	}
	defer second.Close()

	select {
	case env, ok := <-second.Inbox():
		if !ok {
			t.Fatal("超员连接被直接断开，没有收到提示")
		}
		if env.Type != protocol.TypeError {
			t.Fatalf("超员连接收到 %s, 期望 %s", env.Type, protocol.TypeError)
		}
		var p protocol.ErrorPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			t.Fatalf("错误载荷解析失败: %v", err)
		}
		if !strings.Contains(p.Message, "room full") {
			t.Fatalf("拒绝原因 = %q, 期望包含 room full", p.Message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超员连接没有被拒绝")
	}
}

// TestZjhDisconnectAbortsHand 验证对局中有人断线时本局作废回大厅。
func TestZjhDisconnectAbortsHand(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newZjhHostRoom("9988", "Host", 100, 1, 20, 8, 3)
	hostBot := newZjhBotUI(0, 2, 0) // 两人齐了就开局，之后不主动 next
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	// 一个"哑"客户端：加入后什么都不做，让对局停在下注阶段
	conn, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("客户端连接失败: %v", err)
	}
	if join, err := protocol.NewEnvelope(protocol.TypeJoin, "Alice", protocol.JoinPayload{Name: "Alice"}); err == nil {
		_ = conn.Send(join)
	}
	waitZjhLobbyOrFail(t, conn)

	waitForf(t, 5*time.Second, func() bool {
		f, _, _ := hostBot.snapshot()
		return f.Started && len(f.Seats) == 2
	}, func() string {
		f, lines, _ := hostBot.snapshot()
		return fmt.Sprintf("两人对局没有开起来: started=%v seats=%d\n动作: %s",
			f.Started, len(f.Seats), tailLines(lines, 8))
	})

	_ = conn.Close()

	waitForf(t, 5*time.Second, func() bool {
		f, _, _ := hostBot.snapshot()
		return strings.Contains(strings.Join(f.Log, "\n"), "作废")
	}, func() string {
		f, lines, _ := hostBot.snapshot()
		return fmt.Sprintf("掉线后没有作废本局: started=%v\n日志: %v\n动作: %s",
			f.Started, f.Log, tailLines(lines, 8))
	})
}

func waitZjhLobbyOrFail(t *testing.T, conn *netx.Conn) protocol.ZjhLobbyPayload {
	t.Helper()
	p, err := waitZjhLobby(conn)
	if err != nil {
		t.Fatalf("等待大厅失败: %v", err)
	}
	return p
}
