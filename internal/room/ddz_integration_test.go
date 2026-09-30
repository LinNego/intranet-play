package room

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"intranet-play/internal/ddz"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

// botUI 是测试用的假界面：不发终端，只根据每一帧自动决定下一步动作。
type botUI struct {
	seat int
	cmds chan string
	done chan struct{}
	once sync.Once

	mu      sync.Mutex
	lastKey string
	frames  int
	latest  ui.DdzFrame
	lines   []string
}

func newBotUI(seat int) *botUI {
	return &botUI{seat: seat, cmds: make(chan string, 64), done: make(chan struct{})}
}

func (b *botUI) Run() error              { return nil }
func (b *botUI) Done() <-chan struct{}   { return b.done }
func (b *botUI) Commands() <-chan string { return b.cmds }
func (b *botUI) Quit()                   { b.once.Do(func() { close(b.done) }) }

func (b *botUI) UpdateFrame(f ui.DdzFrame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames++
	b.latest = f

	key := frameKey(f)
	if key == b.lastKey {
		return
	}
	line := decideAction(b.seat, f)
	if line == "" {
		return
	}
	b.lastKey = key
	b.lines = append(b.lines, line)
	select {
	case b.cmds <- line:
	default:
	}
}

func (b *botUI) snapshot() (ui.DdzFrame, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latest, append([]string(nil), b.lines...)
}

// frameKey 概括"值得做决定的那些状态"，避免同一状态重复下令。
func frameKey(f ui.DdzFrame) string {
	named := 0
	for _, s := range f.Seats {
		if s.Name != "" {
			named++
		}
	}
	handTop := 0
	if len(f.Hand) > 0 {
		handTop = f.Hand[0].ID
	}
	last := "nil"
	if f.LastPlay != nil {
		last = fmt.Sprintf("%d/%s/%d/%d", f.LastPlay.Seat, f.LastPlay.Kind, f.LastPlay.Main, f.LastPlay.Size)
	}
	return fmt.Sprintf("%v|%s|%d|%d|%d|%d|%s", f.Started, f.Phase, f.Turn, len(f.Hand), handTop, named, last)
}

// decideAction 是三个座位共用的"只会出单张"的笨策略，足够把一局打完。
func decideAction(seat int, f ui.DdzFrame) string {
	if !f.Started {
		if seat == 0 && allSeatsNamed(f) {
			return "start"
		}
		return ""
	}
	switch f.Phase {
	case ddz.PhaseBidding:
		if seat == 1 {
			return "bid 1" // 让座位 1 当地主，客户端才有出牌机会
		}
		return "bid 0"
	case ddz.PhasePlaying:
		if len(f.Hand) == 0 {
			return ""
		}
		if f.LastPlay == nil {
			// 手牌降序，最后一张就是最小单张
			return fmt.Sprintf("play %d", len(f.Hand))
		}
		if f.LastPlay.Kind == ddz.KindSingle {
			for i := len(f.Hand) - 1; i >= 0; i-- {
				if f.Hand[i].Rank > f.LastPlay.Main {
					return fmt.Sprintf("play %d", i+1)
				}
			}
		}
		return "pass"
	case ddz.PhaseRoundEnd:
		return "next"
	}
	return ""
}

func allSeatsNamed(f ui.DdzFrame) bool {
	for _, s := range f.Seats {
		if s.Name == "" {
			return false
		}
	}
	return true
}

// startTestClient 用真实的 room 客户端路径接入（覆盖 ddz_client.go）。
func startTestClient(t *testing.T, addr, name string, bot *botUI) *netx.Conn {
	t.Helper()
	conn, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("客户端 %s 连接失败: %v", name, err)
	}
	join, err := protocol.NewEnvelope(protocol.TypeJoin, name, protocol.JoinPayload{Name: name})
	if err != nil {
		t.Fatalf("构造 JOIN 失败: %v", err)
	}
	if err := conn.Send(join); err != nil {
		t.Fatalf("发送 JOIN 失败: %v", err)
	}
	lobby, err := waitDdzLobby(conn)
	if err != nil {
		t.Fatalf("客户端 %s 等待大厅失败: %v", name, err)
	}

	r := newDdzRoom(addr, lobby.Base, lobby.Rounds)
	r.applyLobby(lobby)
	bot.seat = lobby.You
	r.app = bot
	go func() { _ = r.serveClient(conn, name) }()

	ready, err := protocol.NewEnvelope(protocol.TypeDdzReady, name, nil)
	if err == nil {
		_ = conn.Send(ready)
	}
	return conn
}

// auditor 是裸协议客户端：自己解析报文，一边陪玩一边审计报文里有没有越界的牌。
type auditor struct {
	seat int
	hand []ddz.Card
	view ddz.PublicView

	mu          sync.Mutex
	lastKey     string
	problems    []string
	hands       int
	actions     int
	reveal      [ddz.Seats][]ddz.Card
	rounds      int
	roundWinner int
	roundScore  [ddz.Seats]int
}

func (a *auditor) problemf(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.problems = append(a.problems, fmt.Sprintf(format, args...))
}

func (a *auditor) report() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.problems...)
}

func (a *auditor) run(conn *netx.Conn, name string, matchEnd chan<- protocol.DdzMatchEndPayload) {
	join, err := protocol.NewEnvelope(protocol.TypeJoin, name, protocol.JoinPayload{Name: name})
	if err == nil {
		_ = conn.Send(join)
	}
	for env := range conn.Inbox() {
		switch env.Type {
		case protocol.TypeDdzLobby:
			var p protocol.DdzLobbyPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.seat = p.You
			if ready, err := protocol.NewEnvelope(protocol.TypeDdzReady, name, nil); err == nil {
				_ = conn.Send(ready)
			}

		case protocol.TypeDdzPublic:
			var p protocol.DdzPublicPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			if p.View.Phase != ddz.PhaseRoundEnd {
				a.auditPublic(env.Payload, p.View)
			}
			a.view = p.View
			a.act(conn, name)

		case protocol.TypeDdzHand:
			var p protocol.DdzHandPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			if p.View.Seat != a.seat {
				a.problemf("收到了别人座位的手牌: 报文座位=%d 我的座位=%d", p.View.Seat, a.seat)
			}
			a.mu.Lock()
			a.hands++
			a.mu.Unlock()
			a.hand = p.View.Hand

		case protocol.TypeDdzRoundEnd:
			var p protocol.DdzRoundEndPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			a.mu.Lock()
			a.reveal = p.Reveal
			a.rounds++
			a.roundWinner = p.View.Winner
			a.roundScore = p.View.RoundScore
			a.mu.Unlock()

		case protocol.TypeDdzMatchEnd:
			var p protocol.DdzMatchEndPayload
			if json.Unmarshal(env.Payload, &p) == nil {
				select {
				case matchEnd <- p:
				default:
				}
			}
			return
		}
	}
}

// auditPublic 检查公共报文里出现的每张牌是否都在"公开可见"范围内：
// 自己的手牌、已翻开的底牌、以及刚打出的那手牌。其余一律算泄漏。
func (a *auditor) auditPublic(raw json.RawMessage, view ddz.PublicView) {
	allowed := make(map[int]bool)
	for _, c := range a.hand {
		allowed[c.ID] = true
	}
	for _, c := range view.Bottom {
		allowed[c.ID] = true
	}
	if view.LastPlay != nil {
		for _, c := range view.LastPlay.Cards {
			allowed[c.ID] = true
		}
	}
	for _, id := range collectCardIDs(raw) {
		if !allowed[id] {
			a.problemf("公共状态里出现了不该公开的牌 id=%d（阶段 %s）", id, view.Phase)
		}
	}
}

func (a *auditor) act(conn *netx.Conn, name string) {
	f := ui.DdzFrame{
		Started:  a.view.Phase != "",
		Phase:    a.view.Phase,
		Turn:     a.view.Turn,
		Hand:     a.hand,
		LastPlay: a.view.LastPlay,
	}
	key := frameKey(f)
	if key == a.lastKey {
		return
	}
	line := decideAction(a.seat, f)
	if line == "" {
		return
	}
	a.lastKey = key

	// "next" 不是对局动作，而是独立的开局请求消息。
	if line == "next" {
		env, err := protocol.NewEnvelope(protocol.TypeDdzNext, name, nil)
		if err == nil {
			_ = conn.Send(env)
		}
		return
	}

	act, err := actionFromCmd(parseDdzCmd(line), a.hand)
	if err != nil {
		a.problemf("审计端动作 %q 无法转换: %v", line, err)
		return
	}
	env, err := protocol.NewEnvelope(protocol.TypeDdzAction, name, protocol.DdzActionPayload{Action: act})
	if err != nil {
		return
	}
	if err := conn.Send(env); err != nil {
		a.problemf("审计端发送动作失败: %v", err)
		return
	}
	a.mu.Lock()
	a.actions++
	a.mu.Unlock()
}

// collectCardIDs 递归收集报文里所有 "id" 字段（Card 的编号）。
func collectCardIDs(raw json.RawMessage) []int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []int
	var walk func(node any)
	walk = func(node any) {
		switch t := node.(type) {
		case map[string]any:
			for k, child := range t {
				if k == "id" {
					if f, ok := child.(float64); ok {
						out = append(out, int(f))
					}
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
	return out
}

// TestDdzThreePlayerEndToEnd 起一个房主房间 + 两个真实 TCP 客户端，把一整场打完。
// 覆盖：大厅 → 叫分 → 地主底牌 → 三人轮转出牌 → 结算 → 整场排名，并审计报文隐私。
func TestDdzThreePlayerEndToEnd(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newDdzHostRoom("9988", "Host", 1, 1) // 只打 1 局，打完直接整场结算
	hostBot := newBotUI(0)
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	botA := newBotUI(0)
	connA := startTestClient(t, addr, "Alice", botA)
	defer connA.Close()
	defer botA.Quit()

	connB, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("审计端连接失败: %v", err)
	}
	defer connB.Close()
	aud := &auditor{}
	matchEnd := make(chan protocol.DdzMatchEndPayload, 1)
	go aud.run(connB, "Cara", matchEnd)

	var result protocol.DdzMatchEndPayload
	select {
	case result = <-matchEnd:
	case <-time.After(30 * time.Second):
		frame, lines := hostBot.snapshot()
		t.Fatalf("对局没有在 30 秒内结束\n房主帧: phase=%s turn=%d hand=%d\n房主动作: %v\n审计端问题: %v",
			frame.Phase, frame.Turn, len(frame.Hand), lines, aud.report())
	}

	if problems := aud.report(); len(problems) > 0 {
		t.Fatalf("报文隐私/一致性检查失败:\n%s", joinLines(problems))
	}
	if aud.seat != 2 {
		t.Fatalf("第二个连入的客户端应当拿到座位 2，实际 %d", aud.seat)
	}
	if aud.hands == 0 {
		t.Fatal("审计端一局都没收到手牌下发")
	}
	if aud.actions == 0 {
		t.Fatal("审计端一次都没能行动，出牌流程没跑起来")
	}
	if aud.rounds == 0 {
		t.Fatal("审计端没有收到本局结算（DDZ_ROUND_END）")
	}

	// 结算：三家积分之和为 0，赢家就是分最高的人
	sum := 0
	for _, s := range result.Scores {
		sum += s
	}
	if sum != 0 {
		t.Fatalf("三家积分之和 = %d, 期望 0（%v）", sum, result.Scores)
	}
	best := 0
	for i := range result.Scores {
		if result.Scores[i] > result.Scores[best] {
			best = i
		}
	}
	if result.Winner != best {
		t.Fatalf("整场赢家 = %d, 期望 %d（积分 %v）", result.Winner, best, result.Scores)
	}
	if result.Scores[result.Winner] <= 0 {
		t.Fatalf("整场赢家的积分应当为正数: %v", result.Scores)
	}

	// 揭牌：三家手牌互不重叠，且赢家手牌出完
	seen := make(map[int]bool)
	total := 0
	for seat := 0; seat < ddz.Seats; seat++ {
		for _, c := range aud.reveal[seat] {
			if seen[c.ID] {
				t.Fatalf("揭牌出现重复的牌 id=%d", c.ID)
			}
			seen[c.ID] = true
			total++
		}
	}
	if total == 0 {
		t.Fatal("结算没有揭出任何手牌")
	}
	// 本局赢家（真正把牌出完的那个人）手牌必须为空。
	// 注意：整场第一可能是并列的（两个农民同时赢分），所以这里必须看本局赢家而不是整场第一。
	if aud.roundWinner < 0 || aud.roundWinner >= ddz.Seats {
		t.Fatalf("本局赢家字段非法: %d", aud.roundWinner)
	}
	if got := len(aud.reveal[aud.roundWinner]); got != 0 {
		lengths := make([]int, ddz.Seats)
		for i := range aud.reveal {
			lengths[i] = len(aud.reveal[i])
		}
		t.Fatalf("本局赢家座位 %d 手里还剩 %d 张牌；各家剩牌 %v，本局得分 %v，整场积分 %v",
			aud.roundWinner, got, lengths, aud.roundScore, result.Scores)
	}
	if aud.roundScore[aud.roundWinner] <= 0 {
		t.Fatalf("本局赢家的得分应当为正数: %v", aud.roundScore)
	}
	if result.Scores[result.Winner] != result.Scores[best] {
		t.Fatalf("整场第一不是最高分: 座位 %d 得 %d 分, 最高 %d 分",
			result.Winner, result.Scores[result.Winner], result.Scores[best])
	}

	// 房主视角也应当收到过地主与倍数信息
	if _, lines := hostBot.snapshot(); len(lines) == 0 {
		t.Fatal("房主侧没有任何动作，房主没有被驱动起来")
	}
}

// TestDdzRenderBeforeRunDoesNotDeadlock 用**真实**的 ui.DdzApp 复现接线 bug：
// 房主如果在 app.Run() 之前渲染，bubbletea 的 Program.Send 会把主 goroutine 锁死
// （终端里表现为 "all goroutines are asleep - deadlock"，进程直接崩）。
// 端到端测试用的是假界面，抓不到这个问题，所以这里必须用真界面。
func TestDdzRenderBeforeRunDoesNotDeadlock(t *testing.T) {
	r := newDdzHostRoom("9988", "Host", 1, 1)
	r.app = ui.NewDdzApp(r.frame())
	r.logf("准备中")

	done := make(chan struct{})
	go func() {
		r.render()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 之前调用 render 阻塞了：在真实终端里会 deadlock")
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += "  - " + l
	}
	return out
}

// TestAuditorCatchesLeak 是审计逻辑的反向对照：故意塞一张不该公开的牌，
// 审计必须报警。没有这个对照，端到端测试里的"无泄漏"结论就是空的。
func TestAuditorCatchesLeak(t *testing.T) {
	aud := &auditor{}
	aud.hand = mustTestCards(t, "3", "3")
	own := aud.hand[0].ID

	// 报文里同时出现"自己的牌"（合法）和"别人的牌"（非法）
	raw := json.RawMessage(fmt.Sprintf(
		`{"view":{"handCount":[1,2,3]},"lastPlay":{"cards":[{"id":%d,"rank":3,"suit":0}]}}`, own+1))
	aud.auditPublic(raw, ddz.PublicView{Phase: ddz.PhasePlaying, LastPlay: nil})

	problems := aud.report()
	if len(problems) != 1 {
		t.Fatalf("期望恰好报出 1 条泄漏，实际 %d 条: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], fmt.Sprintf("id=%d", own+1)) {
		t.Fatalf("泄漏报告内容不对: %v", problems)
	}

	// 自己的手牌出现在公共报文里是正常的，不应报警
	aud2 := &auditor{}
	aud2.hand = mustTestCards(t, "K")
	raw2 := json.RawMessage(fmt.Sprintf(`{"bottom":[{"id":%d,"rank":13,"suit":0}]}`, aud2.hand[0].ID))
	aud2.auditPublic(raw2, ddz.PublicView{Phase: ddz.PhasePlaying})
	if got := aud2.report(); len(got) != 0 {
		t.Fatalf("底牌是公开信息，不该报警: %v", got)
	}
}

// TestDdzFourthPlayerRejected 验证第 4 个连接被明确拒绝。
func TestDdzFourthPlayerRejected(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newDdzHostRoom("9988", "Host", 1, 1)
	hostBot := newBotUI(0)
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	// 房主自己占座位 0，所以只有两个客户端座位
	for i := 0; i < 2; i++ {
		conn, err := netx.Connect(addr)
		if err != nil {
			t.Fatalf("第 %d 个客户端连接失败: %v", i+1, err)
		}
		defer conn.Close()
		if join, err := protocol.NewEnvelope(protocol.TypeJoin, fmt.Sprintf("P%d", i+1), protocol.JoinPayload{Name: fmt.Sprintf("P%d", i+1)}); err == nil {
			_ = conn.Send(join)
		}
		waitForLobby(t, conn)
	}

	third, err := netx.Connect(addr)
	if err != nil {
		t.Fatalf("第 3 个客户端连接失败: %v", err)
	}
	defer third.Close()

	select {
	case env, ok := <-third.Inbox():
		if !ok {
			t.Fatal("第 3 个客户端被直接断开，没有收到任何提示")
		}
		if env.Type != protocol.TypeError {
			t.Fatalf("第 3 个客户端收到 %s, 期望 %s", env.Type, protocol.TypeError)
		}
		var p protocol.ErrorPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			t.Fatalf("错误载荷解析失败: %v", err)
		}
		if !strings.Contains(p.Message, "room full") {
			t.Fatalf("拒绝原因 = %q, 期望包含 room full", p.Message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("第 3 个客户端没有被拒绝")
	}
}

// TestDdzDisconnectAbortsRound 验证 3 人局里有人掉线时本局作废回大厅。
func TestDdzDisconnectAbortsRound(t *testing.T) {
	ln, err := netx.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	host := newDdzHostRoom("9988", "Host", 1, 1)
	hostBot := newBotUI(0) // 房主会在三人到齐后自动 start
	host.app = hostBot
	go func() { _ = host.serveHost(ln) }()
	defer hostBot.Quit()

	// 两个"哑"客户端：只加入，不出牌，让对局停在叫分阶段
	var conns []*netx.Conn
	for i := 0; i < 2; i++ {
		conn, err := netx.Connect(addr)
		if err != nil {
			t.Fatalf("客户端连接失败: %v", err)
		}
		conns = append(conns, conn)
		name := fmt.Sprintf("P%d", i+1)
		if join, err := protocol.NewEnvelope(protocol.TypeJoin, name, protocol.JoinPayload{Name: name}); err == nil {
			_ = conn.Send(join)
		}
		waitForLobby(t, conn)
	}

	// 等对局真的开起来
	waitFor(t, 3*time.Second, func() bool {
		f, _ := hostBot.snapshot()
		return f.Started
	}, "对局没有进入开局状态")

	// 掉线：3 人局无法托管，应当作废本局
	_ = conns[0].Close()

	waitFor(t, 3*time.Second, func() bool {
		f, _ := hostBot.snapshot()
		return !f.Started && strings.Contains(strings.Join(f.Log, "\n"), "作废")
	}, "掉线后本局没有作废")

	_ = conns[1].Close()
}

func waitForLobby(t *testing.T, conn *netx.Conn) protocol.DdzLobbyPayload {
	t.Helper()
	p, err := waitDdzLobby(conn)
	if err != nil {
		t.Fatalf("等待大厅失败: %v", err)
	}
	return p
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

// mustTestCards 按点数标签取牌，供审计对照测试构造报文。
func mustTestCards(t *testing.T, labels ...string) []ddz.Card {
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
	return out
}
