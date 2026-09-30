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
	log        []string
	app        ddzUI
	rng        *rand.Rand
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

// --- 命令解析 ---

type ddzCmd struct {
	kind string // play / bid / pass / start / next / ready / chat / leave / help / bad / none
	nums []int
	text string
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
		nums, err := parseIndexList(fields[1:])
		if err != nil {
			return ddzCmd{kind: "bad", text: err.Error()}
		}
		return ddzCmd{kind: "play", nums: nums}
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

// parseIndexList 支持 "1 2 5" 与 "1-3" 两种写法。
func parseIndexList(fields []string) ([]int, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("用法: play 1 2 5 或 play 1-3")
	}
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		parts := strings.Split(f, "-")
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
		lo, err1 := strconv.Atoi(parts[0])
		hi, err2 := strconv.Atoi(parts[1])
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

// actionFromCmd 把界面上的手牌编号翻译成 cardID 动作。编号以最近一次下发的手牌为准。
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
		ids, err := handIndicesToIDs(hand, cmd.nums)
		if err != nil {
			return ddz.Action{}, err
		}
		return ddz.Action{Kind: ddz.ActPlay, Cards: ids}, nil
	}
	return ddz.Action{}, fmt.Errorf("无法识别的动作")
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
