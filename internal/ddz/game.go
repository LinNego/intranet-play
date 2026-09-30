package ddz

import (
	"fmt"
	"math/rand"
)

// 座位与发牌常量。
const (
	Seats      = 3
	HandSize   = 17
	BottomSize = 3
	maxRedeal  = 5
)

// Phase 是对局阶段。
type Phase string

const (
	PhaseBidding  Phase = "BIDDING"   // 叫分
	PhasePlaying  Phase = "PLAYING"   // 出牌
	PhaseRoundEnd Phase = "ROUND_END" // 本局结束
)

// ActionKind 是玩家动作类型。
type ActionKind string

const (
	ActBid  ActionKind = "BID"  // 叫分，Bid = 0..3
	ActPlay ActionKind = "PLAY" // 出牌，Cards = 手牌 ID
	ActPass ActionKind = "PASS" // 不要
)

// Action 是客户端上行的动作，牌一律用 ID 表示，由 Host 校验归属。
type Action struct {
	Kind  ActionKind `json:"kind"`
	Bid   int        `json:"bid,omitempty"`
	Cards []int      `json:"cards,omitempty"`
}

// SeatSystem 用于不属于任何玩家的系统事件（发牌、流局、自由出牌权回收等）。
const SeatSystem = -1

// Event 是需要广播给所有人的对局消息。
type Event struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}

// Play 是一手已打出的牌（打出的牌是公开信息）。
type Play struct {
	Seat  int    `json:"seat"`
	Cards []Card `json:"cards"`
	Combo Combo  `json:"combo"`
}

// Game 是 Host 权威的对局状态。所有校验都集中在 Apply 内，外部不得直接改状态。
type Game struct {
	Hands       [Seats][]Card
	Bottom      []Card
	Landlord    int // -1 表示未定
	Phase       Phase
	Turn        int
	BidTurn     int
	Bids        [Seats]int
	BidScore    int // 当前最高叫分
	bidLeader   int // 当前最高叫分座位
	bidActed    int // 已表态人数
	bidDone     [Seats]bool
	Multiplier  int
	Base        int
	LastPlay    *Play
	PassSeat    [Seats]bool
	PlayCount   [Seats]int
	Scores      [Seats]int
	RoundScore  [Seats]int // 本局得分变化
	Winner      int        // -1 表示未结束
	LandlordWin bool
	Spring      bool
	AntiSpring  bool
	Round       int
	Redeal      int
	Online      [Seats]bool
	LastEvent   string
	rng         *rand.Rand
	passCount   int
}

// NewGame 洗牌发牌并进入叫分阶段。
// rng 为 nil 时使用默认随机源；注入种子便于复现问题与测试。
func NewGame(rng *rand.Rand, firstBidder int, base int) *Game {
	if base <= 0 {
		base = 1
	}
	g := &Game{
		Base:       base,
		Landlord:   -1,
		Winner:     -1,
		Round:      1,
		rng:        rng,
		Multiplier: 1,
	}
	for i := 0; i < Seats; i++ {
		g.Online[i] = true
	}
	g.deal(firstBidder)
	return g
}

// deal 洗牌发牌并重置本局状态（保留累计积分）。
func (g *Game) deal(firstBidder int) {
	deck := Shuffle(g.rng)
	for i := 0; i < Seats; i++ {
		hand := make([]Card, HandSize)
		copy(hand, deck[i*HandSize:(i+1)*HandSize])
		SortDesc(hand)
		g.Hands[i] = hand
	}
	g.Bottom = CloneCards(deck[Seats*HandSize:])
	SortDesc(g.Bottom)

	g.Landlord = -1
	g.Winner = -1
	g.Phase = PhaseBidding
	g.Bids = [Seats]int{}
	g.BidScore = 0
	g.bidLeader = -1
	g.bidActed = 0
	g.bidDone = [Seats]bool{}
	g.Multiplier = 1
	g.LastPlay = nil
	g.PassSeat = [Seats]bool{}
	g.PlayCount = [Seats]int{}
	g.RoundScore = [Seats]int{}
	g.LandlordWin = false
	g.Spring = false
	g.AntiSpring = false
	g.passCount = 0

	g.BidTurn = ((firstBidder % Seats) + Seats) % Seats
	g.Turn = g.BidTurn
	g.LastEvent = "发牌完成，开始叫分"
}

// NextRound 开下一局：重新发牌，叫分起点轮转，累计积分保留。
func (g *Game) NextRound() {
	g.Round++
	g.Redeal = 0
	g.deal(g.BidTurn + 1)
}

// Apply 是唯一的对局推进入口。返回需要广播的事件。
func (g *Game) Apply(seat int, act Action) ([]Event, error) {
	if seat < 0 || seat >= Seats {
		return nil, fmt.Errorf("座位号非法: %d", seat)
	}
	if !g.Online[seat] {
		return nil, fmt.Errorf("该座位已离线")
	}
	if seat != g.Turn {
		return nil, fmt.Errorf("还没轮到你")
	}
	switch g.Phase {
	case PhaseBidding:
		return g.applyBid(seat, act)
	case PhasePlaying:
		return g.applyPlay(seat, act)
	case PhaseRoundEnd:
		return nil, fmt.Errorf("本局已结束，等待开下一局")
	}
	return nil, fmt.Errorf("当前阶段不接受操作: %s", g.Phase)
}

func (g *Game) applyBid(seat int, act Action) ([]Event, error) {
	if act.Kind != ActBid {
		return nil, fmt.Errorf("叫分阶段只能叫分")
	}
	bid := act.Bid
	if bid < 0 || bid > 3 {
		return nil, fmt.Errorf("叫分只能是 0-3")
	}
	if bid != 0 && bid <= g.BidScore {
		return nil, fmt.Errorf("叫分必须高于当前最高分 %d", g.BidScore)
	}

	g.Bids[seat] = bid
	g.bidActed++
	g.bidDone[seat] = true
	var events []Event
	if bid > 0 {
		g.BidScore = bid
		g.bidLeader = seat
		events = append(events, Event{Seat: seat, Text: fmt.Sprintf("叫 %d 分", bid)})
	} else {
		events = append(events, Event{Seat: seat, Text: "不叫"})
	}

	switch {
	case bid == 3:
		events = append(events, g.setLandlord(seat)...)
	case g.bidActed == Seats:
		if g.bidLeader < 0 {
			if g.Redeal >= maxRedeal {
				g.Phase = PhaseRoundEnd
				g.LastEvent = fmt.Sprintf("连续 %d 次无人叫分，本局流局", g.Redeal+1)
				return append(events, Event{Seat: SeatSystem, Text: g.LastEvent}), nil
			}
			g.Redeal++
			g.deal(g.BidTurn + 1)
			events = append(events, Event{Seat: SeatSystem, Text: fmt.Sprintf("三家都不叫，重新发牌（第 %d 次）", g.Redeal)})
			return events, nil
		}
		events = append(events, g.setLandlord(g.bidLeader)...)
	default:
		g.Turn = next(seat)
		g.BidTurn = g.Turn
	}
	return events, nil
}

// setLandlord 定地主：底牌并入地主手牌并公开。
func (g *Game) setLandlord(seat int) []Event {
	g.Landlord = seat
	hand := append(CloneCards(g.Hands[seat]), CloneCards(g.Bottom)...)
	SortDesc(hand)
	g.Hands[seat] = hand
	g.Phase = PhasePlaying
	g.Turn = seat
	g.passCount = 0
	g.LastPlay = nil
	g.LastEvent = fmt.Sprintf("座位 %d 成为地主（%d 分），底牌已并入", seat, g.BidScore)
	return []Event{
		{Seat: seat, Text: fmt.Sprintf("成为地主，底分 %d，拿到底牌", g.BidScore)},
	}
}

func (g *Game) applyPlay(seat int, act Action) ([]Event, error) {
	switch act.Kind {
	case ActPlay:
		combo, cards, err := LegalPlay(g.Hands[seat], act.Cards, g.LastPlay)
		if err != nil {
			return nil, err
		}
		rest, ok := RemoveByIDs(g.Hands[seat], act.Cards)
		if !ok {
			return nil, fmt.Errorf("牌不在你手里")
		}
		g.Hands[seat] = rest
		g.PlayCount[seat]++
		g.PassSeat = [Seats]bool{}
		g.passCount = 0
		g.LastPlay = &Play{Seat: seat, Cards: cards, Combo: combo}
		if combo.Kind == KindBomb || combo.Kind == KindRocket {
			g.Multiplier *= 2
		}

		events := []Event{{Seat: seat, Text: fmt.Sprintf("打出 %s", combo)}}
		if len(g.Hands[seat]) == 0 {
			events = append(events, g.finish(seat)...)
			return events, nil
		}
		g.Turn = next(seat)
		return events, nil

	case ActPass:
		if g.LastPlay == nil {
			return nil, fmt.Errorf("你拥有自由出牌权，必须出牌")
		}
		g.PassSeat[seat] = true
		g.passCount++
		events := []Event{{Seat: seat, Text: "不要"}}
		if g.passCount >= Seats-1 {
			// 连续两家不要，出牌权回到最后出牌的人，且必须出牌。
			g.LastPlay = nil
			g.passCount = 0
			g.PassSeat = [Seats]bool{}
			events = append(events, Event{Seat: SeatSystem, Text: "两家不要，重新自由出牌"})
		}
		g.Turn = next(seat)
		return events, nil
	}
	return nil, fmt.Errorf("当前阶段不接受该操作: %s", act.Kind)
}

// finish 结算本局：春天判定 + 计分。
func (g *Game) finish(winner int) []Event {
	g.Winner = winner
	g.Phase = PhaseRoundEnd
	g.LandlordWin = winner == g.Landlord

	farmers := make([]int, 0, Seats-1)
	for i := 0; i < Seats; i++ {
		if i != g.Landlord {
			farmers = append(farmers, i)
		}
	}
	if g.LandlordWin {
		zero := true
		for _, f := range farmers {
			if g.PlayCount[f] != 0 {
				zero = false
			}
		}
		if zero {
			g.Spring = true
			g.Multiplier *= 2
		}
	} else if g.PlayCount[g.Landlord] == 1 {
		g.AntiSpring = true
		g.Multiplier *= 2
	}

	unit := g.Base * g.BidScore * g.Multiplier
	if unit <= 0 {
		unit = g.Base
	}
	for i := 0; i < Seats; i++ {
		if i == g.Landlord {
			if g.LandlordWin {
				g.RoundScore[i] = 2 * unit
			} else {
				g.RoundScore[i] = -2 * unit
			}
		} else {
			if g.LandlordWin {
				g.RoundScore[i] = -unit
			} else {
				g.RoundScore[i] = unit
			}
		}
		g.Scores[i] += g.RoundScore[i]
	}

	who := "农民"
	if g.LandlordWin {
		who = "地主"
	}
	text := fmt.Sprintf("%s获胜，底分 %d × 叫分 %d × 倍数 %d", who, g.Base, g.BidScore, g.Multiplier)
	if g.Spring {
		text += "（春天）"
	}
	if g.AntiSpring {
		text += "（反春天）"
	}
	g.LastEvent = text
	return []Event{{Seat: winner, Text: text}}
}

// LegalPlay 校验一手出牌：牌必须都在手里、牌型合法、且能压过 target（target 为 nil 表示自由出牌）。
// 返回识别出的牌型与对应的牌，供 Host 与客户端提示共用。
func LegalPlay(hand []Card, ids []int, target *Play) (Combo, []Card, error) {
	if len(ids) == 0 {
		return Combo{}, nil, fmt.Errorf("没有选择任何牌")
	}
	cards, ok := FindByIDs(hand, ids)
	if !ok {
		return Combo{}, nil, fmt.Errorf("牌不在你手里")
	}
	combo, ok := Classify(cards)
	if !ok {
		return Combo{}, nil, fmt.Errorf("不是合法牌型")
	}
	if target != nil && !combo.Beats(target.Combo) {
		return Combo{}, nil, fmt.Errorf("压不过上家的%s", target.Combo.Kind.Label())
	}
	return combo, cards, nil
}

func next(seat int) int { return (seat + 1) % Seats }

// --- 视图 ---

// PlayView 是公开给所有人的出牌信息。
type PlayView struct {
	Seat  int       `json:"seat"`
	Cards []Card    `json:"cards"`
	Kind  ComboKind `json:"kind"`
	Main  Rank      `json:"main"`
	Size  int       `json:"size"`
	Label string    `json:"label"`
}

// PublicView 广播给所有人。这里只有手牌张数，绝不包含任何手牌内容。
type PublicView struct {
	Round       int         `json:"round"`
	Phase       Phase       `json:"phase"`
	Turn        int         `json:"turn"`
	Landlord    int         `json:"landlord"`
	Bottom      []Card      `json:"bottom"`
	HandCount   [Seats]int  `json:"handCount"`
	Online      [Seats]bool `json:"online"`
	LastPlay    *PlayView   `json:"lastPlay,omitempty"`
	PassSeat    [Seats]bool `json:"passSeat"`
	Bids        [Seats]int  `json:"bids"`
	BidActed    [Seats]bool `json:"bidActed"`
	BidScore    int         `json:"bidScore"`
	Multiplier  int         `json:"multiplier"`
	Scores      [Seats]int  `json:"scores"`
	RoundScore  [Seats]int  `json:"roundScore"`
	PlayCount   [Seats]int  `json:"playCount"`
	Winner      int         `json:"winner"`
	LandlordWin bool        `json:"landlordWin"`
	Spring      bool        `json:"spring"`
	AntiSpring  bool        `json:"antiSpring"`
	LastEvent   string      `json:"lastEvent"`
}

// PrivateView 只单播给本人，包含手牌明文。
type PrivateView struct {
	Seat    int    `json:"seat"`
	Hand    []Card `json:"hand"`
	IsTurn  bool   `json:"isTurn"`
	CanPass bool   `json:"canPass"`
	MinBid  int    `json:"minBid"`
}

func (g *Game) PublicView() PublicView {
	pv := PublicView{
		Round:       g.Round,
		Phase:       g.Phase,
		Turn:        g.Turn,
		Landlord:    g.Landlord,
		Bottom:      CloneCards(g.Bottom),
		Online:      g.Online,
		PassSeat:    g.PassSeat,
		Bids:        g.Bids,
		BidActed:    g.bidDone,
		BidScore:    g.BidScore,
		Multiplier:  g.Multiplier,
		Scores:      g.Scores,
		RoundScore:  g.RoundScore,
		PlayCount:   g.PlayCount,
		Winner:      g.Winner,
		LandlordWin: g.LandlordWin,
		Spring:      g.Spring,
		AntiSpring:  g.AntiSpring,
		LastEvent:   g.LastEvent,
	}
	for i := 0; i < Seats; i++ {
		pv.HandCount[i] = len(g.Hands[i])
	}
	if g.Landlord < 0 {
		pv.Bottom = nil // 底牌在定地主前对所有人保密
	}
	if g.LastPlay != nil {
		pv.LastPlay = &PlayView{
			Seat:  g.LastPlay.Seat,
			Cards: CloneCards(g.LastPlay.Cards),
			Kind:  g.LastPlay.Combo.Kind,
			Main:  g.LastPlay.Combo.Main,
			Size:  g.LastPlay.Combo.Size,
			Label: g.LastPlay.Combo.String(),
		}
	}
	return pv
}

// Reveal 返回三家手牌，只在本局结束后用于结算展示。
func (g *Game) Reveal() [Seats][]Card {
	var out [Seats][]Card
	for i := 0; i < Seats; i++ {
		out[i] = CloneCards(g.Hands[i])
	}
	return out
}

func (g *Game) PrivateView(seat int) PrivateView {
	minBid := g.BidScore + 1
	if minBid > 3 {
		minBid = 3
	}
	if minBid < 1 {
		minBid = 1
	}
	view := PrivateView{
		Seat:    seat,
		IsTurn:  seat == g.Turn && g.Phase != PhaseRoundEnd,
		CanPass: g.LastPlay != nil,
		MinBid:  minBid,
	}
	if seat >= 0 && seat < Seats {
		view.Hand = CloneCards(g.Hands[seat])
	}
	return view
}
