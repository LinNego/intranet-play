package zjh

import (
	"fmt"
	"math/rand"

	"intranet-play/internal/ddz"
)

// SeatSystem 用于不属于任何玩家的系统事件。
const SeatSystem = -1

// Phase 是对局阶段。
type Phase string

const (
	PhaseBetting Phase = "BETTING" // 下注
	PhaseSettle  Phase = "SETTLE"  // 本局结束，等 next
	PhaseOver    Phase = "OVER"    // 整桌结束（只剩一个人还有筹码）
)

// Status 是单个座位的状态。
type Status string

const (
	StatusPlaying Status = "PLAYING" // 在局中
	StatusFolded  Status = "FOLDED"  // 本局已弃牌
	StatusOut     Status = "OUT"     // 筹码输光，已出局
)

// ActionKind 是玩家动作类型。
type ActionKind string

const (
	ActLook    ActionKind = "LOOK"    // 看牌（免费，变成明牌）
	ActCall    ActionKind = "CALL"    // 跟注
	ActRaise   ActionKind = "RAISE"   // 加注到 Amount
	ActFold    ActionKind = "FOLD"    // 弃牌
	ActCompare ActionKind = "COMPARE" // 与 Target 比牌
)

// Action 是客户端上行的动作。
type Action struct {
	Kind   ActionKind `json:"kind"`
	Amount int        `json:"amount,omitempty"` // RAISE：新的注额
	Target int        `json:"target,omitempty"` // COMPARE：跟谁比
}

// Event 是需要广播给所有人的消息。
type Event struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}

// Player 是一个座位在本局里的状态。Cards 只在服务端，未看牌时不下发。
type Player struct {
	Cards  []Card `json:"cards"`
	Looked bool   `json:"looked"`
	Status Status `json:"status"`
	Bet    int    `json:"bet"` // 本局累计投入
}

// Result 是一个座位的结算明细。
type Result struct {
	Seat   int    `json:"seat"`
	Delta  int    `json:"delta"`
	Chips  int    `json:"chips"`
	Reason string `json:"reason"`
}

// Peek 是一次比牌的记录：牌面只发给比牌的两个人。
type Peek struct {
	A      int    `json:"a"`
	B      int    `json:"b"`
	CardsA []Card `json:"cardsA"`
	CardsB []Card `json:"cardsB"`
	HandA  string `json:"handA"`
	HandB  string `json:"handB"`
	Loser  int    `json:"loser"`
	Reason string `json:"reason"`
}

// Game 是 Host 权威的对局状态。所有校验集中在 Apply 内，外部不得直接改状态。
type Game struct {
	Players     []Player
	Chips       []int
	Pot         int
	Stake       int // 当前注额（明牌价）
	Ante        int
	MaxStake    int
	MaxRounds   int
	Turn        int
	Phase       Phase
	Round       int // 第几局
	RoundInHand int // 本局已经进行的圈数
	Winner      int // 本局赢家；-1 表示未定
	TableWinner int // 整桌赢家；-1 表示整桌还没结束
	LastEvent   string

	startChips int
	rng        *rand.Rand
	results    []Result
	peeks      []Peek
	acted      int // 本圈已经行动过的人数
}

// NewGame 建一桌炸金花：发牌、收底注，进入下注阶段。
func NewGame(rng *rand.Rand, seats, chips, ante, maxStake, maxRounds int) *Game {
	if seats < 1 {
		seats = 1
	}
	if chips <= 0 {
		chips = 100
	}
	if ante <= 0 {
		ante = 1
	}
	if maxStake < ante {
		maxStake = ante * 10
	}
	if maxRounds <= 0 {
		maxRounds = 6
	}
	g := &Game{
		rng:         rng,
		startChips:  chips,
		Ante:        ante,
		MaxStake:    maxStake,
		MaxRounds:   maxRounds,
		Round:       1,
		Winner:      -1,
		TableWinner: -1,
		Phase:       PhaseBetting,
		Players:     make([]Player, seats),
		Chips:       make([]int, seats),
	}
	for i := range g.Chips {
		g.Chips[i] = chips
	}
	g.beginHand()
	return g
}

// Apply 是唯一的推进入口。
func (g *Game) Apply(seat int, act Action) ([]Event, error) {
	if seat < 0 || seat >= len(g.Players) {
		return nil, fmt.Errorf("座位号非法: %d", seat)
	}
	if g.Phase == PhaseOver {
		return nil, fmt.Errorf("整桌已经结束（房主 restart 可重开）")
	}
	p := &g.Players[seat]
	if p.Status == StatusOut {
		return nil, fmt.Errorf("你已经出局了（等房主 restart 重开一桌）")
	}

	// 看牌只影响自己，随时可用（包括不在自己回合时）
	if act.Kind == ActLook {
		if p.Looked {
			return nil, fmt.Errorf("你已经看过牌了")
		}
		p.Looked = true
		return []Event{{Seat: seat, Text: "看牌"}}, nil
	}

	if g.Phase != PhaseBetting {
		return nil, fmt.Errorf("本局已结束，输入 next 开下一局")
	}
	if seat != g.Turn {
		return nil, fmt.Errorf("还没轮到你")
	}
	if p.Status != StatusPlaying {
		return nil, fmt.Errorf("你已经弃牌了")
	}

	switch act.Kind {
	case ActCall:
		if g.RoundInHand >= g.MaxRounds {
			return nil, fmt.Errorf("已经打了 %d 圈，只能比牌或弃牌", g.MaxRounds)
		}
		cost := g.Cost(seat)
		if err := g.pay(seat, cost); err != nil {
			return nil, err
		}
		g.tickRound()
		events := []Event{{Seat: seat, Text: fmt.Sprintf("跟注 %d", cost)}}
		return append(events, g.advance(seat)...), nil

	case ActRaise:
		if act.Amount <= g.Stake {
			return nil, fmt.Errorf("加注要高于当前注额 %d", g.Stake)
		}
		if act.Amount > g.MaxStake {
			if g.Stake >= g.MaxStake {
				return nil, fmt.Errorf("注额已经到顶（%d），只能跟注/比牌/弃牌", g.MaxStake)
			}
			return nil, fmt.Errorf("最多加到 %d", g.MaxStake)
		}
		if g.RoundInHand >= g.MaxRounds {
			return nil, fmt.Errorf("已经打了 %d 圈，只能比牌或弃牌", g.MaxRounds)
		}
		// 先把注额抬上去再算花费；付不起就把注额回滚，不能留下半截状态
		oldStake := g.Stake
		g.Stake = act.Amount
		cost := g.Cost(seat)
		if err := g.pay(seat, cost); err != nil {
			g.Stake = oldStake
			return nil, err
		}
		g.tickRound()
		events := []Event{{Seat: seat, Text: fmt.Sprintf("加注到 %d（付 %d）", g.Stake, cost)}}
		return append(events, g.advance(seat)...), nil

	case ActFold:
		p.Status = StatusFolded
		g.tickRound()
		events := []Event{{Seat: seat, Text: "弃牌"}}
		if done := g.afterLeave(); len(done) > 0 {
			return append(events, done...), nil
		}
		return append(events, g.advance(seat)...), nil

	case ActCompare:
		target := act.Target
		if target < 0 || target >= len(g.Players) || target == seat {
			return nil, fmt.Errorf("要比牌的座位号不对")
		}
		t := &g.Players[target]
		if t.Status != StatusPlaying {
			return nil, fmt.Errorf("%s 不在局中，比不了", g.nameOf(target))
		}
		cost := g.Cost(seat)
		if err := g.pay(seat, cost); err != nil {
			return nil, err
		}
		g.tickRound()
		events := []Event{{Seat: seat, Text: fmt.Sprintf("花 %d 跟 %s 比牌", cost, g.nameOf(target))}}
		events = append(events, g.compare(seat, target)...)
		// 不论谁出局，比牌之后回合都要往下走（除非本局已经结束）
		if g.Phase == PhaseBetting {
			g.Turn = g.nextPlaying((seat + 1) % len(g.Players))
		}
		return events, nil
	}
	return nil, fmt.Errorf("只能 look / call / raise / fold / compare")
}

// Cost 返回某个座位现在跟注（或比牌）要付多少：闷牌半价，最低 1。
func (g *Game) Cost(seat int) int {
	if seat < 0 || seat >= len(g.Players) {
		return 0
	}
	if g.Players[seat].Looked {
		return g.Stake
	}
	if half := g.Stake / 2; half > 1 {
		return half
	}
	return 1
}

// CanRaise 表示这个座位现在还能不能加注。
func (g *Game) CanRaise(seat int) bool {
	return g.Stake < g.MaxStake && g.RoundInHand < g.MaxRounds
}

func (g *Game) nameOf(seat int) string {
	return fmt.Sprintf("座位%d", seat)
}

func (g *Game) pay(seat, cost int) error {
	if cost <= 0 {
		return fmt.Errorf("花费不合法")
	}
	if g.Chips[seat] < cost {
		return fmt.Errorf("筹码不够（要付 %d，你只有 %d），只能弃牌", cost, g.Chips[seat])
	}
	g.Chips[seat] -= cost
	g.Players[seat].Bet += cost
	g.Pot += cost
	return nil
}

// playingSeats 返回还在局中的座位。
func (g *Game) playingSeats() []int {
	out := make([]int, 0, len(g.Players))
	for i := range g.Players {
		if g.Players[i].Status == StatusPlaying {
			out = append(out, i)
		}
	}
	return out
}

func (g *Game) aliveSeats() []int {
	out := make([]int, 0, len(g.Players))
	for i := range g.Players {
		if g.Players[i].Status != StatusOut {
			out = append(out, i)
		}
	}
	return out
}

// beginHand 开一局：清算出局、洗牌发牌、收底注。
func (g *Game) beginHand() []Event {
	events := make([]Event, 0, 2)

	// 筹码不够付底注的直接出局
	for i := range g.Players {
		if g.Players[i].Status != StatusOut && g.Chips[i] < g.Ante {
			g.Players[i].Status = StatusOut
			events = append(events, Event{Seat: i, Text: "筹码不够底注，出局"})
		}
	}
	alive := g.aliveSeats()
	if len(alive) < 2 {
		g.Phase = PhaseOver
		g.TableWinner = -1
		if len(alive) == 1 {
			g.TableWinner = alive[0]
			events = append(events, Event{Seat: g.TableWinner, Text: "其他人都出局了，你赢到最后！"})
		} else {
			events = append(events, Event{Text: "没人能继续玩了，这桌结束"})
		}
		g.LastEvent = "整桌结束"
		return events
	}

	deck := shuffle(g.rng)
	g.Pot = 0
	g.Stake = g.Ante
	g.RoundInHand = 0
	g.acted = 0
	g.Winner = -1
	g.results = nil
	g.peeks = nil

	card := 0
	for i := range g.Players {
		if g.Players[i].Status == StatusOut {
			g.Players[i] = Player{Status: StatusOut}
			continue
		}
		hand := make([]Card, 3)
		copy(hand, deck[card:card+3])
		card += 3
		g.Players[i] = Player{Cards: hand, Status: StatusPlaying}
	}

	// 收底注
	for _, seat := range g.playingSeats() {
		g.Chips[seat] -= g.Ante
		g.Players[seat].Bet = g.Ante
		g.Pot += g.Ante
	}

	g.Phase = PhaseBetting
	g.Turn = g.playingSeats()[0]
	g.LastEvent = fmt.Sprintf("第 %d 局：每人底注 %d，请下注", g.Round, g.Ante)
	events = append(events, Event{Seat: SeatSystem, Text: fmt.Sprintf("第 %d 局开始，底注 %d，奖池 %d", g.Round, g.Ante, g.Pot)})
	return events
}

func shuffle(rng *rand.Rand) []Card {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	deck := ddz.NewDeck()
	// 炸金花不用大小王
	out := make([]Card, 0, 52)
	for _, c := range deck {
		if c.Rank == ddz.RankJokerSmall || c.Rank == ddz.RankJokerBig {
			continue
		}
		out = append(out, c)
	}
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// tickRound 记一次行动：一圈 = 所有在局玩家各行动一次。
func (g *Game) tickRound() {
	g.acted++
	if playing := len(g.playingSeats()); playing > 0 && g.acted >= playing {
		g.acted = 0
		g.RoundInHand++
	}
}

// advance 把回合交给下一个在局玩家。
func (g *Game) advance(from int) []Event {
	if next := g.nextPlaying((from + 1) % len(g.Players)); next >= 0 {
		g.Turn = next
	}
	return nil
}

func (g *Game) nextPlaying(from int) int {
	n := len(g.Players)
	for i := 0; i < n; i++ {
		seat := (from + i) % n
		if g.Players[seat].Status == StatusPlaying {
			return seat
		}
	}
	return -1
}

// afterLeave 在有人弃牌（或比牌输掉）之后判断本局是否结束。
func (g *Game) afterLeave() []Event {
	playing := g.playingSeats()
	if len(playing) <= 1 {
		winner := -1
		if len(playing) == 1 {
			winner = playing[0]
		}
		return g.finish(winner)
	}
	return nil
}

// compare 执行一次比牌：输的人弃牌，牌面记进 peek 里等房间层单播。
func (g *Game) compare(a, b int) []Event {
	ha := Evaluate(g.Players[a].Cards)
	hb := Evaluate(g.Players[b].Cards)
	res := Compare(ha, hb)

	loser := -1
	reason := ""
	switch {
	case res < 0:
		loser, reason = a, fmt.Sprintf("%s 的%s 压过 %s 的%s", g.nameOf(b), hb.Label(), g.nameOf(a), ha.Label())
	case res > 0:
		loser, reason = b, fmt.Sprintf("%s 的%s 压过 %s 的%s", g.nameOf(a), ha.Label(), g.nameOf(b), hb.Label())
	default:
		// 完全一样大：闷牌占优；都明或都闷则主动比牌的人输
		blindA := !g.Players[a].Looked
		blindB := !g.Players[b].Looked
		switch {
		case blindA && !blindB:
			loser, reason = b, fmt.Sprintf("牌一样大，%s 闷牌占优", g.nameOf(a))
		case blindB && !blindA:
			loser, reason = a, fmt.Sprintf("牌一样大，%s 闷牌占优", g.nameOf(b))
		default:
			loser, reason = a, "牌一样大，主动比牌的一方输"
		}
	}

	g.Players[loser].Status = StatusFolded
	g.peeks = append(g.peeks, Peek{
		A:      a,
		B:      b,
		CardsA: append([]Card(nil), g.Players[a].Cards...),
		CardsB: append([]Card(nil), g.Players[b].Cards...),
		HandA:  ha.Label(),
		HandB:  hb.Label(),
		Loser:  loser,
		Reason: reason,
	})

	events := []Event{
		{Seat: a, Text: fmt.Sprintf("比牌结果：%s", reason)},
	}
	return append(events, g.afterLeave()...)
}

// finish 结算本局：赢家拿走奖池。
//
// 注意：下注时筹码已经扣走并进了奖池，所以这里**只把奖池发给赢家**，
// 不能再退一次本金——那样等于凭空造钱。
func (g *Game) finish(winner int) []Event {
	g.Winner = winner
	g.Phase = PhaseSettle

	pot := g.Pot
	if winner >= 0 {
		g.Chips[winner] += pot
	}
	g.Pot = 0 // 已经派发完毕

	g.results = make([]Result, 0, len(g.Players))
	for i := range g.Players {
		delta := 0
		reason := ""
		switch {
		case i == winner:
			delta = pot - g.Players[i].Bet
			reason = "赢下奖池"
		case g.Players[i].Status == StatusOut:
			delta = 0
			reason = "已出局"
		default:
			delta = -g.Players[i].Bet
			reason = "本局投入"
		}
		g.results = append(g.results, Result{Seat: i, Delta: delta, Chips: g.Chips[i], Reason: reason})
	}

	// 筹码输光的出局
	events := make([]Event, 0, 2)
	for i := range g.Players {
		if g.Players[i].Status != StatusOut && g.Chips[i] <= 0 {
			g.Players[i].Status = StatusOut
			events = append(events, Event{Seat: i, Text: "筹码输光，出局"})
		}
	}
	if len(g.aliveSeats()) < 2 {
		g.Phase = PhaseOver
		g.TableWinner = -1
		for _, seat := range g.aliveSeats() {
			g.TableWinner = seat
		}
		if g.TableWinner >= 0 {
			events = append(events, Event{Seat: g.TableWinner, Text: "其他人都出局了，你赢到最后！"})
		}
	}

	if winner >= 0 {
		if pot > 0 {
			events = append([]Event{{Seat: winner, Text: fmt.Sprintf("赢下奖池 %d", pot)}}, events...)
		}
	}
	g.LastEvent = "本局结束，输入 next 开下一局"
	return events
}

// NextRound 开下一局（筹码不够底注的人在 beginHand 里出局）。
func (g *Game) NextRound() []Event {
	if g.Phase == PhaseOver {
		return []Event{{Seat: SeatSystem, Text: "整桌已经结束了，房主 restart 可重开"}}
	}
	g.Round++
	return g.beginHand()
}

// TakePeeks 取出并清空待发送的比牌记录。
func (g *Game) TakePeeks() []Peek {
	if len(g.peeks) == 0 {
		return nil
	}
	out := g.peeks
	g.peeks = nil
	return out
}

// --- 视图 ---

// SeatView 是公共视图里的一个座位。这里**没有牌面**。
type SeatView struct {
	Seat   int    `json:"seat"`
	Looked bool   `json:"looked"`
	Status Status `json:"status"`
	Bet    int    `json:"bet"`
	Chips  int    `json:"chips"`
}

// PublicView 广播给所有人：奖池、注额、各家状态，没有任何牌面。
type PublicView struct {
	Round       int        `json:"round"`
	Phase       Phase      `json:"phase"`
	Turn        int        `json:"turn"`
	Pot         int        `json:"pot"`
	Stake       int        `json:"stake"`
	Ante        int        `json:"ante"`
	MaxStake    int        `json:"maxStake"`
	RoundInHand int        `json:"roundInHand"`
	MaxRounds   int        `json:"maxRounds"`
	Seats       []SeatView `json:"seats"`
	Winner      int        `json:"winner"`
	TableWinner int        `json:"tableWinner"`
	Over        bool       `json:"over"`
	Results     []Result   `json:"results,omitempty"`
	LastEvent   string     `json:"lastEvent"`
}

// PrivateView 只发给本人。**没看牌时 Cards 为空**——牌不能提前下发，
// 否则改个客户端就能偷看。
type PrivateView struct {
	Seat        int    `json:"seat"`
	Looked      bool   `json:"looked"`
	Cards       []Card `json:"cards,omitempty"`
	HandLabel   string `json:"handLabel,omitempty"`
	IsTurn      bool   `json:"isTurn"`
	Cost        int    `json:"cost"`
	Stake       int    `json:"stake"`
	CanRaise    bool   `json:"canRaise"`
	MaxStake    int    `json:"maxStake"`
	MustCompare bool   `json:"mustCompare"` // 圈数到顶，只能比牌或弃牌
}

func (g *Game) PublicView() PublicView {
	v := PublicView{
		Round:       g.Round,
		Phase:       g.Phase,
		Turn:        g.Turn,
		Pot:         g.Pot,
		Stake:       g.Stake,
		Ante:        g.Ante,
		MaxStake:    g.MaxStake,
		RoundInHand: g.RoundInHand,
		MaxRounds:   g.MaxRounds,
		Winner:      g.Winner,
		TableWinner: g.TableWinner,
		Over:        g.Phase == PhaseOver,
		Results:     g.results,
		LastEvent:   g.LastEvent,
	}
	v.Seats = make([]SeatView, 0, len(g.Players))
	for i, p := range g.Players {
		v.Seats = append(v.Seats, SeatView{
			Seat:   i,
			Looked: p.Looked,
			Status: p.Status,
			Bet:    p.Bet,
			Chips:  g.Chips[i],
		})
	}
	return v
}

func (g *Game) PrivateView(seat int) PrivateView {
	if seat < 0 || seat >= len(g.Players) {
		return PrivateView{}
	}
	p := g.Players[seat]
	view := PrivateView{
		Seat:        seat,
		Looked:      p.Looked,
		IsTurn:      g.Phase == PhaseBetting && seat == g.Turn && p.Status == StatusPlaying,
		Cost:        g.Cost(seat),
		Stake:       g.Stake,
		CanRaise:    g.CanRaise(seat),
		MaxStake:    g.MaxStake,
		MustCompare: g.RoundInHand >= g.MaxRounds,
	}
	if p.Looked {
		view.Cards = append([]Card(nil), p.Cards...)
		view.HandLabel = Evaluate(p.Cards).Label()
	}
	return view
}
