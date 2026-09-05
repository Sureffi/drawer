// draw_test.go — laws for the ladder: which rung a run draws on, and what
// it hands back in place of a fence.

package drawer

import (
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/term"
)

// pickRung is the whole of the auto rung's judgement, and every branch of
// it is a fact about the terminal the run is standing in. A rung asked for
// by name is that rung, whatever the terminal says. Otherwise: pixels want
// kitty, a cell size in pixels and a rasteriser; octants want a terminal
// that draws them itself, which today means kitty or ghostty; everything
// else gets braille, which every font carries.
//
// Looking for a rasteriser walks the PATH, so it is asked last and only
// where the answer can still change — a law nothing enforced while this
// was a function that read the environment itself.
func TestPickRungReadsTheTerminal(t *testing.T) {
	geom := term.Geom{CellW: 10, CellH: 24}
	for _, c := range []struct {
		name       string
		want       rung
		term       string
		geom       term.Geom
		raster     bool
		draws      rung
		asksRaster bool
	}{
		{"asked for by name", rungCells, "xterm-kitty", geom, true, rungCells, false},
		{"kitty, a cell size and a rasteriser", rungAuto, "xterm-kitty", geom, true, rungPixels, true},
		{"kitty with nothing to rasterise with", rungAuto, "xterm-kitty", geom, false, rungOctants, true},
		{"kitty through a pipe, so no cell size", rungAuto, "xterm-kitty", term.Geom{}, true, rungOctants, false},
		{"ghostty draws its own octants", rungAuto, "xterm-ghostty", geom, true, rungOctants, false},
		{"anything else", rungAuto, "xterm-256color", geom, true, rungBraille, false},
		{"no TERM at all", rungAuto, "", geom, true, rungBraille, false},
	} {
		asked := false
		got := pickRung(c.want, c.term, c.geom, func() bool { asked = true; return c.raster })
		if got != c.draws {
			t.Errorf("%s: draws in %s, want %s", c.name, got, c.draws)
		}
		if asked != c.asksRaster {
			t.Errorf("%s: looked for a rasteriser: %v, want %v", c.name, asked, c.asksRaster)
		}
	}
}

// Draw hands CC a bare fence. Every row inside it fits the width it was
// drawn for, and nothing but the fence comes back: no caption, no source —
// the reader gets the picture, not the plumbing.
func TestDrawModeEmitsABareFenceThatFits(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	src := "digraph { rankdir=LR; parse -> check -> emit; check -> warn }\n"
	rows := r.drawBlock(src, 90)
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
	rows := r.drawBlock(src, 30)
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
