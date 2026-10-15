package protocol

import "intranet-play/internal/bj"

// 21 点专用消息类型。与五子棋、斗地主的消息并存，互不影响：
// 未知 type 一律忽略，因此旧版本进程收到 BJ_* 不会崩。
//
// 注意：21 点不需要单播下发手牌——闲家的牌本来就是明牌，唯一要藏的庄家暗牌
// 只要"不下发"就够了，所以这里没有 BJ_HAND 这类单人消息。
const (
	TypeBjLobby  MsgType = "BJ_LOBBY"  // Host → 单人：座位表与筹码（带 You）
	TypeBjStart  MsgType = "BJ_START"  // Host → 全员：开局
	TypeBjPublic MsgType = "BJ_PUBLIC" // Host → 全员：公共牌桌状态（含结算明细）
	TypeBjAction MsgType = "BJ_ACTION" // Client → Host：下注/要牌/停牌/双倍
	TypeBjEvent  MsgType = "BJ_EVENT"  // Host → 全员：一条牌桌日志
	TypeBjNext   MsgType = "BJ_NEXT"   // Client → Host：请求开下一轮
)

// BjSeatPayload 是座位表里的一行。
type BjSeatPayload struct {
	Seat   int    `json:"seat"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	IsHost bool   `json:"isHost"`
}

// BjLobbyPayload 只在加入时单播给本人，因此可以带 You。
type BjLobbyPayload struct {
	You    int             `json:"you"`
	Seats  []BjSeatPayload `json:"seats"`
	Max    int             `json:"max"`
	Chips  int             `json:"chips"`
	Dealer string          `json:"dealer"` // 房主的庄家模式设置：auto / rotate / virtual
}

// BjStartPayload 通知开局或重开。
type BjStartPayload struct {
	Round int    `json:"round"`
	Chips int    `json:"chips"`
	Seats int    `json:"seats"`
	Mode  string `json:"mode"` // VIRTUAL / ROTATE
}

// BjPublicPayload 广播用。庄家暗牌只有张数，绝无牌面。
type BjPublicPayload struct {
	View  bj.PublicView   `json:"view"`
	Seats []BjSeatPayload `json:"seats"`
	Max   int             `json:"max"`
}

// BjActionPayload 是上行动作。
type BjActionPayload struct {
	Action bj.Action `json:"action"`
}

// BjEventPayload 是一条牌桌日志。
type BjEventPayload struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}
