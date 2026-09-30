package protocol

import "intranet-play/internal/ddz"

// 斗地主专用消息类型。与五子棋的消息并存，互不影响：
// 未知 type 一律忽略，因此旧版本进程收到 DDZ_* 不会崩。
const (
	TypeDdzLobby    MsgType = "DDZ_LOBBY"     // Host → 全员：座位表与准备状态
	TypeDdzReady    MsgType = "DDZ_READY"     // Client → Host：我准备好了
	TypeDdzStart    MsgType = "DDZ_START"     // Host → 全员：开打
	TypeDdzPublic   MsgType = "DDZ_PUBLIC"    // Host → 全员：公共状态（只含手牌张数）
	TypeDdzHand     MsgType = "DDZ_HAND"      // Host → 单人：手牌明文（隐私红线）
	TypeDdzAction   MsgType = "DDZ_ACTION"    // Client → Host：叫分/出牌/不要
	TypeDdzEvent    MsgType = "DDZ_EVENT"     // Host → 全员：一条对局日志
	TypeDdzRoundEnd MsgType = "DDZ_ROUND_END" // Host → 全员：本局结算（含三家揭牌）
	TypeDdzMatchEnd MsgType = "DDZ_MATCH_END" // Host → 全员：整场结束与总排名
	TypeDdzNext     MsgType = "DDZ_NEXT"      // Client → Host：请求开下一局
)

// DdzSeatPayload 是座位表里的一行。
type DdzSeatPayload struct {
	Seat   int    `json:"seat"`
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Online bool   `json:"online"`
	IsHost bool   `json:"isHost"`
}

// DdzLobbyPayload 只在加入时单播给本人，因此可以带 You。
type DdzLobbyPayload struct {
	You    int              `json:"you"`
	Seats  []DdzSeatPayload `json:"seats"`
	Base   int              `json:"base"`
	Rounds int              `json:"rounds"`
}

// DdzStartPayload 通知开局。
type DdzStartPayload struct {
	Round  int `json:"round"`
	Rounds int `json:"rounds"`
	Base   int `json:"base"`
}

// DdzPublicPayload 广播用，绝不能包含任何手牌内容。
type DdzPublicPayload struct {
	View   ddz.PublicView   `json:"view"`
	Seats  []DdzSeatPayload `json:"seats"`
	Rounds int              `json:"rounds"`
}

// DdzHandPayload 只发给本人。
type DdzHandPayload struct {
	View ddz.PrivateView `json:"view"`
}

// DdzActionPayload 是上行动作，牌只用 ID 表示。
type DdzActionPayload struct {
	Action ddz.Action `json:"action"`
}

// DdzEventPayload 是一条对局日志，Text 里已含动作描述，Seat 用于渲染玩家名。
type DdzEventPayload struct {
	Seat int    `json:"seat"`
	Text string `json:"text"`
}

// DdzRoundEndPayload 在本局结束时下发，此时三家手牌都已公开。
type DdzRoundEndPayload struct {
	View   ddz.PublicView        `json:"view"`
	Reveal [ddz.Seats][]ddz.Card `json:"reveal"`
}

// DdzMatchEndPayload 是整场结算。
type DdzMatchEndPayload struct {
	Seats  []DdzSeatPayload `json:"seats"`
	Scores []int            `json:"scores"`
	Winner int              `json:"winner"`
}
