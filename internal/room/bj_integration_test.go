package room

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"intranet-play/internal/bj"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// --- 驱动用的假界面（房主侧） ---

// bjBotUI 既当玩家又当"主持人"：人齐了负责 start，结算后负责 next。
// wantSeats / maxRounds 让测试可控：人不够不开局，打够轮数就停手，
// 否则机器人会以毫秒级速度连打几千轮。
type bjBotUI struct {
	seat      int
	wantSeats int
	maxRounds int

	cmds chan string
	done chan struct{}
	once sync.Once

	mu          sync.Mutex
	lastKey     string
	latest      ui.BjFrame
	lines       []string
	settled     int
	lastSeen    int
	startedOnce bool
}

func newBjBotUI(seat, wantSeats, maxRounds int) *bjBotUI {
	return &bjBotUI{
		seat:      seat,
		wantSeats: wantSeats,
		maxRounds: maxRounds,
		cmds:      make(chan string, 64),
		done:      make(chan struct{}),
	}
}

func (b *bjBotUI) Run() error              { return nil }
func (b *bjBotUI) Done() <-chan struct{}   { return b.done }
func (b *bjBotUI) Commands() <-chan string { return b.cmds }
func (b *bjBotUI) Quit()                   { b.once.Do(func() { close(b.done) }) }

func (b *bjBotUI) UpdateFrame(f ui.BjFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latest = f
	if f.Started && f.Phase == bj.PhaseSettle && f.Round != b.lastSeen {
		b.lastSeen = f.Round
		b.settled++
	}

	var line string
	switch {
	case !f.Started:
		// 人齐了才开局，而且只开一次（掉线作废之后不要又自动重开）
		if !b.startedOnce && len(f.Seats) >= b.wantSeats && allBjSeatsNamed(f) {
			b.startedOnce = true
			line = "start"
		}
	case f.Over:
		// 整桌结束了，等测试断言，不再自动 next
	case f.Phase == bj.PhaseSettle && b.settled < b.maxRounds:
		line = "next"
	default:
		line = bjDecide(b.seat, f)
	}
	if line == "" {
		return
	}
	key := bjFrameKey(b.seat, f) + "|" + line
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

func (b *bjBotUI) snapshot() (ui.BjFrame, []string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latest, append([]string(nil), b.lines...), b.settled
}

// --- 共用决策 ---

func bjSeatAt(f ui.BjFrame, seat int) *ui.BjSeatInfo {
	if seat < 0 || seat >= len(f.Seats) {
		return nil
	}
	return &f.Seats[seat]
}

func allBjSeatsNamed(f ui.BjFrame) bool {
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

// bjFrameKey 概括值得做决定的那些状态，避免同一状态重复下令。
// 关键：只看"我自己的可观察状态 + 回合 + 阶段"，不看别人的下注——
// 否则别人一下注就会让我重新决策，重复发一次已经发过的动作。
func bjFrameKey(seat int, f ui.BjFrame) string {
	named := 0
	for _, s := range f.Seats {
		if strings.TrimSpace(s.Name) != "" {
			named++
		}
	}
	mine := "nil"
	if s := bjSeatAt(f, seat); s != nil {
		mine = fmt.Sprintf("%s/%d/%d/%d", s.Status, s.Score, len(s.Cards), s.Chips)
	}
	dealer := fmt.Sprintf("%d/%d/%v", f.Dealer.Score, f.Dealer.Hidden, f.Dealer.Final)
	return fmt.Sprintf("%v|%s|%d|%d|%d|%d|%s|%s",
		f.Started, f.Phase, f.Round, f.Turn, len(f.Seats), named, mine, dealer)
}

// bjDecide 只负责自己的下注与行动：17 点以下继续要牌，够 17 就停。
// 坐庄和出局的座位什么都不用做。
func bjDecide(seat int, f ui.BjFrame) string {
	s := bjSeatAt(f, seat)
	if s == nil {
		return ""
	}
	switch s.Status {
	case bj.StatusDealer, bj.StatusOut:
		return ""
	}
	switch f.Phase {
	case bj.PhaseBetting:
		if s.Status == bj.StatusIdle {
			amount := 5
			if s.MaxBet < amount {
				amount = s.MaxBet
			}
			if s.Chips < amount {
				amount = s.Chips
			}
			if amount <= 0 {
				return ""
			}
			return fmt.Sprintf("bet %d", amount)
		}
	case bj.PhasePlaying:
		if f.Turn != seat {
			return ""
		}
		if s.Score < 17 {
			return "hit"
		}
		return "stand"
	}
	return ""
}

// bjFrameOf 用公共视图拼出界面帧，供裸协议客户端复用同一套决策逻辑。
func bjFrameOf(view bj.PublicView, seats []protocol.BjSeatPayload, you int) ui.BjFrame {
	f := ui.BjFrame{
		You:     you,
		Started: view.Phase != "",
		Phase:   view.Phase,
		Turn:    view.Turn,
		Round:   view.Round,
		Dealer: ui.BjDealerInfo{
			Up:     view.Dealer.Up,
			Hidden: view.Dealer.Hidden,
			Score:  view.Dealer.Score,
			Soft:   view.Dealer.Soft,
			Final:  view.Dealer.Final,
		},
		Results: view.Results,
	}
	for i := range seats {
		info := ui.BjSeatInfo{Name: seats[i].Name, Online: seats[i].Online, IsHost: seats[i].IsHost}
		if i < len(view.Seats) {
			vs := view.Seats[i]
			info.Bet, info.Status = vs.Bet, vs.Status
			info.Cards, info.Score, info.Soft, info.Chips = vs.Cards, vs.Score, vs.Soft, vs.Chips
			info.MaxBet = vs.MaxBet // 漏了这行机器人就会以为"最多只能押 0"，干看着不下注
		}
		f.Seats = append(f.Seats, info)
	}
	return f
}

// countCardIDsIn 数报文里出现了多少张牌。
func countCardIDsIn(raw json.RawMessage) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return -1
	}
	n := 0
	var walk func(any)
	walk = func(node any) {
		switch t := node.(type) {
		case map[string]any:
			for k, child := range t {
				if k == "id" {
					n++
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return n
}

// --- 裸协议客户端 ---

type bjWireBot struct {
	name      string
	maxRounds int

	mu         sync.Mutex
	seat       int
	seats      []protocol.BjSeatPayload
	problems   []string
	sumDelta   map[int]int
	dealerGain map[int]int
	chips      map[int]int
	lastSeen   int
	actions    int
	key        string
}

func (b *bjWireBot) problemf(format string, args ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.problems = append(b.problems, fmt.Sprintf(format, args...))
}

func (b *bjWireBot) report() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.problems...)
}

func (b *bjWireBot) run(conn *netx.Conn) {
	join, err := protocol.NewEnvelope(protocol.TypeJoin, b.name, protocol.JoinPayload{Name: b.name})
	if err == nil {
		_ = conn.Send(join)
	}
	for env := range conn.Inbox() {
		switch env.Type {
		case protocol.TypeBjLobby:
			var p protocol.BjLobbyPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			b.mu.Lock()
			b.seat, b.seats = p.You, p.Seats
			b.mu.Unlock()

		case protocol.TypeBjPublic:
			var p protocol.BjPublicPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			b.audit(env.Payload, p)
			b.mu.Lock()
			b.seats = p.Seats
			done := b.lastSeen >= b.maxRounds
			b.mu.Unlock()
			if !done {
				b.act(conn, p)
			}

		case protocol.TypeError:
			var p protocol.ErrorPayload
			if json.Unmarshal(env.Payload, &p) == nil {
				b.problemf("收到错误: %s", p.Message)
			}
		}
	}
}

// audit 是隐私红线：公共报文里出现的牌数必须等于可见牌数，多出来就是暗牌泄漏。
func (b *bjWireBot) audit(raw json.RawMessage, p protocol.BjPublicPayload) {
	visible := len(p.View.Dealer.Up)
	for _, s := range p.View.Seats {
		visible += len(s.Cards)
	}
	if got := countCardIDsIn(raw); got != visible {
		b.problemf("公共报文里有 %d 张牌，可见牌只有 %d 张（疑似暗牌泄漏）", got, visible)
	}
	if p.View.Phase != bj.PhaseSettle && p.View.Dealer.Final {
		b.problemf("结算前庄家不该是已翻开状态")
	}
	if p.View.Phase == bj.PhasePlaying && p.View.Dealer.Hidden != 1 {
		b.problemf("行动阶段暗牌张数 = %d，期望 1", p.View.Dealer.Hidden)
	}

	// 记录每轮结算，用于最后校验筹码
	if p.View.Phase == bj.PhaseSettle && len(p.View.Results) > 0 {
		b.mu.Lock()
		if b.lastSeen != p.View.Round {
			b.lastSeen = p.View.Round
			if b.sumDelta == nil {
				b.sumDelta = map[int]int{}
				b.dealerGain = map[int]int{}
				b.chips = map[int]int{}
			}
			for _, r := range p.View.Results {
				b.sumDelta[r.Seat] += r.Delta
			}
			// 轮庄时庄家自己没下注，他的变化走 DealerDelta
			if p.View.Dealer.IsSeat {
				b.dealerGain[p.View.Dealer.Seat] += p.View.Dealer.Delta
			}
			for _, sv := range p.View.Seats {
				b.chips[sv.Seat] = sv.Chips
			}
		}
		b.mu.Unlock()
	}
}

func (b *bjWireBot) act(conn *netx.Conn, p protocol.BjPublicPayload) {
	b.mu.Lock()
	seat, seats := b.seat, b.seats
	prevKey := b.key
	b.mu.Unlock()

	f := bjFrameOf(p.View, seats, seat)
	line := bjDecide(seat, f)
	if line == "" {
		return
	}
	key := bjFrameKey(seat, f) + "|" + line
	if key == prevKey {
		return
	}
	act, err := bjActionFromCmd(parseBjCmd(line))
	if err != nil {
		b.problemf("动作 %q 无法转换: %v", line, err)
		return
	}
	env, err := protocol.NewEnvelope(protocol.TypeBjAction, b.name, protocol.BjActionPayload{Action: act})
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

// TestBjMultiPlayerEndToEnd 起一个房主房间 + 两个真实 TCP 客户端，连打两轮并逐条审计报文。
func TestBjMultiPlayerEndToEnd(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newBjHostRoom("9988", "Host", 100, 4, "auto")
	hostBot := newBjBotUI(0, 3, 2) // 三人齐了才开局，打完两轮就停
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	var bots []*bjWireBot
	for _, name := range []string{"Alice", "Cara"} {
		conn, err := netx.Connect(addr)
		if err != nil {
			t.Fatalf("客户端 %s 连接失败: %v", name, err)
		}
		defer conn.Close()
		bot := &bjWireBot{name: name, maxRounds: 2}
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
		return fmt.Sprintf("两轮没有打完（已结算 %d 轮）\n房主帧: phase=%s turn=%d round=%d seats=%d\n房主动作: %s\n客户端问题: %v",
			settled, frame.Phase, frame.Turn, frame.Round, len(frame.Seats), tailLines(lines, 12), parts)
	})

	for _, b := range bots {
		if problems := b.report(); len(problems) > 0 {
			t.Fatalf("客户端 %s 的报文审计失败（%d 条）:\n%s", b.name, len(problems), tailLines(problems, 5))
		}
	}

	// 每个客户端都必须真的下过注、做过决定，且筹码能对上账
	startChips := 100
	seats := 3
	for _, b := range bots {
		b.mu.Lock()
		actions, seat, name := b.actions, b.seat, b.name
		sumDelta, dealerGain, chips := b.sumDelta, b.dealerGain, b.chips
		b.mu.Unlock()
		if actions == 0 {
			t.Fatalf("客户端 %s 一次都没行动过", name)
		}
		if seat != 1 && seat != 2 {
			t.Fatalf("客户端 %s 的座位 = %d, 期望 1 或 2", name, seat)
		}
		total := 0
		for s := 0; s < seats; s++ {
			want := startChips + sumDelta[s] + dealerGain[s]
			if chips[s] != want {
				t.Fatalf("座位 %d 两轮后筹码 = %d, 期望 %d（闲家净输赢 %d + 坐庄收付 %d）",
					s, chips[s], want, sumDelta[s], dealerGain[s])
			}
			total += chips[s]
		}
		// 轮庄模式下筹码只在玩家之间流动，总额必须守恒
		if want := startChips * seats; total != want {
			t.Fatalf("轮庄筹码应当守恒：总额 %d, 期望 %d", total, want)
		}
	}
}

// TestBjRoomFull 验证超过人数上限的连接被明确拒绝。
func TestBjRoomFull(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newBjHostRoom("9988", "Host", 100, 2, "virtual") // 房主 + 1 个客户端就满了
	hostBot := newBjBotUI(0, 99, 0)                          // 这个测试不需要机器人开局
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
	waitBjLobbyOrFail(t, first)

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

// TestBjDisconnectAbortsGame 验证对局中有人断线时本局作废回大厅。
func TestBjDisconnectAbortsGame(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newBjHostRoom("9988", "Host", 100, 3, "virtual")
	hostBot := newBjBotUI(0, 2, 0) // 两人齐了就开局，之后不主动 next
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
	waitBjLobbyOrFail(t, conn)

	waitForf(t, 5*time.Second, func() bool {
		f, _, _ := hostBot.snapshot()
		return f.Started && len(f.Seats) == 2
	}, func() string {
		f, lines, _ := hostBot.snapshot()
		return fmt.Sprintf("两人对局没有开起来: started=%v seats=%d\n动作: %s", f.Started, len(f.Seats), tailLines(lines, 8))
	})

	_ = conn.Close()

	waitForf(t, 5*time.Second, func() bool {
		f, _, _ := hostBot.snapshot()
		return strings.Contains(strings.Join(f.Log, "\n"), "作废")
	}, func() string {
		f, lines, _ := hostBot.snapshot()
		return fmt.Sprintf("掉线后没有作废本局: started=%v\n日志: %v\n动作: %s", f.Started, f.Log, tailLines(lines, 8))
	})
}

func waitBjLobbyOrFail(t *testing.T, conn *netx.Conn) protocol.BjLobbyPayload {
	t.Helper()
	p, err := waitBjLobby(conn)
	if err != nil {
		t.Fatalf("等待大厅失败: %v", err)
	}
	return p
}

// waitForf 与 ddz 测试里的 waitFor 同形，只是失败信息用闭包惰性拼，
// 这样超时时才能把现场状态带出来。
func waitForf(t *testing.T, timeout time.Duration, cond func() bool, msg func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg())
}

// tailLines 只保留最近 n 条，免得失败信息刷出成千上万行。
func tailLines(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
		return fmt.Sprintf("…(只显示最后 %d 条) %v", n, lines)
	}
	return fmt.Sprintf("%v", lines)
}
