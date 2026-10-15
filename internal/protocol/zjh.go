package protocol

import "intranet-play/internal/zjh"

// 炸金花专用消息类型。与其它游戏的消息并存，未知 type 一律忽略。
//
// 本作有两条隐私红线：
//  1. ZJH_HAND 在玩家**没看牌之前 Cards 必须为空**——牌不能提前下发，
//     否则改个客户端就能偷看牌
//  2. ZJH_PEEK（比牌的牌面）**只发给比牌的两个人**，其他人只看到结果
const (
	TypeZjhLobby  MsgType = "ZJH_LOBBY"  // Host → 单人：座位表与规则参数（带 You）
	TypeZjhStart  MsgType = "ZJH_START"  // Host → 全员：开局
	TypeZjhPublic MsgType = "ZJH_PUBLIC" // Host → 全员：公共牌桌状态（无任何牌面）
	TypeZjhHand   MsgType = "ZJH_HAND"   // Host → 单人：自己的牌（没看牌时为空）
	TypeZjhPeek   MsgType = "ZJH_PEEK"   // Host → 比牌的两人：双方牌面
	TypeZjhAction MsgType = "ZJH_ACTION" // Client → Host：看牌/跟注/加注/弃牌/比牌
	TypeZjhEvent  MsgType = "ZJH_EVENT"  // Host → 全员：一条牌桌日志
	TypeZjhNext   MsgType = "ZJH_NEXT"   // Client → Host：请求下一局
)

// ZjhSeatPayload 是座位表里的一行。
type ZjhSeatPayload struct {
	Seat   int    `json:"seat"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	IsHost bool   `json:"isHost"`
}

// ZjhLobbyPayload 只在加入时单播给本人，因此可以带 You。
type ZjhLobbyPayload struct {
	You       int              `json:"you"`
	Seats     []ZjhSeatPayload `json:"seats"`
	Max       int              `json:"max"`
	Chips     int              `json:"chips"`
	Ante      int              `json:"ante"`
	MaxStake  int              `json:"maxStake"`
	MaxRounds int              `json:"maxRounds"`
}

// ZjhStartPayload 通知开局或重开。
type ZjhStartPayload struct {
	Round int `json:"round"`
	Ante  int `json:"ante"`
	Seats int `json:"seats"`
}

// ZjhPublicPayload 广播用：奖池、注额、各家状态，**没有任何牌面**。
type ZjhPublicPayload struct {
	View  zjh.PublicView   `json:"view"`
	Seats []ZjhSeatPayload `json:"seats"`
}

// ZjhHandPayload 只发给本人；没看牌时 View.Cards 为空。
type ZjhHandPayload struct {
	View zjh.PrivateView `json:"view"`
}

// ZjhPeekPayload 一次比牌的牌面，只发给比牌的两个人。
type ZjhPeekPayload struct {
	Peek zjh.Peek `json:"peek"`
}

// ZjhActionPayload 是上行动作。
type ZjhActionPayload struct {
	Action zjh.Action `json:"action"`
}

// ZjhEventPayload 是一条牌桌日志。
type ZjhEventPayload struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}
