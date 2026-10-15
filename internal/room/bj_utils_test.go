package room

import (
	"testing"

	"intranet-play/internal/bj"
)

func TestParseBjCmd(t *testing.T) {
	cases := []struct {
		line   string
		kind   string
		amount int
		text   string
	}{
		{line: "bet 10", kind: "bet", amount: 10},
		{line: "BET 25", kind: "bet", amount: 25},
		{line: "押 5", kind: "bet", amount: 5},
		{line: "下注 100", kind: "bet", amount: 100},
		{line: "hit", kind: "hit"},
		{line: "要", kind: "hit"},
		{line: "要牌", kind: "hit"},
		{line: "stand", kind: "stand"},
		{line: "停", kind: "stand"},
		{line: "停牌", kind: "stand"},
		{line: "double", kind: "double"},
		{line: "双倍", kind: "double"},
		{line: "next", kind: "next"},
		{line: "下一轮", kind: "next"},
		{line: "start", kind: "start"},
		{line: "restart", kind: "restart"},
		{line: "help", kind: "help"},
		{line: "?", kind: "help"},
		{line: "leave", kind: "leave"},
		{line: "quit", kind: "leave"},
		{line: "chat 手气不错", kind: "chat", text: "手气不错"},
		{line: "say hi", kind: "chat", text: "hi"},
		{line: "", kind: "none"},
		{line: "   ", kind: "none"},

		// 失败路径
		{line: "bet", kind: "bad"},
		{line: "bet abc", kind: "bad"},
		{line: "bet 0", kind: "bad"},
		{line: "bet -5", kind: "bad"},
		{line: "bet 1 2", kind: "bad"},
		{line: "chat", kind: "bad"},
		{line: "whatever", kind: "bad"},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := parseBjCmd(tc.line)
			if got.kind != tc.kind {
				t.Fatalf("parseBjCmd(%q).kind = %q, 期望 %q", tc.line, got.kind, tc.kind)
			}
			if got.amount != tc.amount {
				t.Fatalf("parseBjCmd(%q).amount = %d, 期望 %d", tc.line, got.amount, tc.amount)
			}
			if tc.text != "" && got.text != tc.text {
				t.Fatalf("parseBjCmd(%q).text = %q, 期望 %q", tc.line, got.text, tc.text)
			}
		})
	}
}

func TestBjActionFromCmd(t *testing.T) {
	act, err := bjActionFromCmd(parseBjCmd("bet 15"))
	if err != nil || act.Kind != bj.ActBet || act.Amount != 15 {
		t.Fatalf("bet 动作不对: %+v err=%v", act, err)
	}
	act, err = bjActionFromCmd(parseBjCmd("hit"))
	if err != nil || act.Kind != bj.ActHit {
		t.Fatalf("hit 动作不对: %+v err=%v", act, err)
	}
	act, err = bjActionFromCmd(parseBjCmd("stand"))
	if err != nil || act.Kind != bj.ActStand {
		t.Fatalf("stand 动作不对: %+v err=%v", act, err)
	}
	act, err = bjActionFromCmd(parseBjCmd("double"))
	if err != nil || act.Kind != bj.ActDouble {
		t.Fatalf("double 动作不对: %+v err=%v", act, err)
	}
	// next / start 不是对局动作，不该被当成动作
	if _, err := bjActionFromCmd(parseBjCmd("next")); err == nil {
		t.Fatal("next 不该被当成对局动作")
	}
}

// TestResolveDealerMode auto 的含义：2 人用虚拟庄家（否则每局都有一半人没牌可打），
// 3 人以上轮流坐庄。
func TestResolveDealerMode(t *testing.T) {
	cases := []struct {
		flag  string
		seats int
		want  bj.DealerMode
	}{
		{flag: "auto", seats: 2, want: bj.DealerVirtual},
		{flag: "auto", seats: 3, want: bj.DealerRotate},
		{flag: "auto", seats: 6, want: bj.DealerRotate},
		{flag: "", seats: 3, want: bj.DealerRotate},
		{flag: "rotate", seats: 2, want: bj.DealerRotate},
		{flag: "virtual", seats: 5, want: bj.DealerVirtual},
		{flag: "ROTATE", seats: 2, want: bj.DealerRotate},
		{flag: "轮庄", seats: 2, want: bj.DealerRotate},
		{flag: "虚拟", seats: 5, want: bj.DealerVirtual},
		{flag: "看不懂的", seats: 3, want: bj.DealerRotate}, // 落到 auto
	}
	for _, tc := range cases {
		if got := resolveDealerMode(tc.flag, tc.seats); got != tc.want {
			t.Fatalf("resolveDealerMode(%q, %d) = %s, 期望 %s", tc.flag, tc.seats, got, tc.want)
		}
	}
}

// TestNewBjHostRoomWiring 锁住接线细节：房名是端口、座位 0 的名字是玩家名，
// 而且没开局时 Turn 必须是 -1（零值 0 会让界面把座位 0 标成当前行动方）。
func TestNewBjHostRoomWiring(t *testing.T) {
	r := newBjHostRoom("9988", "Alice", 200, 4, "auto")

	if r.name != "9988" {
		t.Fatalf("房名 = %q, 期望端口号 9988", r.name)
	}
	if r.startChips != 200 || r.maxSeats != 4 {
		t.Fatalf("筹码/人数上限没透传: chips=%d max=%d", r.startChips, r.maxSeats)
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
	if f.Dealer.Hidden != 0 || len(f.Dealer.Up) != 0 {
		t.Fatalf("没开局时不该有庄家牌: %+v", f.Dealer)
	}
}
