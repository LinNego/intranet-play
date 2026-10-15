package room

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"intranet-play/internal/bj"
	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
)

const bjMaxLog = 40

// bjUI 是房间层对界面层的最小依赖。抽出接口是为了能在没有终端的环境里
// 跑多人完整对局的集成测试（测试用假界面脚本化地驱动房间）。
type bjUI interface {
	Run() error
	Done() <-chan struct{}
	Commands() <-chan string
	UpdateFrame(ui.BjFrame)
	Quit()
}

// bjSeat 是房间里的一个座位。房主进程的座位没有 conn（自己就是服务端）。
type bjSeat struct {
	name   string
	conn   *netx.Conn
	joined bool
	online bool
	isHost bool
}

// bjRoom 同时服务房主与客户端：
// 房主持有 game（唯一权威状态），客户端只持有收到的视图，永远不本地推演。
//
// 座位用切片表示并且**始终保持连续**：有人掉线就把后面的座位往前压，
// 这样"房间座位号"和"对局座位号"永远一致，不需要额外的映射表。
type bjRoom struct {
	name        string
	you         int
	startChips  int
	maxSeats    int
	dealerFlag  string // auto / rotate / virtual
	seats       []*bjSeat
	game        *bj.Game
	view        bj.PublicView
	seatInfos   []ui.BjSeatInfo
	log         []string
	started     bool
	pendingKind string
	app         bjUI
	rng         *rand.Rand
}

func newBjRoom(name string, chips, maxSeats int, dealerFlag string) *bjRoom {
	if chips <= 0 {
		chips = 100
	}
	if maxSeats <= 0 {
		maxSeats = 4
	}
	if dealerFlag == "" {
		dealerFlag = dealerAuto
	}
	return &bjRoom{
		name:       name,
		startChips: chips,
		maxSeats:   maxSeats,
		dealerFlag: dealerFlag,
		view:       emptyBjView(),
	}
}

// newBjHostRoom 建好房主座位（座位 0）。room 是房间标识（用端口号），
// hostName 是房主这个玩家的名字，两者不能混。
func newBjHostRoom(room, hostName string, chips, maxSeats int, dealerFlag string) *bjRoom {
	r := newBjRoom(room, chips, maxSeats, dealerFlag)
	r.you = 0
	r.seats = append(r.seats, &bjSeat{name: hostName, online: true, joined: true, isHost: true})
	r.refreshSeatInfo()
	return r
}

// 庄家模式的三种取值与解析。
const (
	dealerAuto    = "auto"
	dealerRotate  = "rotate"
	dealerVirtual = "virtual"
)

// resolveDealerMode 把命令行开关解析成实际模式：auto 表示 2 人用虚拟庄家
// （否则每局都有一半人没牌可打），3 人以上轮流坐庄。
func resolveDealerMode(flag string, seats int) bj.DealerMode {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case dealerRotate, "轮庄":
		return bj.DealerRotate
	case dealerVirtual, "虚拟":
		return bj.DealerVirtual
	default:
		if seats >= 3 {
			return bj.DealerRotate
		}
		return bj.DealerVirtual
	}
}

// dealerModeText 是给玩家看的模式说明。
func dealerModeText(mode bj.DealerMode) string {
	if mode == bj.DealerRotate {
		return "轮流坐庄"
	}
	return "虚拟庄家"
}

// emptyBjView 是"还没开局"的公共状态。Turn 必须显式设成 -1，
// 否则零值 0 会让界面把座位 0 标成"当前行动方"。
func emptyBjView() bj.PublicView {
	return bj.PublicView{Turn: -1}
}

func (r *bjRoom) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	r.log = append(r.log, line)
	if len(r.log) > bjMaxLog {
		r.log = r.log[len(r.log)-bjMaxLog:]
	}
}

func (r *bjRoom) nameOf(seat int) string {
	if seat == bj.SeatSystem {
		return ""
	}
	if seat < 0 || seat >= len(r.seats) {
		return fmt.Sprintf("座位%d", seat)
	}
	if s := r.seats[seat]; s != nil && s.name != "" {
		return s.name
	}
	return fmt.Sprintf("座位%d", seat)
}

// seatOf 按连接反查座位号。用连接而不是把座位号钉在 goroutine 里，
// 这样座位压缩之后消息归属依然正确。
func (r *bjRoom) seatOf(conn *netx.Conn) int {
	for i, s := range r.seats {
		if s != nil && s.conn == conn {
			return i
		}
	}
	return -1
}

func (r *bjRoom) refreshSeatInfo() {
	infos := make([]ui.BjSeatInfo, 0, len(r.seats))
	for i := range r.seats {
		info := ui.BjSeatInfo{}
		if s := r.seats[i]; s != nil {
			info.Name, info.Online, info.IsHost = s.name, s.online, s.isHost
		}
		if i < len(r.view.Seats) {
			vs := r.view.Seats[i]
			info.Bet, info.Status = vs.Bet, vs.Status
			info.Cards, info.Score, info.Soft, info.Chips = vs.Cards, vs.Score, vs.Soft, vs.Chips
			info.MaxBet = vs.MaxBet
		}
		infos = append(infos, info)
	}
	r.seatInfos = infos
}

func (r *bjRoom) frame() ui.BjFrame {
	seats := make([]ui.BjSeatInfo, len(r.seatInfos))
	copy(seats, r.seatInfos)
	return ui.BjFrame{
		Room:    r.name,
		You:     r.you,
		Round:   r.view.Round,
		Started: r.started,
		Phase:   r.view.Phase,
		Mode:    r.view.Mode,
		Turn:    r.view.Turn,
		Seats:   seats,
		Dealer: ui.BjDealerInfo{
			Up:      r.view.Dealer.Up,
			Hidden:  r.view.Dealer.Hidden,
			Score:   r.view.Dealer.Score,
			Soft:    r.view.Dealer.Soft,
			Final:   r.view.Dealer.Final,
			IsSeat:  r.view.Dealer.IsSeat,
			Seat:    r.view.Dealer.Seat,
			Delta:   r.view.Dealer.Delta,
			NeedBet: r.view.Dealer.NeedBet,
		},
		Results: r.view.Results,
		Over:    r.view.Over,
		Winner:  r.view.Winner,
		Log:     append([]string(nil), r.log...),
	}
}

func (r *bjRoom) render() {
	r.refreshSeatInfo()
	if r.app == nil {
		return
	}
	r.app.UpdateFrame(r.frame())
}

func (r *bjRoom) seatPayloads() []protocol.BjSeatPayload {
	out := make([]protocol.BjSeatPayload, 0, len(r.seats))
	for i, s := range r.seats {
		sp := protocol.BjSeatPayload{Seat: i}
		if s != nil {
			sp.Name, sp.Online, sp.IsHost = s.name, s.online, s.isHost
		}
		out = append(out, sp)
	}
	return out
}

// joinedCount 返回已经报上名字的座位数（决定开局时有几个座位）。
func (r *bjRoom) joinedCount() int {
	n := 0
	for _, s := range r.seats {
		if s != nil && s.joined && s.online {
			n++
		}
	}
	return n
}

func (r *bjRoom) allJoined() bool {
	return r.joinedCount() == len(r.seats)
}

func (r *bjRoom) unicast(seat int, env protocol.Envelope) {
	if seat < 0 || seat >= len(r.seats) {
		return
	}
	s := r.seats[seat]
	if s == nil || s.conn == nil || !s.online {
		return
	}
	_ = s.conn.Send(env)
}

func (r *bjRoom) broadcast(env protocol.Envelope) {
	for seat := range r.seats {
		r.unicast(seat, env)
	}
}

func (r *bjRoom) sendErr(seat int, msg string) {
	env, err := protocol.NewEnvelope(protocol.TypeError, "host", protocol.ErrorPayload{Message: msg})
	if err == nil {
		r.unicast(seat, env)
	}
}

func (r *bjRoom) broadcastEvent(e bj.Event) {
	env, err := protocol.NewEnvelope(protocol.TypeBjEvent, "host", protocol.BjEventPayload{Seat: e.Seat, Text: e.Text})
	if err == nil {
		r.broadcast(env)
	}
}

// afterEvents 把引擎事件写进面板日志并广播出去。
func (r *bjRoom) afterEvents(events []bj.Event) {
	for _, e := range events {
		if e.Seat == bj.SeatSystem {
			r.logf("%s", e.Text)
		} else {
			r.logf("%s %s", r.nameOf(e.Seat), e.Text)
		}
		r.broadcastEvent(e)
	}
}

// pushAll 是对外状态的唯一出口。
func (r *bjRoom) pushAll() {
	if !r.started || r.game == nil {
		r.view = emptyBjView()
		r.pushLobby()
		r.render()
		return
	}
	r.view = r.game.PublicView()
	r.broadcastState()
	r.render()
}

func (r *bjRoom) pushLobby() {
	for seat := range r.seats {
		s := r.seats[seat]
		if s == nil || s.conn == nil {
			continue
		}
		env, err := protocol.NewEnvelope(protocol.TypeBjLobby, "host", protocol.BjLobbyPayload{
			You:    seat,
			Seats:  r.seatPayloads(),
			Max:    r.maxSeats,
			Chips:  r.startChips,
			Dealer: r.dealerFlag,
		})
		if err == nil {
			_ = s.conn.Send(env)
		}
	}
}

func (r *bjRoom) gameSeatCount() int {
	if r.game == nil {
		return 0
	}
	return len(r.game.Hands)
}

// broadcastState 广播公共牌桌状态。21 点没有私密手牌，所以只有广播，没有单播。
func (r *bjRoom) broadcastState() {
	env, err := protocol.NewEnvelope(protocol.TypeBjPublic, "host", protocol.BjPublicPayload{
		View:  r.view,
		Seats: r.seatPayloads(),
		Max:   r.maxSeats,
	})
	if err == nil {
		r.broadcast(env)
	}
}

func (r *bjRoom) relayChat(from int, msg string) {
	env, err := protocol.NewEnvelope(protocol.TypeChat, r.nameOf(from), protocol.ChatPayload{Message: msg})
	if err != nil {
		return
	}
	for seat := range r.seats {
		if seat == from {
			continue
		}
		r.unicast(seat, env)
	}
}

func (r *bjRoom) rejectHint(err error, cmd bjCmd) {
	msg := err.Error()
	if cmd.kind == "bet" {
		msg += "（用法: bet 10）"
	}
	r.logf("操作被拒: %s", msg)
	r.render()
}

// --- 命令解析 ---

type bjCmd struct {
	kind   string // bet / hit / stand / double / next / start / restart / chat / help / leave / bad / none
	amount int
	text   string
}

func parseBjCmd(line string) bjCmd {
	line = strings.TrimSpace(line)
	if line == "" {
		return bjCmd{kind: "none"}
	}
	lower := strings.ToLower(line)
	switch {
	case lower == "start" || line == "开始":
		return bjCmd{kind: "start"}
	case lower == "restart" || line == "重开":
		return bjCmd{kind: "restart"}
	case lower == "next" || line == "下一轮":
		return bjCmd{kind: "next"}
	case lower == "hit" || line == "要" || line == "要牌":
		return bjCmd{kind: "hit"}
	case lower == "stand" || line == "停" || line == "停牌":
		return bjCmd{kind: "stand"}
	case lower == "double" || line == "双倍":
		return bjCmd{kind: "double"}
	case lower == "help" || line == "帮助" || line == "?":
		return bjCmd{kind: "help"}
	case lower == "leave" || lower == "quit" || lower == "exit":
		return bjCmd{kind: "leave"}
	}

	fields := strings.Fields(line)
	head := strings.ToLower(fields[0])
	rest := strings.TrimSpace(line[len(fields[0]):])

	switch head {
	case "bet", "押", "下注":
		if len(fields) != 2 {
			return bjCmd{kind: "bad", text: "用法: bet 10"}
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n <= 0 {
			return bjCmd{kind: "bad", text: "注额要写正整数，例如 bet 10"}
		}
		return bjCmd{kind: "bet", amount: n}
	case "chat", "say", "说":
		if strings.TrimSpace(rest) == "" {
			return bjCmd{kind: "bad", text: "用法: chat TEXT"}
		}
		return bjCmd{kind: "chat", text: rest}
	}
	return bjCmd{kind: "bad", text: "无法识别的命令，输入 help 查看用法"}
}

const bjHelpText = "命令: bet 10（下注）| hit 要牌 | stand 停牌 | double 双倍 | next 下一轮 | start 开局 | restart 重开 | chat TEXT | leave"

func bjActionFromCmd(cmd bjCmd) (bj.Action, error) {
	switch cmd.kind {
	case "bet":
		return bj.Action{Kind: bj.ActBet, Amount: cmd.amount}, nil
	case "hit":
		return bj.Action{Kind: bj.ActHit}, nil
	case "stand":
		return bj.Action{Kind: bj.ActStand}, nil
	case "double":
		return bj.Action{Kind: bj.ActDouble}, nil
	}
	return bj.Action{}, fmt.Errorf("无法识别的动作")
}
