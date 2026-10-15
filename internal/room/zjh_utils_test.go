package room

import (
	"testing"

	"intranet-play/internal/zjh"
)

func TestParseZjhCmd(t *testing.T) {
	cases := []struct {
		line   string
		kind   string
		amount int
		target int
		text   string
	}{
		{line: "look", kind: "look"},
		{line: "看", kind: "look"},
		{line: "看牌", kind: "look"},
		{line: "call", kind: "call"},
		{line: "跟", kind: "call"},
		{line: "跟注", kind: "call"},
		{line: "fold", kind: "fold"},
		{line: "弃", kind: "fold"},
		{line: "弃牌", kind: "fold"},
		{line: "raise 6", kind: "raise", amount: 6},
		{line: "加 8", kind: "raise", amount: 8},
		{line: "加注 20", kind: "raise", amount: 20},
		{line: "compare 1", kind: "compare", target: 1},
		{line: "比 0", kind: "compare", target: 0},
		{line: "比牌 2", kind: "compare", target: 2},
		{line: "next", kind: "next"},
		{line: "下一局", kind: "next"},
		{line: "start", kind: "start"},
		{line: "restart", kind: "restart"},
		{line: "help", kind: "help"},
		{line: "leave", kind: "leave"},
		{line: "quit", kind: "leave"},
		{line: "chat 闷到底", kind: "chat", text: "闷到底"},
		{line: "say hi", kind: "chat", text: "hi"},
		{line: "", kind: "none"},
		{line: "   ", kind: "none"},

		// 失败路径
		{line: "raise", kind: "bad"},
		{line: "raise abc", kind: "bad"},
		{line: "raise 0", kind: "bad"},
		{line: "raise -3", kind: "bad"},
		{line: "compare", kind: "bad"},
		{line: "compare x", kind: "bad"},
		{line: "chat", kind: "bad"},
		{line: "whatever", kind: "bad"},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := parseZjhCmd(tc.line)
			if got.kind != tc.kind {
				t.Fatalf("parseZjhCmd(%q).kind = %q, 期望 %q", tc.line, got.kind, tc.kind)
			}
			if got.amount != tc.amount {
				t.Fatalf("parseZjhCmd(%q).amount = %d, 期望 %d", tc.line, got.amount, tc.amount)
			}
			if got.target != tc.target {
				t.Fatalf("parseZjhCmd(%q).target = %d, 期望 %d", tc.line, got.target, tc.target)
			}
			if tc.text != "" && got.text != tc.text {
				t.Fatalf("parseZjhCmd(%q).text = %q, 期望 %q", tc.line, got.text, tc.text)
			}
		})
	}
}

func TestZjhActionFromCmd(t *testing.T) {
	cases := []struct {
		line string
		kind zjh.ActionKind
	}{
		{line: "look", kind: zjh.ActLook},
		{line: "call", kind: zjh.ActCall},
		{line: "raise 6", kind: zjh.ActRaise},
		{line: "fold", kind: zjh.ActFold},
		{line: "compare 1", kind: zjh.ActCompare},
	}
	for _, tc := range cases {
		act, err := zjhActionFromCmd(parseZjhCmd(tc.line))
		if err != nil {
			t.Fatalf("%q 转换失败: %v", tc.line, err)
		}
		if act.Kind != tc.kind {
			t.Fatalf("%q 动作 = %s, 期望 %s", tc.line, act.Kind, tc.kind)
		}
	}
	if act, _ := zjhActionFromCmd(parseZjhCmd("raise 6")); act.Amount != 6 {
		t.Fatalf("加注金额丢失: %+v", act)
	}
	if act, _ := zjhActionFromCmd(parseZjhCmd("compare 1")); act.Target != 1 {
		t.Fatalf("比牌目标丢失: %+v", act)
	}
	// next / start 不是对局动作
	if _, err := zjhActionFromCmd(parseZjhCmd("next")); err == nil {
		t.Fatal("next 不该被当成对局动作")
	}
	if _, err := zjhActionFromCmd(parseZjhCmd("start")); err == nil {
		t.Fatal("start 不该被当成对局动作")
	}
}

// TestNewZjhHostRoomWiring 锁住接线细节：房名是端口、座位 0 的名字是玩家名，
// 而且没开局时 Turn 必须是 -1（零值 0 会让界面把座位 0 标成当前行动方）。
func TestNewZjhHostRoomWiring(t *testing.T) {
	r := newZjhHostRoom("9988", "Alice", 100, 2, 30, 8, 4)

	if r.name != "9988" {
		t.Fatalf("房名 = %q, 期望端口号 9988", r.name)
	}
	if r.startChips != 100 || r.ante != 2 || r.maxStake != 30 || r.maxRounds != 8 || r.maxSeats != 4 {
		t.Fatalf("规则参数没透传: chips=%d ante=%d maxStake=%d maxRounds=%d maxSeats=%d",
			r.startChips, r.ante, r.maxStake, r.maxRounds, r.maxSeats)
	}
	if len(r.seats) != 1 || r.seats[0].name != "Alice" || !r.seats[0].isHost || !r.seats[0].joined {
		t.Fatalf("房主座位不对: %+v", r.seats[0])
	}

	f := r.frame()
	if f.Started {
		t.Fatal("没开局时 Started 应当为 false")
	}
	if f.Turn != -1 {
		t.Fatalf("没开局时 Turn = %d, 期望 -1", f.Turn)
	}
	if f.You != 0 || f.Room != "9988" || len(f.Seats) != 1 || f.Seats[0].Name != "Alice" {
		t.Fatalf("大厅帧不对: you=%d room=%q seats=%+v", f.You, f.Room, f.Seats)
	}
	if f.Looked || len(f.Cards) != 0 {
		t.Fatalf("没开局时不该有牌: %+v", f.Cards)
	}
	// 参数缺省时的兜底
	r2 := newZjhRoom("x", 0, 0, 0, 0, 0)
	if r2.startChips <= 0 || r2.ante <= 0 || r2.maxStake <= 0 || r2.maxRounds <= 0 || r2.maxSeats <= 0 {
		t.Fatalf("缺省参数应当被兜底: %+v", r2)
	}
}
