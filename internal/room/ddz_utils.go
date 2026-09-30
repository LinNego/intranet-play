package room

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"intranet-play/internal/ddz"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

const ddzMaxLog = 40

// ddzSeat 是房间里的一个座位。房主进程的座位没有 conn（自己就是服务端）。
type ddzSeat struct {
	name   string
	conn   *netx.Conn
	joined bool
	online bool
	ready  bool
	isHost bool
}

// ddzUI 是房间层对界面层的最小依赖。抽出接口是为了能在没有终端的环境里
// 跑三人完整对局的集成测试（测试用假界面脚本化地驱动房间）。
type ddzUI interface {
	Run() error
	Done() <-chan struct{}
	Commands() <-chan string
	UpdateFrame(ui.DdzFrame)
	Quit()
}

// ddzRoom 同时服务房主与客户端：
// 房主持有 game（唯一权威状态），客户端只持有收到的视图，永远不本地推演。
type ddzRoom struct {
	name       string
	you        int
	base       int
	rounds     int
	seats      [ddz.Seats]*ddzSeat
	game       *ddz.Game // 仅房主进程非空
	view       ddz.PublicView
	seatInfos  [ddz.Seats]ui.DdzSeatInfo
	hand       []ddz.Card
	reveal     [ddz.Seats][]ddz.Card
	revealSent bool
	started    bool
	// pendingKind 记住最近一次上行命令的类型，被房主拒绝时好补一句用法提示。
	pendingKind string
	log         []string
	app         ddzUI
	rng         *rand.Rand
}

func newDdzRoom(name string, base, rounds int) *ddzRoom {
	if base <= 0 {
		base = 1
	}
	return &ddzRoom{name: name, base: base, rounds: rounds, view: emptyView()}
}

// newDdzHostRoom 建好房主座位（座位 0，没有 conn，自己就是服务端）。
// room 是房间标识（用端口号），hostName 是房主这个玩家的名字，两者不能混。
func newDdzHostRoom(room, hostName string, base, rounds int) *ddzRoom {
	r := newDdzRoom(room, base, rounds)
	r.you = 0
	r.seats[0] = &ddzSeat{name: hostName, online: true, ready: true, joined: true, isHost: true}
	r.refreshSeatInfo()
	return r
}

// emptyView 是"还没开局"的公共状态。
// 注意 Landlord / Winner 必须显式设成 -1：零值 0 会被界面当成"座位 0 是地主"。
func emptyView() ddz.PublicView {
	return ddz.PublicView{Landlord: -1, Winner: -1}
}

func (r *ddzRoom) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	r.log = append(r.log, line)
	if len(r.log) > ddzMaxLog {
		r.log = r.log[len(r.log)-ddzMaxLog:]
	}
}

func (r *ddzRoom) nameOf(seat int) string {
	if seat == ddz.SeatSystem {
		return ""
	}
	if seat < 0 || seat >= ddz.Seats {
		return "系统"
	}
	if s := r.seats[seat]; s != nil && s.name != "" {
		return s.name
	}
	return fmt.Sprintf("座位%d", seat)
}

func (r *ddzRoom) refreshSeatInfo() {
	for i := 0; i < ddz.Seats; i++ {
		info := ui.DdzSeatInfo{HandCount: r.view.HandCount[i]}
		if s := r.seats[i]; s != nil {
			info.Name = s.name
			info.Online = s.online
			info.Ready = s.ready
			info.IsHost = s.isHost
		}
		r.seatInfos[i] = info
	}
}

func (r *ddzRoom) frame() ui.DdzFrame {
	return ui.DdzFrame{
		Room:        r.name,
		You:         r.you,
		Round:       r.view.Round,
		Rounds:      r.rounds,
		Base:        r.base,
		Started:     r.started,
		Phase:       r.view.Phase,
		Turn:        r.view.Turn,
		Landlord:    r.view.Landlord,
		BidScore:    r.view.BidScore,
		Multiplier:  r.view.Multiplier,
		Bids:        r.view.Bids,
		BidActed:    r.view.BidActed,
		Bottom:      r.view.Bottom,
		Seats:       r.seatInfos,
		LastPlay:    r.view.LastPlay,
		PassSeat:    r.view.PassSeat,
		Scores:      r.view.Scores,
		RoundScore:  r.view.RoundScore,
		Hand:        r.hand,
		Reveal:      r.reveal,
		Winner:      r.view.Winner,
		LandlordWin: r.view.LandlordWin,
		Log:         append([]string(nil), r.log...),
	}
}

func (r *ddzRoom) render() {
	r.refreshSeatInfo()
	if r.app == nil {
		return
	}
	r.app.UpdateFrame(r.frame())
}

func (r *ddzRoom) revealReset() {
	r.reveal = [ddz.Seats][]ddz.Card{}
	r.revealSent = false
}

func (r *ddzRoom) seatPayloads() []protocol.DdzSeatPayload {
	out := make([]protocol.DdzSeatPayload, 0, ddz.Seats)
	for seat := 0; seat < ddz.Seats; seat++ {
		sp := protocol.DdzSeatPayload{Seat: seat}
		if s := r.seats[seat]; s != nil {
			sp.Name, sp.Ready, sp.Online, sp.IsHost = s.name, s.ready, s.online, s.isHost
		}
		out = append(out, sp)
	}
	return out
}

func (r *ddzRoom) allJoined() bool {
	for i := 0; i < ddz.Seats; i++ {
		s := r.seats[i]
		if s == nil || !s.joined || !s.online {
			return false
		}
	}
	return true
}

func (r *ddzRoom) missingCount() int {
	n := 0
	for i := 0; i < ddz.Seats; i++ {
		s := r.seats[i]
		if s == nil || !s.joined || !s.online {
			n++
		}
	}
	return n
}

// freeSeat 只把 1/2 号座位分给连进来的客户端，0 号永远留给房主进程。
func (r *ddzRoom) freeSeat() int {
	for i := 1; i < ddz.Seats; i++ {
		s := r.seats[i]
		if s == nil || (!s.online && s.conn == nil) {
			return i
		}
	}
	return -1
}

func (r *ddzRoom) unicast(seat int, env protocol.Envelope) {
	s := r.seats[seat]
	if s == nil || s.conn == nil || !s.online {
		return
	}
	_ = s.conn.Send(env)
}

func (r *ddzRoom) broadcast(env protocol.Envelope) {
	for seat := 0; seat < ddz.Seats; seat++ {
		r.unicast(seat, env)
	}
}

func (r *ddzRoom) sendErr(s *ddzSeat, msg string) {
	if s == nil || s.conn == nil {
		return
	}
	env, err := protocol.NewEnvelope(protocol.TypeError, "host", protocol.ErrorPayload{Message: msg})
	if err == nil {
		_ = s.conn.Send(env)
	}
}

func (r *ddzRoom) broadcastEvent(e ddz.Event) {
	env, err := protocol.NewEnvelope(protocol.TypeDdzEvent, "host", protocol.DdzEventPayload{Seat: e.Seat, Text: e.Text})
	if err == nil {
		r.broadcast(env)
	}
}

// pushAll 是对外状态的唯一出口：不开始时发大厅，开局后广播公共状态 + 单播各家手牌。
func (r *ddzRoom) pushAll() {
	if !r.started || r.game == nil {
		r.view = emptyView()
		r.hand = nil
		r.revealReset()
		r.pushLobby()
		r.render()
		return
	}
	r.view = r.game.PublicView()
	r.hand = r.game.PrivateView(r.you).Hand
	r.broadcastState()
	r.render()
}

func (r *ddzRoom) pushLobby() {
	for seat := 0; seat < ddz.Seats; seat++ {
		s := r.seats[seat]
		if s == nil || s.conn == nil {
			continue
		}
		env, err := protocol.NewEnvelope(protocol.TypeDdzLobby, "host", protocol.DdzLobbyPayload{
			You: seat, Seats: r.seatPayloads(), Base: r.base, Rounds: r.rounds,
		})
		if err == nil {
			_ = s.conn.Send(env)
		}
	}
}

// broadcastState 广播公共视图（只有张数），并把手牌单播给本人。
func (r *ddzRoom) broadcastState() {
	env, err := protocol.NewEnvelope(protocol.TypeDdzPublic, "host", protocol.DdzPublicPayload{
		View: r.view, Seats: r.seatPayloads(), Rounds: r.rounds,
	})
	if err != nil {
		return
	}
	r.broadcast(env)

	for seat := 0; seat < ddz.Seats; seat++ {
		s := r.seats[seat]
		if s == nil || s.conn == nil {
			continue
		}
		handEnv, err := protocol.NewEnvelope(protocol.TypeDdzHand, "host", protocol.DdzHandPayload{
			View: r.game.PrivateView(seat),
		})
		if err == nil {
			_ = s.conn.Send(handEnv)
		}
	}

	if r.view.Phase == ddz.PhaseRoundEnd && !r.revealSent {
		r.revealSent = true
		endEnv, err := protocol.NewEnvelope(protocol.TypeDdzRoundEnd, "host", protocol.DdzRoundEndPayload{
			View: r.view, Reveal: r.game.Reveal(),
		})
		if err == nil {
			r.broadcast(endEnv)
		}
	}
}

func (r *ddzRoom) relayChat(from int, msg string) {
	env, err := protocol.NewEnvelope(protocol.TypeChat, r.nameOf(from), protocol.ChatPayload{Message: msg})
	if err != nil {
		return
	}
	for seat := 0; seat < ddz.Seats; seat++ {
		if seat == from {
			continue
		}
		r.unicast(seat, env)
	}
}

// --- 提示与报错 ---

// maxHintShown 一条提示里最多列几种出法，超出的只报数量。
const maxHintShown = 6

// showHints 用"自己的手牌 + 公开的上一手牌"算出所有能压过去的出法并写进日志。
// 房主和客户端都在本地算：客户端手里只有自己的牌，算出来的东西不涉及别人的隐私，
// 也不需要多一次网络往返。给出的写法可以直接复制成下一条命令。
func (r *ddzRoom) showHints() {
	if !r.started || len(r.hand) == 0 {
		r.logf("提示: 对局还没开始")
		r.render()
		return
	}
	var target *ddz.Combo
	if last := r.view.LastPlay; last != nil {
		combo := last.AsCombo()
		target = &combo
	}

	hints := ddz.Hints(r.hand, target)
	if len(hints) == 0 {
		if target == nil {
			r.logf("提示: 手里没有能出的牌")
		} else {
			r.logf("提示: 没有能压过%s的牌，只能 pass", r.view.LastPlay.Label)
		}
		r.render()
		return
	}

	parts := make([]string, 0, maxHintShown+1)
	for i, h := range hints {
		if i == maxHintShown {
			parts = append(parts, fmt.Sprintf("…还有 %d 种", len(hints)-maxHintShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s play %s", h.Combo, h.Notation()))
	}
	if target == nil {
		r.logf("可以出（后面的写法直接可用）: %s", strings.Join(parts, " | "))
	} else {
		r.logf("可以压过%s: %s", r.view.LastPlay.Label, strings.Join(parts, " | "))
	}
	r.render()
}

// rejectHint 统一处理被拒绝的操作：出牌失败时顺带提醒两套写法，免得卡在编号/点数上。
func (r *ddzRoom) rejectHint(err error, cmd ddzCmd) {
	msg := err.Error()
	if cmd.kind == "play" {
		msg += "（写法: play 55 / play 34567 / play #3 #7）"
	}
	r.logf("操作被拒: %s", msg)
	r.render()
}

// --- 命令解析 ---

type ddzCmd struct {
	kind  string     // play / bid / pass / start / next / ready / chat / hint / leave / help / bad / none
	nums  []int      // 手牌编号（play #3 #7 形式）
	ranks []ddz.Rank // 牌面点数（play 55 / play 34567 形式）
	text  string
}

func parseDdzCmd(line string) ddzCmd {
	line = strings.TrimSpace(line)
	if line == "" {
		return ddzCmd{kind: "none"}
	}
	lower := strings.ToLower(line)
	switch {
	case lower == "start" || line == "开始":
		return ddzCmd{kind: "start"}
	case lower == "next" || line == "下一局":
		return ddzCmd{kind: "next"}
	case lower == "ready" || line == "准备":
		return ddzCmd{kind: "ready"}
	case lower == "pass" || line == "不要" || line == "过":
		return ddzCmd{kind: "pass"}
	case lower == "hint" || lower == "提示" || lower == "有什":
		return ddzCmd{kind: "hint"}
	case lower == "help" || line == "帮助" || line == "?":
		return ddzCmd{kind: "help"}
	case lower == "leave" || lower == "quit" || lower == "exit":
		return ddzCmd{kind: "leave"}
	}

	fields := strings.Fields(line)
	head := strings.ToLower(fields[0])
	rest := strings.TrimSpace(line[len(fields[0]):])

	switch head {
	case "play", "出", "出牌":
		return parsePlayArgs(fields[1:])
	case "bid", "叫", "叫分":
		if len(fields) != 2 {
			return ddzCmd{kind: "bad", text: "用法: bid 0 / bid 1 / bid 2 / bid 3"}
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n < 0 || n > 3 {
			return ddzCmd{kind: "bad", text: "叫分只能是 0-3（0 表示不叫）"}
		}
		return ddzCmd{kind: "bid", nums: []int{n}}
	case "chat", "say", "说":
		if strings.TrimSpace(rest) == "" {
			return ddzCmd{kind: "bad", text: "用法: chat TEXT"}
		}
		return ddzCmd{kind: "chat", text: rest}
	}
	return ddzCmd{kind: "bad", text: "无法识别的命令，输入 help 查看用法"}
}

// parsePlayArgs 解析出牌参数。两套写法：
//
//	带 # 前缀 → 手牌编号：play #3 #7 / play #3-#7
//	否则      → 牌面点数：play 55 / play 34567 / play wW / play T J Q K A
//
// 两种写法不能混用，免得"3 到底是编号还是点数"说不清。
func parsePlayArgs(args []string) ddzCmd {
	if len(args) == 0 {
		return ddzCmd{kind: "bad", text: playUsage}
	}
	useIndex := false
	for _, a := range args {
		if strings.HasPrefix(a, "#") {
			useIndex = true
			break
		}
	}
	if useIndex {
		nums, err := parseIndexList(args)
		if err != nil {
			return ddzCmd{kind: "bad", text: err.Error()}
		}
		return ddzCmd{kind: "play", nums: nums}
	}
	ranks, err := ddz.ParseRanks(strings.Join(args, " "))
	if err != nil {
		return ddzCmd{kind: "bad", text: err.Error()}
	}
	return ddzCmd{kind: "play", ranks: ranks}
}

const playUsage = "用法: play 55（按牌面，如 34567 / wW / T J Q K A）或 play #3 #7（按手牌编号）"

// parseIndexList 只接受带 # 的编号写法："#3 #7" 与 "#3-#7"。
func parseIndexList(fields []string) ([]int, error) {
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		if !strings.HasPrefix(f, "#") {
			return nil, fmt.Errorf("%q 缺少 # 前缀；编号写法要写成 play #3 #7，牌面写法直接写点数如 play 55", f)
		}
		body := strings.TrimPrefix(f, "#")
		parts := strings.Split(body, "-")
		if len(parts) == 1 {
			n, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("编号 %q 不是数字", f)
			}
			nums = append(nums, n)
			continue
		}
		if len(parts) != 2 {
			return nil, fmt.Errorf("区间 %q 格式不对", f)
		}
		// 区间两端都允许再写一次 #，即 "#1-#3" 与 "#1-3" 等价
		lo, err1 := strconv.Atoi(strings.TrimPrefix(parts[0], "#"))
		hi, err2 := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
		if err1 != nil || err2 != nil || hi < lo {
			return nil, fmt.Errorf("区间 %q 格式不对", f)
		}
		if hi-lo > 30 {
			return nil, fmt.Errorf("区间 %q 太大", f)
		}
		for n := lo; n <= hi; n++ {
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		return nil, fmt.Errorf("%s", playUsage)
	}
	return nums, nil
}

// rankText 描述整场排名。两个农民会同时赢分，所以并列是常态，必须如实说明。
func rankText(scores []int) string {
	if len(scores) == 0 {
		return "无有效积分"
	}
	best := scores[0]
	for _, s := range scores {
		if s > best {
			best = s
		}
	}
	leaders := make([]string, 0, len(scores))
	for i, s := range scores {
		if s == best {
			leaders = append(leaders, fmt.Sprintf("座位%d", i))
		}
	}
	if len(leaders) > 1 {
		return fmt.Sprintf("%s 并列第一（各 %d 分）", strings.Join(leaders, "、"), best)
	}
	return fmt.Sprintf("%s 第一（%d 分）", leaders[0], best)
}

// actionFromCmd 把手牌编号或牌面点数翻译成 cardID 动作。
// 两套写法都以最近一次下发的手牌为准。
func actionFromCmd(cmd ddzCmd, hand []ddz.Card) (ddz.Action, error) {
	switch cmd.kind {
	case "pass":
		return ddz.Action{Kind: ddz.ActPass}, nil
	case "bid":
		if len(cmd.nums) != 1 {
			return ddz.Action{}, fmt.Errorf("用法: bid 0-3")
		}
		return ddz.Action{Kind: ddz.ActBid, Bid: cmd.nums[0]}, nil
	case "play":
		if len(cmd.ranks) > 0 {
			ids, err := handRanksToIDs(hand, cmd.ranks)
			if err != nil {
				return ddz.Action{}, err
			}
			return ddz.Action{Kind: ddz.ActPlay, Cards: ids}, nil
		}
		ids, err := handIndicesToIDs(hand, cmd.nums)
		if err != nil {
			return ddz.Action{}, err
		}
		return ddz.Action{Kind: ddz.ActPlay, Cards: ids}, nil
	}
	return ddz.Action{}, fmt.Errorf("无法识别的动作")
}

// handRanksToIDs 按点数从手里挑牌。斗地主不看花色，同点数的牌挑哪几张都一样。
func handRanksToIDs(hand []ddz.Card, ranks []ddz.Rank) ([]int, error) {
	if len(hand) == 0 {
		return nil, fmt.Errorf("手牌为空")
	}
	need := make(map[ddz.Rank]int, len(ranks))
	for _, r := range ranks {
		need[r]++
	}
	used := make(map[int]bool, len(ranks))
	ids := make([]int, 0, len(ranks))
	for _, r := range ranks {
		picked := false
		for _, c := range hand {
			if c.Rank == r && !used[c.ID] {
				used[c.ID] = true
				ids = append(ids, c.ID)
				picked = true
				break
			}
		}
		if !picked {
			have := 0
			for _, c := range hand {
				if c.Rank == r {
					have++
				}
			}
			return nil, fmt.Errorf("你手里只有 %d 张 %s，出不了 %d 张", have, r.Label(), need[r])
		}
	}
	return ids, nil
}

func handIndicesToIDs(hand []ddz.Card, nums []int) ([]int, error) {
	if len(hand) == 0 {
		return nil, fmt.Errorf("手牌为空")
	}
	ids := make([]int, 0, len(nums))
	seen := make(map[int]bool, len(nums))
	for _, n := range nums {
		if n < 1 || n > len(hand) {
			return nil, fmt.Errorf("手牌编号 %d 超出范围 1-%d", n, len(hand))
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		ids = append(ids, hand[n-1].ID)
	}
	return ids, nil
}
