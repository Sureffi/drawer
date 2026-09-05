// draw_test.go — laws for the ladder: which rung a run draws on.

package main

import "testing"

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
	geom := pxGeom{CellW: 10, CellH: 24}
	for _, c := range []struct {
		name       string
		want       rung
		term       string
		geom       pxGeom
		raster     bool
		draws      rung
		asksRaster bool
	}{
		{"asked for by name", rungCells, "xterm-kitty", geom, true, rungCells, false},
		{"kitty, a cell size and a rasteriser", rungAuto, "xterm-kitty", geom, true, rungPixels, true},
		{"kitty with nothing to rasterise with", rungAuto, "xterm-kitty", geom, false, rungOctants, true},
		{"kitty through a pipe, so no cell size", rungAuto, "xterm-kitty", pxGeom{}, true, rungOctants, false},
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
