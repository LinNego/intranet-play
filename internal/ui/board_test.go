package ui

import (
	"regexp"
	"strings"
	"testing"

	"intranet-play/internal/game"
	"github.com/mattn/go-runewidth"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderBoardAlignment(t *testing.T) {
	b := game.NewBoard(15)
	_, _ = b.Place(1, 1, 2)
	_, _ = b.Place(2, 2, 1)

	plain := ansiRE.ReplaceAllString(renderBoard(b), "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	if len(lines) != 16 {
		t.Fatalf("want 16 lines, got %d\n%s", len(lines), plain)
	}
	if strings.Contains(lines[0], "101112") {
		t.Fatalf("column headers cramped: %q", lines[0])
	}
	// hex axis should include 'a' for column 10
	if !strings.Contains(lines[0], "a") {
		t.Fatalf("expected hex axis label, got %q", lines[0])
	}

	wantWidth := gutterW + 15*cellW
	for i, line := range lines {
		if w := runewidth.StringWidth(line); w != wantWidth {
			t.Fatalf("line %d width=%d want=%d\n%q", i, w, wantWidth, line)
		}
	}
}
