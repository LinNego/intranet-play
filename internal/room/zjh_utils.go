package room

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"intranet-play/internal/netx"
	"intranet-play/internal/protocol"
	"intranet-play/internal/ui"
	"intranet-play/internal/zjh"
)

const zjhMaxLog = 40

// zjhUI 是房间层对界面层的最小依赖（与 ddzUI / bjUI 同形，便于无终端环境下做集成测试）。
type zjhUI interface {
	Run() error
	Done() <-chan struct{}
	Commands() <-chan string
	UpdateFrame(ui.ZjhFrame)
	Quit()
}

// zjhSeat 是房间里的一个座位。房主进程的座位没有 conn（自己就是服务端）。
type zjhSeat struct {
	name   string
	conn   *netx.Conn
	joined bool
	online bool
	isHost bool
}

// zjhRoom 同时服务房主与客户端。
// 座位用切片表示并始终保持连续（有人掉线就把后面的往前压），
// 所以"房间座位号"和"对局座位号"永远一致；消息靠连接反查座位。
type zjhRoom struct {
	name       string
	you        int
	startChips int
	ante       int
	maxStake   int
	maxRounds  int
	maxSeats   int

	seats   []*zjhSeat
	game    *zjh.Game
	view    zjh.PublicView
	priv    zjh.PrivateView
	peek    string
	started bool
	log     []string

	seatInfos   []ui.ZjhSeatInfo
	pendingKind string
	app         zjhUI
	rng         *rand.Rand
}

func newZjhRoom(name string, chips, ante, maxStake, maxRounds, maxSeats int) *zjhRoom {
	if chips <= 0 {
		chips = 100
	}
	if ante <= 0 {
		ante = 1
	}
	if maxStake < ante {
		maxStake = ante * 20
	}
	if maxRounds <= 0 {
		maxRounds = 6
	}
	if maxSeats <= 0 {
		maxSeats = 4
	}
	return &zjhRoom{
		name:       name,
		startChips: chips,
		ante:       ante,
		maxStake:   maxStake,
		maxRounds:  maxRounds,
		maxSeats:   maxSeats,
		view:       emptyZjhView(),
	}
}

// newZjhHostRoom 建好房主座位（座位 0）。room 是房间标识（端口号），
// hostName 是房主这个玩家的名字，两者不能混。
func newZjhHostRoom(room, hostName string, chips, ante, maxStake, maxRounds, maxSeats int) *zjhRoom {
	r := newZjhRoom(room, chips, ante, maxStake, maxRounds, maxSeats)
	r.you = 0
	r.seats = append(r.seats, &zjhSeat{name: hostName, online: true, joined: true, isHost: true})
	r.refreshSeatInfo()
	return r
}

// emptyZjhView 是"还没开局"的公共状态。Turn 必须显式设成 -1，
// 否则零值 0 会让界面把座位 0 标成当前行动方。
func emptyZjhView() zjh.PublicView {
	return zjh.PublicView{Turn: -1, Winner: -1, TableWinner: -1}
}

func (r *zjhRoom) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	r.log = append(r.log, line)
	if len(r.log) > zjhMaxLog {
		r.log = r.log[len(r.log)-zjhMaxLog:]
	}
}

func (r *zjhRoom) nameOf(seat int) string {
	if seat == zjh.SeatSystem {
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

// seatOf 按连接反查座位号：座位会压缩，钉座位号会张冠李戴。
func (r *zjhRoom) seatOf(conn *netx.Conn) int {
	for i, s := range r.seats {
		if s != nil && s.conn == conn {
			return i
		}
	}
	return -1
}

func (r *zjhRoom) refreshSeatInfo() {
	infos := make([]ui.ZjhSeatInfo, 0, len(r.seats))
	for i := range r.seats {
		info := ui.ZjhSeatInfo{}
		if s := r.seats[i]; s != nil {
			info.Name, info.Online, info.IsHost = s.name, s.online, s.isHost
		}
		if i < len(r.view.Seats) {
			vs := r.view.Seats[i]
			info.Looked, info.Status = vs.Looked, vs.Status
			info.Bet, info.Chips = vs.Bet, vs.Chips
		}
		infos = append(infos, info)
	}
	r.seatInfos = infos
}

func (r *zjhRoom) frame() ui.ZjhFrame {
	seats := make([]ui.ZjhSeatInfo, len(r.seatInfos))
	copy(seats, r.seatInfos)
	return ui.ZjhFrame{
		Room:        r.name,
		You:         r.you,
		Round:       r.view.Round,
		Started:     r.started,
		Phase:       r.view.Phase,
		Turn:        r.view.Turn,
		Pot:         r.view.Pot,
		Stake:       r.view.Stake,
		Ante:        r.view.Ante,
		MaxStake:    r.view.MaxStake,
		RoundInHand: r.view.RoundInHand,
		MaxRounds:   r.view.MaxRounds,
		Seats:       seats,
		Looked:      r.priv.Looked,
		Cards:       r.priv.Cards,
		HandLabel:   r.priv.HandLabel,
		Cost:        r.priv.Cost,
		CanRaise:    r.priv.CanRaise,
		MustCompare: r.priv.MustCompare,
		PeekText:    r.peek,
		Results:     r.view.Results,
		Winner:      r.view.Winner,
		TableWinner: r.view.TableWinner,
		Over:        r.view.Over,
		Log:         append([]string(nil), r.log...),
	}
}

func (r *zjhRoom) render() {
	r.refreshSeatInfo()
	if r.app == nil {
		return
	}
	r.app.UpdateFrame(r.frame())
}

func (r *zjhRoom) seatPayloads() []protocol.ZjhSeatPayload {
	out := make([]protocol.ZjhSeatPayload, 0, len(r.seats))
	for i, s := range r.seats {
		sp := protocol.ZjhSeatPayload{Seat: i}
		if s != nil {
			sp.Name, sp.Online, sp.IsHost = s.name, s.online, s.isHost
		}
		out = append(out, sp)
	}
	return out
}

func (r *zjhRoom) joinedCount() int {
	n := 0
	for _, s := range r.seats {
		if s != nil && s.joined && s.online {
			n++
		}
	}
	return n
}

func (r *zjhRoom) allJoined() bool { return r.joinedCount() == len(r.seats) }

func (r *zjhRoom) unicast(seat int, env protocol.Envelope) {
	if seat < 0 || seat >= len(r.seats) {
		return
	}
	s := r.seats[seat]
	if s == nil || s.conn == nil || !s.online {
		return
	}
	_ = s.conn.Send(env)
}

func (r *zjhRoom) broadcast(env protocol.Envelope) {
	for seat := range r.seats {
		r.unicast(seat, env)
	}
}

func (r *zjhRoom) sendErr(seat int, msg string) {
	env, err := protocol.NewEnvelope(protocol.TypeError, "host", protocol.ErrorPayload{Message: msg})
	if err == nil {
		r.unicast(seat, env)
	}
}

func (r *zjhRoom) broadcastEvent(e zjh.Event) {
	env, err := protocol.NewEnvelope(protocol.TypeZjhEvent, "host", protocol.ZjhEventPayload{Seat: e.Seat, Text: e.Text})
	if err == nil {
		r.broadcast(env)
	}
}

func (r *zjhRoom) afterEvents(events []zjh.Event) {
	for _, e := range events {
		if e.Seat == zjh.SeatSystem {
			r.logf("%s", e.Text)
		} else {
			r.logf("%s %s", r.nameOf(e.Seat), e.Text)
		}
		r.broadcastEvent(e)
	}
}

// pushAll 是对外状态的唯一出口：广播公共状态、单播各自的手牌、把比牌牌面发给当事人。
func (r *zjhRoom) pushAll() {
	if !r.started || r.game == nil {
		r.view = emptyZjhView()
		r.priv = zjh.PrivateView{}
		r.peek = ""
		r.pushLobby()
		r.render()
		return
	}
	r.view = r.game.PublicView()
	r.priv = r.game.PrivateView(r.you)
	r.broadcastState()
	r.render()
}

func (r *zjhRoom) pushLobby() {
	for seat := range r.seats {
		s := r.seats[seat]
		if s == nil || s.conn == nil {
			continue
		}
		env, err := protocol.NewEnvelope(protocol.TypeZjhLobby, "host", protocol.ZjhLobbyPayload{
			You:       seat,
			Seats:     r.seatPayloads(),
			Max:       r.maxSeats,
			Chips:     r.startChips,
			Ante:      r.ante,
			MaxStake:  r.maxStake,
			MaxRounds: r.maxRounds,
		})
		if err == nil {
			_ = s.conn.Send(env)
		}
	}
}

// broadcastState 广播公共状态，然后按"该发给谁"单播手牌与比牌牌面。
func (r *zjhRoom) broadcastState() {
	env, err := protocol.NewEnvelope(protocol.TypeZjhPublic, "host", protocol.ZjhPublicPayload{
		View:  r.view,
		Seats: r.seatPayloads(),
	})
	if err == nil {
		r.broadcast(env)
	}

	// 手牌：只发本人的；没看牌时引擎给的就是空数组
	for seat := range r.seats {
		if seat >= len(r.game.Players) {
			continue
		}
		s := r.seats[seat]
		if s == nil || s.conn == nil {
			continue
		}
		handEnv, err := protocol.NewEnvelope(protocol.TypeZjhHand, "host", protocol.ZjhHandPayload{
			View: r.game.PrivateView(seat),
		})
		if err == nil {
			_ = s.conn.Send(handEnv)
		}
	}

	// 比牌的牌面只发给比牌的两个人
	for _, peek := range r.game.TakePeeks() {
		peekEnv, err := protocol.NewEnvelope(protocol.TypeZjhPeek, "host", protocol.ZjhPeekPayload{Peek: peek})
		if err != nil {
			continue
		}
		r.unicast(peek.A, peekEnv)
		r.unicast(peek.B, peekEnv)
		if r.you == peek.A || r.you == peek.B {
			r.peek = r.peekText(peek)
		}
	}
}

// peekText 把一次比牌描述成"我 vs 他"的一句话，只给当事人看。
func (r *zjhRoom) peekText(p zjh.Peek) string {
	you := r.you
	mine, theirs := p.CardsA, p.CardsB
	myLabel, theirLabel := p.HandA, p.HandB
	other := p.B
	if you == p.B {
		mine, theirs = p.CardsB, p.CardsA
		myLabel, theirLabel = p.HandB, p.HandA
		other = p.A
	}
	result := "你赢"
	if p.Loser == you {
		result = "你输"
	}
	return fmt.Sprintf("%s %s vs %s %s %s → %s",
		myLabel, cardsText(mine), r.nameOf(other), theirLabel, cardsText(theirs), result)
}

func cardsText(cards []zjh.Card) string {
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		parts = append(parts, c.Label())
	}
	return strings.Join(parts, " ")
}

func (r *zjhRoom) relayChat(from int, msg string) {
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

func (r *zjhRoom) rejectHint(err error, cmd zjhCmd) {
	msg := err.Error()
	if cmd.kind == "raise" {
		msg += "（用法: raise 6，要高于当前注额且不超过上限）"
	}
	if cmd.kind == "compare" {
		msg += "（用法: compare 1，填对方座位号）"
	}
	r.logf("操作被拒: %s", msg)
	r.render()
}

// --- 命令解析 ---

type zjhCmd struct {
	kind   string // look / call / raise / fold / compare / next / start / restart / chat / help / leave / bad / none
	amount int
	target int
	text   string
}

func parseZjhCmd(line string) zjhCmd {
	line = strings.TrimSpace(line)
	if line == "" {
		return zjhCmd{kind: "none"}
	}
	lower := strings.ToLower(line)
	switch {
	case lower == "look" || line == "看" || line == "看牌":
		return zjhCmd{kind: "look"}
	case lower == "call" || line == "跟" || line == "跟注":
		return zjhCmd{kind: "call"}
	case lower == "fold" || line == "弃" || line == "弃牌":
		return zjhCmd{kind: "fold"}
	case lower == "start" || line == "开始":
		return zjhCmd{kind: "start"}
	case lower == "restart" || line == "重开":
		return zjhCmd{kind: "restart"}
	case lower == "next" || line == "下一局":
		return zjhCmd{kind: "next"}
	case lower == "help" || line == "帮助" || line == "?":
		return zjhCmd{kind: "help"}
	case lower == "leave" || lower == "quit" || lower == "exit":
		return zjhCmd{kind: "leave"}
	}

	fields := strings.Fields(line)
	head := strings.ToLower(fields[0])
	rest := strings.TrimSpace(line[len(fields[0]):])

	switch head {
	case "raise", "加", "加注":
		if len(fields) != 2 {
			return zjhCmd{kind: "bad", text: "用法: raise 6（要高于当前注额）"}
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n <= 0 {
			return zjhCmd{kind: "bad", text: "注额要写正整数，例如 raise 6"}
		}
		return zjhCmd{kind: "raise", amount: n}
	case "compare", "比", "比牌":
		if len(fields) != 2 {
			return zjhCmd{kind: "bad", text: "用法: compare 1（填对方座位号）"}
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n < 0 {
			return zjhCmd{kind: "bad", text: "座位号要写数字，例如 compare 1"}
		}
		return zjhCmd{kind: "compare", target: n}
	case "chat", "say", "说":
		if strings.TrimSpace(rest) == "" {
			return zjhCmd{kind: "bad", text: "用法: chat TEXT"}
		}
		return zjhCmd{kind: "chat", text: rest}
	}
	return zjhCmd{kind: "bad", text: "无法识别的命令，输入 help 查看用法"}
}

const zjhHelpText = "命令: look 看牌 | call 跟注 | raise N 加注 | fold 弃牌 | compare N 比牌 | next 下一局 | start 开局 | restart 重开 | chat TEXT | leave"

func zjhActionFromCmd(cmd zjhCmd) (zjh.Action, error) {
	switch cmd.kind {
	case "look":
		return zjh.Action{Kind: zjh.ActLook}, nil
	case "call":
		return zjh.Action{Kind: zjh.ActCall}, nil
	case "raise":
		return zjh.Action{Kind: zjh.ActRaise, Amount: cmd.amount}, nil
	case "fold":
		return zjh.Action{Kind: zjh.ActFold}, nil
	case "compare":
		return zjh.Action{Kind: zjh.ActCompare, Target: cmd.target}, nil
	}
	return zjh.Action{}, fmt.Errorf("无法识别的动作")
}
