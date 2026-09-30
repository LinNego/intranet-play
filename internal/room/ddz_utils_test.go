package room

import (
	"strings"
	"testing"

	"intranet-play/internal/ddz"
)

func TestParseDdzCmd(t *testing.T) {
	cases := []struct {
		line  string
		kind  string
		nums  []int
		ranks []ddz.Rank
		text  string
	}{
		// 按牌面出牌
		{line: "play 55", kind: "play", ranks: []ddz.Rank{ddz.Rank5, ddz.Rank5}},
		{line: "play 5556", kind: "play", ranks: []ddz.Rank{ddz.Rank5, ddz.Rank5, ddz.Rank5, ddz.Rank6}},
		{line: "play 34567", kind: "play", ranks: []ddz.Rank{ddz.Rank3, ddz.Rank4, ddz.Rank5, ddz.Rank6, ddz.Rank7}},
		{line: "play wW", kind: "play", ranks: []ddz.Rank{ddz.RankJokerSmall, ddz.RankJokerBig}},
		{line: "play T J Q K A", kind: "play", ranks: []ddz.Rank{ddz.Rank10, ddz.RankJ, ddz.RankQ, ddz.RankK, ddz.RankA}},
		{line: "play 10 J Q K A", kind: "play", ranks: []ddz.Rank{ddz.Rank10, ddz.RankJ, ddz.RankQ, ddz.RankK, ddz.RankA}},
		{line: "PLAY 99", kind: "play", ranks: []ddz.Rank{ddz.Rank9, ddz.Rank9}},
		{line: "出 5", kind: "play", ranks: []ddz.Rank{ddz.Rank5}},
		{line: "出牌 5555", kind: "play", ranks: []ddz.Rank{ddz.Rank5, ddz.Rank5, ddz.Rank5, ddz.Rank5}},

		// 按手牌编号出牌（必须带 #）
		{line: "play #1 #2 #5", kind: "play", nums: []int{1, 2, 5}},
		{line: "play #1-#3", kind: "play", nums: []int{1, 2, 3}},
		{line: "play #2 #2", kind: "play", nums: []int{2, 2}},

		{line: "bid 0", kind: "bid", nums: []int{0}},
		{line: "bid 3", kind: "bid", nums: []int{3}},
		{line: "叫 2", kind: "bid", nums: []int{2}},
		{line: "pass", kind: "pass"},
		{line: "不要", kind: "pass"},
		{line: "start", kind: "start"},
		{line: "next", kind: "next"},
		{line: "ready", kind: "ready"},
		{line: "hint", kind: "hint"},
		{line: "提示", kind: "hint"},
		{line: "help", kind: "help"},
		{line: "leave", kind: "leave"},
		{line: "quit", kind: "leave"},
		{line: "chat 你好 世界", kind: "chat", text: "你好 世界"},
		{line: "say hi", kind: "chat", text: "hi"},
		{line: "", kind: "none"},
		{line: "   ", kind: "none"},

		// 失败路径
		{line: "play", kind: "bad"},
		{line: "play X", kind: "bad"},     // 认不出的牌面
		{line: "play 1", kind: "bad"},     // 旧编号写法已废弃，必须带 #
		{line: "play 1 2 5", kind: "bad"}, // 同上
		{line: "play #3 x", kind: "bad"},  // 两种写法不能混用
		{line: "play #1-#0", kind: "bad"},
		{line: "play #1-#99", kind: "bad"},
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

	// 编号写法
	act, err := actionFromCmd(parseDdzCmd("play #1"), hand)
	if err != nil || act.Kind != ddz.ActPlay || len(act.Cards) != 1 || act.Cards[0] != 11 {
		t.Fatalf("play #1 动作不对: %+v err=%v", act, err)
	}
	// 牌面写法
	act, err = actionFromCmd(parseDdzCmd("play 9"), hand)
	if err != nil || act.Kind != ddz.ActPlay || len(act.Cards) != 1 || act.Cards[0] != 11 {
		t.Fatalf("play 9 动作不对: %+v err=%v", act, err)
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
	if _, err := actionFromCmd(parseDdzCmd("play #9"), hand); err == nil {
		t.Fatal("越界编号应当报错")
	}
}

// TestHandRanksToIDs 验证"按牌面出牌"如何落到具体的牌上。
func TestHandRanksToIDs(t *testing.T) {
	hand := []ddz.Card{
		{ID: 1, Rank: ddz.Rank5, Suit: ddz.SuitSpade},
		{ID: 2, Rank: ddz.Rank5, Suit: ddz.SuitHeart},
		{ID: 3, Rank: ddz.Rank3, Suit: ddz.SuitClub},
		{ID: 4, Rank: ddz.Rank7, Suit: ddz.SuitDiamond},
	}

	ids, err := handRanksToIDs(hand, []ddz.Rank{ddz.Rank5, ddz.Rank5, ddz.Rank7})
	if err != nil {
		t.Fatalf("按牌面取牌失败: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("应当取 3 张，实际 %v", ids)
	}
	// 同点数的牌挑哪几张都一样，只要不重复、且都在手里
	seen := map[int]bool{}
	ranks := map[int]ddz.Rank{}
	for _, c := range hand {
		ranks[c.ID] = c.Rank
	}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("重复取到了 id=%d", id)
		}
		seen[id] = true
	}
	if ranks[ids[0]] != ddz.Rank5 || ranks[ids[1]] != ddz.Rank5 || ranks[ids[2]] != ddz.Rank7 {
		t.Fatalf("取到的点数不对: %v", ids)
	}

	// 手里牌不够时要给出说明张数的错误
	if _, err := handRanksToIDs(hand, []ddz.Rank{ddz.Rank5, ddz.Rank5, ddz.Rank5}); err == nil {
		t.Fatal("只有两张 5 却要出三张，应当报错")
	} else if !strings.Contains(err.Error(), "只有 2 张") {
		t.Fatalf("错误信息应当说明手里有几张: %v", err)
	}
	if _, err := handRanksToIDs(hand, []ddz.Rank{ddz.RankJokerBig}); err == nil {
		t.Fatal("手里没有的牌应当报错")
	}
	if _, err := handRanksToIDs(nil, []ddz.Rank{ddz.Rank3}); err == nil {
		t.Fatal("空手牌应当报错")
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

// TestShowHintsOutputIsUsable 保证 hint 给出的写法不只是给人看的：
// 把它原样喂回命令解析，必须能变成一手合法出牌。
func TestShowHintsOutputIsUsable(t *testing.T) {
	r := newDdzRoom("9988", 1, 10)
	r.you = 0
	r.started = true
	r.hand = mustTestCards(t, "5", "5", "5", "6", "8", "9")
	r.view = emptyView()
	r.view.Phase = ddz.PhasePlaying

	r.showHints()
	if len(r.log) == 0 {
		t.Fatal("showHints 没有写任何日志")
	}
	line := r.log[len(r.log)-1]
	if !strings.HasPrefix(line, "可以出") {
		t.Fatalf("自由出牌权下的提示文案不对: %q", line)
	}

	// 取出提示里的第一种写法（形如 "对子(5) play 55"），喂回命令解析
	at := strings.Index(line, "play ")
	if at < 0 {
		t.Fatalf("提示里没有可直接使用的 play 写法: %q", line)
	}
	fields := strings.Fields(line[at:]) // ["play", "55", "|", ...]
	if len(fields) < 2 {
		t.Fatalf("提示里的写法不完整: %q", line)
	}
	notation := fields[1]

	cmd := parseDdzCmd("play " + notation)
	if cmd.kind != "play" {
		t.Fatalf("hint 给出的写法 %q 解析失败: kind=%s text=%s", notation, cmd.kind, cmd.text)
	}
	act, err := actionFromCmd(cmd, r.hand)
	if err != nil {
		t.Fatalf("hint 给出的写法 %q 无法执行: %v", notation, err)
	}
	if _, _, err := ddz.LegalPlay(r.hand, act.Cards, nil); err != nil {
		t.Fatalf("hint 给出的 %q 不是合法牌型: %v", notation, err)
	}

	// 有上家牌时，提示必须真的能压过去
	upstream := ddz.PlayView{Seat: 2, Kind: ddz.KindSingle, Main: ddz.Rank8, Size: 1, Chain: 1, Label: "单张(8)"}
	upstreamPlay := &ddz.Play{Seat: 2, Combo: upstream.AsCombo()}
	r.view.LastPlay = &upstream
	r.log = nil
	r.showHints()
	if len(r.log) == 0 {
		t.Fatal("有上家牌时 showHints 没有输出")
	}
	line = r.log[len(r.log)-1]
	if !strings.HasPrefix(line, "可以压过") {
		t.Fatalf("有上家牌时的提示文案不对: %q", line)
	}
	at = strings.Index(line, "play ")
	if at < 0 {
		t.Fatalf("提示里没有可直接使用的写法: %q", line)
	}
	notation = strings.Fields(line[at:])[1]
	cmd = parseDdzCmd("play " + notation)
	act, err = actionFromCmd(cmd, r.hand)
	if err != nil {
		t.Fatalf("压牌提示 %q 无法执行: %v", notation, err)
	}
	combo, _, err := ddz.LegalPlay(r.hand, act.Cards, upstreamPlay)
	if err != nil {
		t.Fatalf("压牌提示 %q 不合法: %v", notation, err)
	}
	if !combo.Beats(upstream.AsCombo()) {
		t.Fatalf("压牌提示 %q 其实压不过 %s", notation, upstream.Label)
	}
}
