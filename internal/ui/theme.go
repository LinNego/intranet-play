package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// 一小组"开发工具"味道的视觉元素：方块 logo、⏺ 圆点、⎿ 续行、圆角输入框、
// 暗色分隔线。
//
// 配色克制但有重点：底色只有"默认 / 暗灰 / 加粗"，另外留三个语义色
// （绿=轮到你/赢、琥珀=限制、红=出局/被拒）。绿红本来就是终端里
// 测试输出与 diff 的样子，所以上色不会让它更像游戏，反而更像工具。

var (
	// 默认前景色：主要内容
	plainStyle = lipgloss.NewStyle()
	// 加粗：需要跳出来的那一处（自己的牌、关键标签）
	boldStyle = lipgloss.NewStyle().Bold(true)
	// 次级信息：标签、坐标轴、统计
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	// 更暗：边框、装饰
	faintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	// 提示/页脚
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// 语义色：只有这三档
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("114")) // 绿：轮到你、赢、通过
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("179")) // 琥珀：限制、需要决定
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // 红：被拒、出局、输

	// 品牌橙：只用在抬头那个方块图标上
	logoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))
)

// logoArt 是三行方块图标，右侧接说明文字。
var logoArt = []string{
	" ▐▛███▜▌  ",
	"▝▜█████▛▘ ",
	"  ▘▘ ▝▝   ",
}

// LogoBlock 是顶部的抬头：方块图标 + 最多三行说明。
// 第一行放原来的标题，第二行放实时状态，第三行放这桌在玩什么。
func LogoBlock(lines ...string) string {
	var b strings.Builder
	for i, art := range logoArt {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		b.WriteString(logoStyle.Render(art))
		switch {
		case i == 0:
			b.WriteString(plainStyle.Render(text))
		case i == 1:
			b.WriteString(mutedStyle.Render(text))
		default:
			b.WriteString(hintStyle.Render(text))
		}
		if i < len(logoArt)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Bullet 是圆点前缀，用来列日志/事件（像工具在逐条打日志）。
func Bullet(text string) string {
	return faintStyle.Render("⏺ ") + text
}

// TurnBullet 渲染状态行：轮到"你"时标绿（余光一扫就知道该自己了），否则压暗。
func TurnBullet(mine bool, text string) string {
	if mine {
		return Bullet(okStyle.Render(text))
	}
	return Bullet(mutedStyle.Render(text))
}

// LogLine 按内容给日志行上色，只认这三类：
// 被拒/出局/输 → 红，限制类提示 → 琥珀，其余 → 灰。
func LogLine(text string) string {
	switch {
	case strings.Contains(text, "被拒"), strings.Contains(text, "出局"),
		strings.Contains(text, "爆牌"), strings.Contains(text, "输"),
		strings.Contains(text, "失败"), strings.Contains(text, "作废"):
		return Bullet(errStyle.Render(text))
	case strings.Contains(text, "只能"), strings.Contains(text, "不够"),
		strings.Contains(text, "到顶"), strings.Contains(text, "必须"):
		return Bullet(warnStyle.Render(text))
	}
	return Bullet(hintStyle.Render(text))
}

// SubLine 是挂在圆点下面的续行。
func SubLine(text string) string {
	return hintStyle.Render("  ⎿ " + text)
}

// Field 是 "标签 值" 这种一行信息，标签压暗、值用默认色。
func Field(label, value string) string {
	return mutedStyle.Render(label+" ") + plainStyle.Render(value)
}

// Rule 是分段用的暗色横线。
func Rule(width int) string {
	if width <= 0 {
		width = 56
	}
	return faintStyle.Render(strings.Repeat("─", width))
}

// Section 是带标题的分段线，例如 "── 本局结算 ──────────"。
func Section(title string, width int) string {
	if width <= 0 {
		width = 56
	}
	label := " " + title + " "
	n := width - runewidth.StringWidth(label) - 2
	if n < 0 {
		n = 0
	}
	return faintStyle.Render("──") + mutedStyle.Render(label) + faintStyle.Render(strings.Repeat("─", n))
}

// InputBox 是底部那个圆角输入框。
func InputBox(input string, width int) string {
	if width < 24 {
		width = 60
	}
	inner := width - 4 // "│ " ... " │"
	textW := inner - 2 // 去掉 "❯ "
	line := input + "_"
	if w := runewidth.StringWidth(line); w > textW {
		line = runewidth.Truncate(line, textW, "")
	}
	pad := strings.Repeat(" ", textW-runewidth.StringWidth(line))

	top := faintStyle.Render("╭" + strings.Repeat("─", width-2) + "╮")
	mid := faintStyle.Render("│ ") + boldStyle.Render("❯ ") +
		plainStyle.Render(line) + pad + faintStyle.Render(" │")
	bottom := faintStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")
	return top + "\n" + mid + "\n" + bottom
}

// Footer 是底部一行暗色提示（以 "? " 开头，像工具在提示快捷键）。
func Footer(text string) string {
	return hintStyle.Render(text)
}
