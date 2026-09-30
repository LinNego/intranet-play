package room

import (
	"testing"

	"intranet-play/internal/ddz"
)

func TestParseDdzCmd(t *testing.T) {
	cases := []struct {
		line string
		kind string
		nums []int
		text string
	}{
		{line: "play 1 2 5", kind: "play", nums: []int{1, 2, 5}},
		{line: "play 1-3", kind: "play", nums: []int{1, 2, 3}},
		{line: "play 2 2", kind: "play", nums: []int{2, 2}},
		{line: "PLAY 7", kind: "play", nums: []int{7}},
		{line: "出牌 4", kind: "play", nums: []int{4}},
		{line: "bid 0", kind: "bid", nums: []int{0}},
		{line: "bid 3", kind: "bid", nums: []int{3}},
		{line: "叫 2", kind: "bid", nums: []int{2}},
		{line: "pass", kind: "pass"},
		{line: "不要", kind: "pass"},
		{line: "start", kind: "start"},
		{line: "next", kind: "next"},
		{line: "ready", kind: "ready"},
		{line: "help", kind: "help"},
		{line: "leave", kind: "leave"},
		{line: "quit", kind: "leave"},
		{line: "chat 你好 世界", kind: "chat", text: "你好 世界"},
		{line: "say hi", kind: "chat", text: "hi"},
		{line: "", kind: "none"},
		{line: "   ", kind: "none"},

		// 失败路径
		{line: "play", kind: "bad"},
		{line: "play a", kind: "bad"},
		{line: "play 3-1", kind: "bad"},
		{line: "play 1-99", kind: "bad"},
		{line: "bid 4", kind: "bad"},
		{line: "bid", kind: "bad"},
		{line: "chat", kind: "bad"},
		{line: "whatever", kind: "bad"},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := parseDdzCmd(tc.line)
			if got.kind != tc.kind {
				t.Fatalf("parseDdzCmd(%q).kind = %q, 期望 %q", tc.line, got.kind, tc.kind)
			}
			if len(got.nums) != len(tc.nums) {
				t.Fatalf("parseDdzCmd(%q).nums = %v, 期望 %v", tc.line, got.nums, tc.nums)
			}
			for i := range tc.nums {
				if got.nums[i] != tc.nums[i] {
					t.Fatalf("parseDdzCmd(%q).nums = %v, 期望 %v", tc.line, got.nums, tc.nums)
				}
			}
			if tc.text != "" && got.text != tc.text {
				t.Fatalf("parseDdzCmd(%q).text = %q, 期望 %q", tc.line, got.text, tc.text)
			}
		})
	}
}

func TestHandIndicesToIDs(t *testing.T) {
	hand := []ddz.Card{
		{ID: 50, Rank: ddz.Rank2, Suit: ddz.SuitSpade},
		{ID: 20, Rank: ddz.RankA, Suit: ddz.SuitHeart},
		{ID: 7, Rank: ddz.Rank3, Suit: ddz.SuitClub},
	}

	ids, err := handIndicesToIDs(hand, []int{1, 3})
	if err != nil {
		t.Fatalf("编号换算失败: %v", err)
	}
	if len(ids) != 2 || ids[0] != 50 || ids[1] != 7 {
		t.Fatalf("编号换算结果 = %v, 期望 [50 7]", ids)
	}

	// 重复编号去重
	ids, err = handIndicesToIDs(hand, []int{2, 2})
	if err != nil || len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("重复编号应当去重: ids=%v err=%v", ids, err)
	}

	// 越界与空手牌
	if _, err := handIndicesToIDs(hand, []int{4}); err == nil {
		t.Fatal("编号越界应当报错")
	}
	if _, err := handIndicesToIDs(hand, []int{0}); err == nil {
		t.Fatal("编号 0 应当报错")
	}
	if _, err := handIndicesToIDs(nil, []int{1}); err == nil {
		t.Fatal("手牌为空应当报错")
	}
}

func TestActionFromCmd(t *testing.T) {
	hand := []ddz.Card{{ID: 11, Rank: ddz.Rank9, Suit: ddz.SuitSpade}}

	act, err := actionFromCmd(parseDdzCmd("play 1"), hand)
	if err != nil || act.Kind != ddz.ActPlay || len(act.Cards) != 1 || act.Cards[0] != 11 {
		t.Fatalf("play 动作不对: %+v err=%v", act, err)
	}
	act, err = actionFromCmd(parseDdzCmd("bid 2"), hand)
	if err != nil || act.Kind != ddz.ActBid || act.Bid != 2 {
		t.Fatalf("bid 动作不对: %+v err=%v", act, err)
	}
	act, err = actionFromCmd(parseDdzCmd("pass"), hand)
	if err != nil || act.Kind != ddz.ActPass {
		t.Fatalf("pass 动作不对: %+v err=%v", act, err)
	}
	// 编号越界在转换成动作时就该被拦住，不浪费一次网络往返
	if _, err := actionFromCmd(parseDdzCmd("play 9"), hand); err == nil {
		t.Fatal("越界编号应当报错")
	}
}

// TestNewDdzHostRoomWiring 锁住两个真在终端里露过馅的接线细节：
// 房名是端口号、座位 0 的名字是玩家名（曾经把端口当成了玩家名）；
// 没开局时地主必须是 -1，否则界面会把座位 0 标成地主。
func TestNewDdzHostRoomWiring(t *testing.T) {
	r := newDdzHostRoom("9988", "Alice", 2, 10)

	if r.name != "9988" {
		t.Fatalf("房名 = %q, 期望端口号 9988", r.name)
	}
	if got := r.seats[0].name; got != "Alice" {
		t.Fatalf("房主座位名字 = %q, 期望 Alice", got)
	}
	if !r.seats[0].isHost || !r.seats[0].joined || !r.seats[0].online {
		t.Fatalf("房主座位状态不对: %+v", r.seats[0])
	}
	if r.base != 2 || r.rounds != 10 {
		t.Fatalf("底分/局数没有透传: base=%d rounds=%d", r.base, r.rounds)
	}

	f := r.frame()
	if f.Started {
		t.Fatal("没开局时 Started 应当是 false")
	}
	if f.Landlord != -1 || f.Winner != -1 {
		t.Fatalf("没开局时不该有地主/赢家: landlord=%d winner=%d", f.Landlord, f.Winner)
	}
	if f.You != 0 || f.Room != "9988" || f.Seats[0].Name != "Alice" {
		t.Fatalf("大厅帧不对: you=%d room=%q seat0=%q", f.You, f.Room, f.Seats[0].Name)
	}
	if f.Hand != nil {
		t.Fatalf("没开局时不该有手牌: %v", f.Hand)
	}
}
