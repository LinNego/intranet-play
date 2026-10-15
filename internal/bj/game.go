package bj

import (
	"fmt"
	"math/rand"
)

// SeatSystem 用于不属于任何玩家的系统事件。
const SeatSystem = -1

// DealerMode 决定谁来坐庄。
type DealerMode string

const (
	// DealerVirtual 是虚拟庄家：程序按固定规则坐庄，所有人都是闲家、都下注。
	// 2 人局用这个，否则每局都有一半人没牌可打。
	DealerVirtual DealerMode = "VIRTUAL"
	// DealerRotate 是轮流坐庄：每轮换一个人坐庄，他不下注、也没决策（按规则自动补牌）。
	DealerRotate DealerMode = "ROTATE"
)

// Phase 是对局阶段。庄家补牌是 PLAYING 结束时的一次自动步骤，不单独占一个交互阶段。
type Phase string

const (
	PhaseBetting Phase = "BETTING" // 下注
	PhasePlaying Phase = "PLAYING" // 闲家依次要牌/停牌
	PhaseSettle  Phase = "SETTLE"  // 已结算，等 next 开下一轮
	PhaseOver    Phase = "OVER"    // 整桌结束（只剩一个人没出局）
)

// Status 是单个座位的状态。
type Status string

const (
	StatusIdle      Status = "IDLE"      // 还没下注
	StatusBet       Status = "BET"       // 已下注，等其他人下注
	StatusDealer    Status = "DEALER"    // 本局坐庄，不下注、不决策
	StatusOut       Status = "OUT"       // 筹码输光，已出局
	StatusWaiting   Status = "WAITING"   // 已发牌，等待行动
	StatusStood     Status = "STOOD"     // 停牌
	StatusBust      Status = "BUST"      // 爆牌
	StatusBlackjack Status = "BLACKJACK" // 起手 21 点，自动停牌
)

// ActionKind 是玩家动作类型。
type ActionKind string

const (
	ActBet    ActionKind = "BET"    // 下注，Amount = 注额
	ActHit    ActionKind = "HIT"    // 要牌
	ActStand  ActionKind = "STAND"  // 停牌
	ActDouble ActionKind = "DOUBLE" // 双倍：加注一份、只补一张、自动停牌
)

// Action 是客户端上行的动作。
type Action struct {
	Kind   ActionKind `json:"kind"`
	Amount int        `json:"amount,omitempty"`
}

// Event 是需要广播给所有人的消息。
type Event struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}

// Hand 是一个座位的手牌与状态。
type Hand struct {
	Bet    int    `json:"bet"`
	Status Status `json:"status"`
	Cards  []Card `json:"cards"`
}

// Result 是一个闲家的结算明细。
type Result struct {
	Seat   int    `json:"seat"`
	Delta  int    `json:"delta"` // 本局净输赢（正=赢，负=输，0=平）
	Chips  int    `json:"chips"` // 结算后的筹码
	Reason string `json:"reason"`
}

// Game 是 Host 权威的对局状态。所有校验集中在 Apply 内，外部不得直接改状态。
//
// 筹码是"真"的：输光即出局，不再自动补满；轮庄模式下筹码只在玩家之间流动。
type Game struct {
	Hands       []Hand
	Dealer      []Card // 庄家的牌（虚拟庄家也放这里）
	DealerSeat  int    // 本局坐庄的座位；-1 表示虚拟庄家
	Mode        DealerMode
	Phase       Phase
	Turn        int
	Chips       []int
	Round       int
	DealerDelta int // 轮庄时庄家的净输赢
	Winner      int // 整桌赢家；-1 表示整桌还没结束
	LastEvent   string

	startChips int
	rng        *rand.Rand
	shoe       []Card
	hidden     bool // 庄家暗牌是否还盖着
	results    []Result
	lastDealer int // 上一局的庄家座位，用于轮庄
}

// NewGame 建一桌 21 点。mode 决定是虚拟庄家还是轮流坐庄。
func NewGame(rng *rand.Rand, seats, chips int, mode DealerMode) *Game {
	if seats < 1 {
		seats = 1
	}
	if chips <= 0 {
		chips = 100
	}
	if mode != DealerRotate {
		mode = DealerVirtual
	}
	g := &Game{
		rng:        rng,
		startChips: chips,
		Mode:       mode,
		Phase:      PhaseBetting,
		Round:      1,
		DealerSeat: -1,
		Winner:     -1,
		lastDealer: -1,
		Hands:      make([]Hand, seats),
		Chips:      make([]int, seats),
	}
	for i := range g.Chips {
		g.Chips[i] = chips
	}
	for i := range g.Hands {
		g.Hands[i].Status = StatusIdle
	}
	g.beginRound(true)
	return g
}

// Apply 是唯一的推进入口。
func (g *Game) Apply(seat int, act Action) ([]Event, error) {
	if seat < 0 || seat >= len(g.Hands) {
		return nil, fmt.Errorf("座位号非法: %d", seat)
	}
	if g.Phase == PhaseOver {
		return nil, fmt.Errorf("整桌已经结束（房主 restart 可重开）")
	}
	if g.Hands[seat].Status == StatusOut {
		return nil, fmt.Errorf("你已经出局了（等房主 restart 重开一桌）")
	}
	switch g.Phase {
	case PhaseBetting:
		return g.applyBet(seat, act)
	case PhasePlaying:
		return g.applyPlay(seat, act)
	case PhaseSettle:
		return nil, fmt.Errorf("本局已结算，输入 next 开下一轮")
	}
	return nil, fmt.Errorf("当前阶段不接受操作: %s", g.Phase)
}

func (g *Game) applyBet(seat int, act Action) ([]Event, error) {
	if act.Kind != ActBet {
		return nil, fmt.Errorf("下注阶段只能下注（bet N）")
	}
	h := &g.Hands[seat]
	switch h.Status {
	case StatusDealer:
		return nil, fmt.Errorf("本局你坐庄，不用下注")
	case StatusIdle:
	default:
		return nil, fmt.Errorf("你这一轮不用再下注了")
	}
	if act.Amount <= 0 {
		return nil, fmt.Errorf("注额必须大于 0")
	}
	if limit := g.MaxBet(seat); act.Amount > limit {
		return nil, fmt.Errorf("最多只能押 %d（你有 %d，庄家要赔得起）", limit, g.Chips[seat])
	}

	h.Bet = act.Amount
	h.Status = StatusBet
	g.Chips[seat] -= act.Amount
	events := []Event{{Seat: seat, Text: fmt.Sprintf("下注 %d", act.Amount)}}

	if g.allBet() {
		events = append(events, g.deal()...)
	}
	return events, nil
}

// MaxBet 返回某个闲家本局最多能押多少。
// 轮庄时要保证庄家最坏情况（所有人都是黑杰克，赔 1.5 倍）也赔得起。
func (g *Game) MaxBet(seat int) int {
	if seat < 0 || seat >= len(g.Chips) {
		return 0
	}
	limit := g.Chips[seat]
	if g.DealerSeat >= 0 {
		farmers := g.farmerCount()
		if farmers > 0 {
			byDealer := g.Chips[g.DealerSeat] * 2 / (3 * farmers)
			if byDealer < limit {
				limit = byDealer
			}
		}
	}
	return limit
}

func (g *Game) farmerCount() int {
	n := 0
	for i := range g.Hands {
		if i == g.DealerSeat {
			continue
		}
		if g.Hands[i].Status == StatusOut {
			continue
		}
		n++
	}
	return n
}

func (g *Game) allBet() bool {
	for i := range g.Hands {
		switch g.Hands[i].Status {
		case StatusBet, StatusDealer, StatusOut:
			continue
		default:
			return false
		}
	}
	return true
}

func (g *Game) aliveSeats() []int {
	out := make([]int, 0, len(g.Hands))
	for i := range g.Hands {
		if g.Hands[i].Status != StatusOut {
			out = append(out, i)
		}
	}
	return out
}

// dealerNeed 是坐庄需要的筹码：最坏情况要按 1.5 倍赔给每个闲家。
func dealerNeed(farmers int) int {
	if farmers <= 0 {
		return 1
	}
	return (3*farmers + 1) / 2
}

// beginRound 开始一轮：清算出局、决定庄家、把状态摆好。
// first 为 true 表示这是第一轮（庄家从头开始轮）。
func (g *Game) beginRound(first bool) []Event {
	events := make([]Event, 0, 2)

	// 筹码输光的直接出局
	for i := range g.Hands {
		if g.Hands[i].Status != StatusOut && g.Chips[i] <= 0 {
			g.Hands[i].Status = StatusOut
			events = append(events, Event{Seat: i, Text: "筹码输光，出局"})
		}
	}

	g.DealerSeat = -1
	g.DealerDelta = 0
	g.results = nil
	g.Dealer = nil
	g.hidden = false

	// 决定庄家
	if g.Mode == DealerRotate {
		for {
			seat := g.nextDealerCandidate()
			if seat < 0 {
				break
			}
			farmers := len(g.aliveSeats()) - 1
			if farmers <= 0 {
				break
			}
			if g.Chips[seat] >= dealerNeed(farmers) {
				g.DealerSeat = seat
				break
			}
			// 赔不起就没资格坐庄，直接出局（"赔不起就输了"）
			g.Hands[seat].Status = StatusOut
			events = append(events, Event{
				Seat: seat,
				Text: fmt.Sprintf("筹码不足 %d，坐不起庄，出局", dealerNeed(farmers)),
			})
		}
	}

	alive := g.aliveSeats()
	// 一个人开的桌可以一直对着虚拟庄家打；多人桌里只剩一个还有筹码的人就算他赢到最后。
	// 轮庄至少要有一个庄家加一个闲家。
	minAlive := 1
	if g.Mode == DealerRotate || len(g.Hands) >= 2 {
		minAlive = 2
	}
	if len(alive) < minAlive || (g.Mode == DealerRotate && g.DealerSeat < 0 && len(alive) > 0) {
		g.Phase = PhaseOver
		g.Winner = -1
		if len(alive) == 1 {
			g.Winner = alive[0]
			events = append(events, Event{Seat: g.Winner, Text: "其他人都出局了，你赢到最后！"})
		} else {
			events = append(events, Event{Text: "没人能继续坐庄，这桌结束"})
		}
		g.LastEvent = "整桌结束"
		return events
	}

	// 摆好本轮状态
	for i := range g.Hands {
		switch {
		case g.Hands[i].Status == StatusOut:
			g.Hands[i] = Hand{Status: StatusOut}
		case i == g.DealerSeat:
			g.Hands[i] = Hand{Status: StatusDealer}
		default:
			g.Hands[i] = Hand{Status: StatusIdle}
		}
	}
	g.Phase = PhaseBetting
	g.Turn = 0
	if g.DealerSeat >= 0 {
		g.lastDealer = g.DealerSeat
	}
	g.LastEvent = fmt.Sprintf("第 %d 轮：下注阶段", g.Round)
	if !first {
		events = append(events, Event{Seat: SeatSystem, Text: fmt.Sprintf("第 %d 轮开始，请下注", g.Round)})
	}
	return events
}

// nextDealerCandidate 从上一个庄家的下一位开始，找第一个还能坐庄的座位。
func (g *Game) nextDealerCandidate() int {
	n := len(g.Hands)
	for i := 1; i <= n; i++ {
		seat := ((g.lastDealer+i)%n + n) % n
		if g.Hands[seat].Status != StatusOut {
			return seat
		}
	}
	return -1
}

// deal 发牌：每个闲家两张明牌，庄家一明一暗（暗牌只是不下发，牌面仍在服务端）。
func (g *Game) deal() []Event {
	g.shoe = NewShoe(g.rng)
	g.Dealer = nil
	g.hidden = true

	for i := range g.Hands {
		g.Hands[i].Cards = nil
		if g.Hands[i].Status == StatusBet {
			g.Hands[i].Status = StatusWaiting
		}
	}
	for round := 0; round < 2; round++ {
		for i := range g.Hands {
			if g.Hands[i].Status == StatusWaiting {
				g.Hands[i].Cards = append(g.Hands[i].Cards, g.draw())
			}
		}
		g.Dealer = append(g.Dealer, g.draw())
	}

	events := []Event{{Seat: SeatSystem, Text: "发牌完成"}}
	for i := range g.Hands {
		if g.Hands[i].Status == StatusWaiting && IsBlackjack(g.Hands[i].Cards) {
			g.Hands[i].Status = StatusBlackjack
			events = append(events, Event{Seat: i, Text: "起手 21 点，黑杰克！"})
		}
	}

	g.Phase = PhasePlaying
	g.Turn = g.nextActor(0)
	if g.Turn < 0 { // 所有闲家都是黑杰克，直接进庄家
		events = append(events, g.playDealer()...)
	}
	return events
}

func (g *Game) applyPlay(seat int, act Action) ([]Event, error) {
	if seat != g.Turn {
		return nil, fmt.Errorf("还没轮到你")
	}
	h := &g.Hands[seat]

	switch act.Kind {
	case ActHit:
		if h.Status != StatusWaiting {
			return nil, fmt.Errorf("你现在不能要牌")
		}
		h.Cards = append(h.Cards, g.draw())
		events := []Event{{Seat: seat, Text: fmt.Sprintf("要了一张，%s", Describe(h.Cards))}}
		switch {
		case IsBust(h.Cards):
			h.Status = StatusBust
			events = append(events, Event{Seat: seat, Text: fmt.Sprintf("爆牌（%s）", Describe(h.Cards))})
			return append(events, g.advance(seat)...), nil
		case isTarget(h.Cards):
			h.Status = StatusStood // 恰好 21 点自动停牌，省一次操作
			events = append(events, Event{Seat: seat, Text: "21 点，自动停牌"})
			return append(events, g.advance(seat)...), nil
		}
		// 没爆也没到 21 点：回合还是自己的，可以继续要牌
		return events, nil

	case ActStand:
		if h.Status != StatusWaiting {
			return nil, fmt.Errorf("你现在不能停牌")
		}
		h.Status = StatusStood
		events := []Event{{Seat: seat, Text: fmt.Sprintf("停牌，%s", Describe(h.Cards))}}
		return append(events, g.advance(seat)...), nil

	case ActDouble:
		if len(h.Cards) != 2 {
			return nil, fmt.Errorf("只有起手两张才能双倍")
		}
		if h.Status != StatusWaiting {
			return nil, fmt.Errorf("你现在不能双倍")
		}
		if g.Chips[seat] < h.Bet {
			return nil, fmt.Errorf("筹码不够双倍（还要再加 %d，你现在有 %d）", h.Bet, g.Chips[seat])
		}
		if limit := g.MaxBet(seat); h.Bet*2 > limit {
			return nil, fmt.Errorf("庄家赔不起，最多只能押到 %d（现在是 %d）", limit, h.Bet)
		}
		g.Chips[seat] -= h.Bet
		h.Bet *= 2
		h.Cards = append(h.Cards, g.draw())
		h.Status = StatusStood
		if IsBust(h.Cards) {
			h.Status = StatusBust
		}
		events := []Event{{Seat: seat, Text: fmt.Sprintf("双倍到 %d，%s", h.Bet, Describe(h.Cards))}}
		return append(events, g.advance(seat)...), nil
	}
	return nil, fmt.Errorf("行动阶段只能 hit / stand / double")
}

func isTarget(cards []Card) bool {
	total, _ := Score(cards)
	return total == Target
}

// nextActor 找下一个还需要行动的座位（StatusWaiting），没有则返回 -1。
func (g *Game) nextActor(from int) int {
	n := len(g.Hands)
	for i := 0; i < n; i++ {
		seat := (from + i) % n
		if g.Hands[seat].Status == StatusWaiting {
			return seat
		}
	}
	return -1
}

func (g *Game) advance(from int) []Event {
	next := g.nextActor((from + 1) % len(g.Hands))
	if next >= 0 {
		g.Turn = next
		return nil
	}
	return g.playDealer()
}

// playDealer 庄家按 <17 必须要牌、>=17 停的规则补牌，然后结算。
func (g *Game) playDealer() []Event {
	events := make([]Event, 0, 2)

	// 所有闲家都爆牌时庄家不补牌（标准规则），只翻开暗牌
	anyAlive := false
	for i := range g.Hands {
		if i == g.DealerSeat {
			continue
		}
		if g.Hands[i].Status != StatusBust && g.Hands[i].Status != StatusDealer && g.Hands[i].Status != StatusOut {
			anyAlive = true
			break
		}
	}
	if anyAlive {
		for {
			total, _ := Score(g.Dealer)
			if total >= DealerStand {
				break
			}
			g.Dealer = append(g.Dealer, g.draw())
		}
	}

	g.hidden = false
	events = append(events, Event{Seat: SeatSystem, Text: fmt.Sprintf("庄家 %s", Describe(g.Dealer))})
	g.settle()
	return events
}

// settle 逐个闲家与庄家比大小并更新筹码。
// 押注时已扣过筹码，这里退还本金 + 净输赢；轮庄时庄家的筹码按闲家净输赢之和反向变动。
func (g *Game) settle() {
	dealerTotal, _ := Score(g.Dealer)
	dealerBJ := IsBlackjack(g.Dealer)
	dealerBust := dealerTotal > Target

	g.results = make([]Result, 0, len(g.Hands))
	sumDelta := 0
	for i := range g.Hands {
		if i == g.DealerSeat || g.Hands[i].Status == StatusOut || g.Hands[i].Status == StatusDealer {
			continue
		}
		h := g.Hands[i]
		playerTotal, _ := Score(h.Cards)
		playerBJ := h.Status == StatusBlackjack

		var delta int
		var reason string
		switch {
		case h.Status == StatusBust:
			delta, reason = -h.Bet, fmt.Sprintf("爆牌（%d 点）", playerTotal)
		case playerBJ && dealerBJ:
			delta, reason = 0, "双方黑杰克，平局"
		case playerBJ:
			delta, reason = h.Bet*blackjackNum/blackjackDen, "黑杰克，赔 3:2"
		case dealerBJ:
			delta, reason = -h.Bet, "庄家黑杰克"
		case dealerBust:
			delta, reason = h.Bet, fmt.Sprintf("庄家爆牌（%d 点）", dealerTotal)
		case playerTotal > dealerTotal:
			delta, reason = h.Bet, fmt.Sprintf("%d 点大过庄家 %d 点", playerTotal, dealerTotal)
		case playerTotal == dealerTotal:
			delta, reason = 0, fmt.Sprintf("%d 点打平", playerTotal)
		default:
			delta, reason = -h.Bet, fmt.Sprintf("%d 点小于庄家 %d 点", playerTotal, dealerTotal)
		}

		g.Chips[i] += h.Bet + delta
		g.results = append(g.results, Result{Seat: i, Delta: delta, Chips: g.Chips[i], Reason: reason})
		sumDelta += delta
	}

	// 轮庄：庄家和闲家对赌，筹码在玩家之间流动（总额不变）
	if g.DealerSeat >= 0 {
		g.DealerDelta = -sumDelta
		g.Chips[g.DealerSeat] += g.DealerDelta
	}

	g.Phase = PhaseSettle
	g.LastEvent = "本局结束，输入 next 开下一轮"
}

// NextRound 进入下一轮：轮庄、清算出局，整桌只剩一个人时结束。
func (g *Game) NextRound() []Event {
	if g.Phase == PhaseOver {
		return []Event{{Seat: SeatSystem, Text: "整桌已经结束了，房主 restart 可重开"}}
	}
	g.Round++
	events := g.beginRound(false)
	if g.Phase == PhaseOver {
		return events
	}
	return events
}

func (g *Game) draw() Card {
	if len(g.shoe) == 0 { // 单副牌理论上够用，兜底再洗一副
		g.shoe = NewShoe(g.rng)
	}
	c := g.shoe[0]
	g.shoe = g.shoe[1:]
	return c
}

// --- 视图 ---

// SeatView 是公共视图里的一个座位。
type SeatView struct {
	Seat   int    `json:"seat"`
	Bet    int    `json:"bet"`
	Status Status `json:"status"`
	Cards  []Card `json:"cards"`
	Score  int    `json:"score"`
	Soft   bool   `json:"soft"`
	Chips  int    `json:"chips"`
	// MaxBet 是这一轮还能押多少（0 表示不用下注或下不了注）
	MaxBet int `json:"maxBet"`
}

// DealerView 是庄家的公开信息。结算前 Hidden > 0 且**不含任何暗牌牌面**。
type DealerView struct {
	Up      []Card `json:"up"`
	Hidden  int    `json:"hidden"`
	Score   int    `json:"score"`
	Soft    bool   `json:"soft"`
	Final   bool   `json:"final"`
	IsSeat  bool   `json:"isSeat"`  // 是否由玩家坐庄
	Seat    int    `json:"seat"`    // 坐庄的座位（IsSeat 时有效）
	Delta   int    `json:"delta"`   // 坐庄方的净输赢（结算后）
	NeedBet int    `json:"needBet"` // 坐庄门槛（筹码低于这个数就没资格坐庄）
}

// PublicView 广播给所有人。除了庄家暗牌的张数，没有任何隐藏信息。
type PublicView struct {
	Round     int        `json:"round"`
	Phase     Phase      `json:"phase"`
	Turn      int        `json:"turn"`
	Mode      DealerMode `json:"mode"`
	Dealer    DealerView `json:"dealer"`
	Seats     []SeatView `json:"seats"`
	Results   []Result   `json:"results,omitempty"`
	Over      bool       `json:"over"`
	Winner    int        `json:"winner"`
	LastEvent string     `json:"lastEvent"`
}

func (g *Game) PublicView() PublicView {
	v := PublicView{
		Round:     g.Round,
		Phase:     g.Phase,
		Turn:      g.Turn,
		Mode:      g.Mode,
		Results:   g.results,
		Over:      g.Phase == PhaseOver,
		Winner:    g.Winner,
		LastEvent: g.LastEvent,
	}
	if len(g.Dealer) > 0 {
		if g.hidden {
			up := []Card{g.Dealer[0]}
			v.Dealer.Up = up
			v.Dealer.Hidden = len(g.Dealer) - 1
			v.Dealer.Score, v.Dealer.Soft = Score(up)
		} else {
			v.Dealer.Up = append([]Card(nil), g.Dealer...)
			v.Dealer.Final = true
			v.Dealer.Score, v.Dealer.Soft = Score(g.Dealer)
		}
	}
	if g.DealerSeat >= 0 {
		v.Dealer.IsSeat = true
		v.Dealer.Seat = g.DealerSeat
		v.Dealer.Delta = g.DealerDelta
		v.Dealer.NeedBet = dealerNeed(g.farmerCount())
	}

	v.Seats = make([]SeatView, 0, len(g.Hands))
	for i, h := range g.Hands {
		sv := SeatView{
			Seat:   i,
			Bet:    h.Bet,
			Status: h.Status,
			Chips:  g.Chips[i],
		}
		// 坐庄座位的牌只从 Dealer 走，避免暗牌从座位视图漏出去
		if i == g.DealerSeat {
			sv.Bet = 0
		} else {
			sv.Cards = append([]Card(nil), h.Cards...)
			sv.Score, sv.Soft = Score(h.Cards)
			if h.Status == StatusIdle {
				sv.MaxBet = g.MaxBet(i)
			}
		}
		v.Seats = append(v.Seats, sv)
	}
	return v
}
