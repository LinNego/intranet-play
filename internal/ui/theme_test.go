package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestThemeHighlightStylesCarryColor 锁住"该高亮的东西真的有颜色"。
//
// 平时 lipgloss 在非终端（比如管道、重定向）下会自动剥掉颜色，这是期望行为；
// 所以这里显式把色深调上去，检查样式本身确实带了颜色码，防止哪天又被改回全灰。
func TestThemeHighlightStylesCarryColor(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(saved)

	cases := []struct {
		name  string
		text  string
		style lipgloss.Style
		code  string
	}{
		{name: "轮到你用绿", text: "state=RUN", style: okStyle, code: "38;5;114"},
		{name: "限制提示用琥珀", text: "只能比牌", style: warnStyle, code: "38;5;179"},
		{name: "被拒/出局用红", text: "操作被拒", style: errStyle, code: "38;5;203"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.style.Render(tc.text); !strings.Contains(got, tc.code) {
				t.Fatalf("%s 没带上颜色码 %s：%q", tc.name, tc.code, got)
			}
		})
	}

	// 状态行：轮到我 → 绿，不是我的回合 → 灰，两者必须能区分
	mine := TurnBullet(true, "state=RUN  turn=local/1")
	other := TurnBullet(false, "state=RUN  turn=peer/2")
	if mine == other {
		t.Fatal("轮到我和等待别人的状态行应当有区别")
	}
	if !strings.Contains(mine, "38;5;114") {
		t.Fatalf("轮到我时状态行应当是绿的: %q", mine)
	}
	if strings.Contains(other, "38;5;114") {
		t.Fatalf("不是我的回合时不该用绿色高亮: %q", other)
	}

	// 日志分类：被拒 → 红，限制 → 琥珀，其余 → 灰
	if got := LogLine("操作被拒: 压不过上家"); !strings.Contains(got, "38;5;203") {
		t.Fatalf("被拒的日志应当标红: %q", got)
	}
	if got := LogLine("筹码不够，只能弃牌"); !strings.Contains(got, "38;5;179") {
		t.Fatalf("限制类提示应当标琥珀: %q", got)
	}
	if got := LogLine("Alice 看牌"); hasSemanticColor(got) {
		t.Fatalf("普通日志不该用语义色（绿/琥珀/红）: %q", got)
	}

	// 抬头第一行（标题）保持默认色，只有方块图标是品牌橙
	// （ANSI256 下橙色会被降采样成 38;5;173，真彩下才是 #D97757，两种都认）
	logo := LogoBlock("nx-sync 0.4  |  peer table", "room=9988", "炸金花")
	if !hasSemanticColor(logo) {
		t.Fatalf("logo 方块应当带品牌橙: %q", logo)
	}
	if !strings.Contains(logo, "nx-sync 0.4") {
		t.Fatalf("标题文字丢了: %q", logo)
	}
}

// TestThemeStaysQuiet 提醒式的检查：整屏里上色的地方应该很少。
// 摸鱼工具的观感靠的是"大部分是灰的"，不是靠到处高亮。
func TestThemeStaysQuiet(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(saved)

	f := zjhBaseFrame(t)
	f.Looked = true
	f.Cards = zjhHand(t, "A♠", "K♥", "7♦")
	f.HandLabel = "散牌"
	f.PeekText = "散牌 A♠ K♥ 7♦ vs 座位0 金花 A♥ K♥ 7♥ → 你输"
	f.Log = []string{"第 3 局开始，底注 1，奖池 3", "Alice 看牌"}

	out := (&zjhModel{frame: f}).View()
	lines := strings.Split(out, "\n")
	colored := 0
	for _, line := range lines {
		if hasSemanticColor(line) {
			colored++
		}
	}
	if colored > 5 {
		t.Fatalf("整屏用语义色的行太多了（%d 行 / 共 %d 行），摸鱼工具的观感会变花", colored, len(lines))
	}
	if colored == 0 {
		t.Fatal("一行高亮都没有：轮到我/比牌结果这些该跳出来的信息也得有一处颜色")
	}
}

// hasSemanticColor 判断一段文本是否用了语义色（绿/琥珀/红/品牌橙）。
// 只数这几种：灰阶不算"花"，它是这类界面的底色。
func hasSemanticColor(s string) bool {
	// 173 是 #D97757 在 256 色下的降采样结果
	for _, code := range []string{"38;5;114", "38;5;179", "38;5;203", "38;5;173", "38;2;217;119;87"} {
		if strings.Contains(s, code) {
			return true
		}
	}
	return false
}
