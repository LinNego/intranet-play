package bj

import (
	"fmt"
	"math/rand"
)

// SeatSystem 用于不属于任何玩家的系统事件。
const SeatSystem = -1

// Phase 是对局阶段。庄家补牌是 PLAYING 结束时的一次自动步骤，不单独占一个交互阶段。
type Phase string

const (
	PhaseBetting Phase = "BETTING" // 下注
	PhasePlaying Phase = "PLAYING" // 闲家依次要牌/停牌
	PhaseSettle  Phase = "SETTLE"  // 已结算，等 next 开下一轮
)

// Status 是单个闲家的状态。
type Status string

const (
	StatusIdle      Status = "IDLE"      // 还没下注
	StatusBet       Status = "BET"       // 已下注，等其他人下注
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

// Hand 是一个闲家的手牌与状态。
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
type Game struct {
	Hands      []Hand
	Dealer     []Card
	Phase      Phase
	Turn       int
	Chips      []int
	Round      int
	LastEvent  string
	startChips int
	rng        *rand.Rand
	shoe       []Card
	hidden     bool // 庄家暗牌是否还盖着
	results    []Result
}

// NewGame 建一局 21 点：所有座位都是闲家，庄家由服务端按固定规则打。
func NewGame(rng *rand.Rand, seats, chips int) *Game {
	if seats < 1 {
		seats = 1
	}
	if chips <= 0 {
		chips = 100
	}
	g := &Game{
		rng:        rng,
		startChips: chips,
		Phase:      PhaseBetting,
		Round:      1,
		Hands:      make([]Hand, seats),
		Chips:      make([]int, seats),
	}
	for i := range g.Chips {
		g.Chips[i] = chips
	}
	for i := range g.Hands {
		g.Hands[i].Status = StatusIdle
	}
	g.LastEvent = "下注阶段：每人 bet N"
	return g
}

// Apply 是唯一的推进入口。
func (g *Game) Apply(seat int, act Action) ([]Event, error) {
	if seat < 0 || seat >= len(g.Hands) {
		return nil, fmt.Errorf("座位号非法: %d", seat)
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
	if h.Status != StatusIdle {
		return nil, fmt.Errorf("你已经下过注了")
	}
	if act.Amount <= 0 {
		return nil, fmt.Errorf("注额必须大于 0")
	}
	if act.Amount > g.Chips[seat] {
		return nil, fmt.Errorf("筹码不够：你现在有 %d", g.Chips[seat])
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

func (g *Game) allBet() bool {
	for i := range g.Hands {
		if g.Hands[i].Status != StatusBet {
			return false
		}
	}
	return true
}

// deal 发牌：每人两张明牌，庄家一明一暗（暗牌只是不下发，牌面仍在服务端）。
func (g *Game) deal() []Event {
	g.shoe = NewShoe(g.rng)
	g.Dealer = nil
	g.hidden = true
	for i := range g.Hands {
		g.Hands[i].Cards = nil
		g.Hands[i].Status = StatusWaiting
	}
	for round := 0; round < 2; round++ {
		for i := range g.Hands {
			g.Hands[i].Cards = append(g.Hands[i].Cards, g.draw())
		}
		g.Dealer = append(g.Dealer, g.draw())
	}

	events := []Event{{Seat: SeatSystem, Text: "发牌完成"}}
	for i := range g.Hands {
		if IsBlackjack(g.Hands[i].Cards) {
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
	for _, h := range g.Hands {
		if h.Status != StatusBust {
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

// settle 逐个闲家与庄家比大小并更新筹码。押注时已扣过筹码，这里退还本金 + 净输赢。
func (g *Game) settle() {
	dealerTotal, _ := Score(g.Dealer)
	dealerBJ := IsBlackjack(g.Dealer)
	dealerBust := dealerTotal > Target

	g.results = make([]Result, len(g.Hands))
	for i, h := range g.Hands {
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
		g.results[i] = Result{Seat: i, Delta: delta, Chips: g.Chips[i], Reason: reason}
	}

	g.Phase = PhaseSettle
	g.LastEvent = "本局结束，输入 next 开下一轮"
}

// NextRound 进入下一轮下注。筹码输光的座位自动补满。
func (g *Game) NextRound() []Event {
	g.Round++
	g.Phase = PhaseBetting
	g.Turn = 0
	g.Dealer = nil
	g.hidden = false
	g.results = nil

	events := make([]Event, 0, len(g.Hands)+1)
	for i := range g.Hands {
		g.Hands[i] = Hand{Status: StatusIdle}
		if g.Chips[i] <= 0 {
			g.Chips[i] = g.startChips
			events = append(events, Event{Seat: i, Text: fmt.Sprintf("筹码输光，自动补满 %d", g.startChips)})
		}
	}
	events = append(events, Event{Seat: SeatSystem, Text: fmt.Sprintf("第 %d 轮开始，请下注", g.Round)})
	g.LastEvent = fmt.Sprintf("第 %d 轮：下注阶段", g.Round)
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

// SeatView 是公共视图里的一个闲家。
type SeatView struct {
	Seat   int    `json:"seat"`
	Bet    int    `json:"bet"`
	Status Status `json:"status"`
	Cards  []Card `json:"cards"`
	Score  int    `json:"score"`
	Soft   bool   `json:"soft"`
	Chips  int    `json:"chips"`
}

// DealerView 是庄家的公开信息。结算前 Hidden > 0 且**不含任何暗牌牌面**。
type DealerView struct {
	Up     []Card `json:"up"`
	Hidden int    `json:"hidden"`
	Score  int    `json:"score"`
	Soft   bool   `json:"soft"`
	Final  bool   `json:"final"`
}

// PublicView 广播给所有人。除了庄家暗牌的张数，没有任何隐藏信息。
type PublicView struct {
	Round     int        `json:"round"`
	Phase     Phase      `json:"phase"`
	Turn      int        `json:"turn"`
	Dealer    DealerView `json:"dealer"`
	Seats     []SeatView `json:"seats"`
	Results   []Result   `json:"results,omitempty"`
	LastEvent string     `json:"lastEvent"`
}

func (g *Game) PublicView() PublicView {
	v := PublicView{
		Round:     g.Round,
		Phase:     g.Phase,
		Turn:      g.Turn,
		Results:   g.results,
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
	v.Seats = make([]SeatView, 0, len(g.Hands))
	for i, h := range g.Hands {
		sv := SeatView{
			Seat:   i,
			Bet:    h.Bet,
			Status: h.Status,
			Cards:  append([]Card(nil), h.Cards...),
			Chips:  g.Chips[i],
		}
		sv.Score, sv.Soft = Score(h.Cards)
		v.Seats = append(v.Seats, sv)
	}
	return v
}
