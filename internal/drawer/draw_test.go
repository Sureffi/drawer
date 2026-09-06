// draw_test.go — laws for the ladder: which rung a run draws on, and what
// it hands back in place of a fence.

package drawer

import (
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
)

// pickRung is the whole of the auto rung's judgement, and every branch of
// it is a fact about the terminal the run is standing in. A rung asked for
// by name is that rung, whatever the terminal says. Otherwise: pixels want
// a terminal that draws placeholder cells — kitty or ghostty — and a cell
// size in pixels to cut a picture to. Everything else is cells, which is
// every terminal that draws no placeholders and every one that draws them
// but would not say how big a cell is.
//
// The name is the terminal's, already resolved: under tmux term.Name
// answers with the terminal behind it, so "xterm-kitty" here is a kitty
// with or without a multiplexer in the way and this function never learns
// the difference.
func TestPickRungReadsTheTerminal(t *testing.T) {
	geom := term.Geom{CellW: 10, CellH: 24}
	for _, c := range []struct {
		name  string
		want  rung
		term  string
		geom  term.Geom
		draws rung
	}{
		{"asked for by name", rungCells, "xterm-kitty", geom, rungCells},
		{"pixels asked for by name", rungPixels, "xterm-256color", term.Geom{}, rungPixels},
		{"kitty and a cell size", rungAuto, "xterm-kitty", geom, rungPixels},
		{"kitty through a pipe, so no cell size", rungAuto, "xterm-kitty", term.Geom{}, rungCells},
		{"ghostty and a cell size", rungAuto, "xterm-ghostty", geom, rungPixels},
		{"ghostty through a pipe, so no cell size", rungAuto, "xterm-ghostty", term.Geom{}, rungCells},
		{"anything else", rungAuto, "xterm-256color", geom, rungCells},
		{"no TERM at all", rungAuto, "", geom, rungCells},
	} {
		if got := pickRung(c.want, c.term, c.geom); got != c.draws {
			t.Errorf("%s: draws in %s, want %s", c.name, got, c.draws)
		}
	}
}

// A rung asked for by name is that rung — and the pixels rung still asks
// the one thing of the terminal that nothing else in this binary asks:
// U+10EEEE is a picture in kitty and ghostty and a tofu box everywhere
// else, and the picture itself goes down the parent's own tty as an escape
// nobody there would read. So `-render pixels` in a terminal that draws
// none of that draws glyphs, which is the same fail-open every other stage
// of the rung takes.
func TestForcedPixelsStillNeedATerminalThatDrawsThem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	r := run{rung: rungPixels, term: "xterm-256color", geom: term.Geom{CellW: 10, CellH: 24}, theme: &inForce{}}
	rows := r.drawBlock(t.Context(), "digraph { a -> b }", 60)
	if rows == nil {
		t.Fatal("a forced pixels rung drew nothing at all")
	}
	if strings.ContainsRune(strings.Join(rows, "\n"), pixel.PlaceholderRune) {
		t.Errorf("placeholder cells went to a terminal that draws none:\n%s", strings.Join(rows, "\n"))
	}
}

// Draw hands CC a bare fence. Every row inside it fits the width it was
// drawn for, and nothing but the fence comes back: no caption, no source —
// the reader gets the picture, not the plumbing.
func TestDrawModeEmitsABareFenceThatFits(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	src := "digraph { rankdir=LR; parse -> check -> emit; check -> warn }\n"
	rows := r.drawBlock(t.Context(), src, 90)
	if rows == nil {
		t.Fatal("nothing drawn")
	}
	if rows[0] != fenceTick || rows[len(rows)-1] != fenceTick {
		t.Fatalf("not a bare fence:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows[1 : len(rows)-1] {
		if n := grid.Cells(grid.StripSGR(row)); n > 90 {
			t.Errorf("row is %d cells in 90 columns: %q", n, row)
		}
		if strings.Contains(row, "digraph") {
			t.Errorf("the source reached the reader: %q", row)
		}
	}
	if !strings.Contains(strings.Join(rows, "\n"), "▶") {
		t.Error("no arrowhead: the fence was replaced by something that is not the drawing")
	}
}

// A fence that will not fit says why, and the reader keeps the source in
// the same fence — one fence in, one fence out is what the oracle holds
// the wire to, so the notice may not become a second block.
func TestDrawModeNoticeKeepsTheSourceInOneFence(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	// a label wider than the window: no orientation can save it
	src := "digraph { rankdir=LR; alpha -> \"a label far wider than thirty columns of window\" }\n"
	rows := r.drawBlock(t.Context(), src, 30)
	if rows == nil {
		t.Fatal("a too-narrow window produced nothing, not even a reason")
	}
	joined := strings.Join(rows, "\n")
	if strings.Count(joined, fenceTick+"\n") != 1 || !strings.HasSuffix(joined, fenceTick) {
		t.Fatalf("notice and source are not one fence:\n%s", joined)
	}
	if !strings.Contains(joined, "no diagram") || !strings.Contains(joined, "alpha ->") {
		t.Fatalf("notice without its source, or source without its notice:\n%s", joined)
	}
}
