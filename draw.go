// draw.go — the hook draws, and hands CC a finished picture.
//
// This is the whole output path. A ```dot fence goes
// in; what comes back is a bare fence holding the drawing, and CC lays it
// out as ordinary code-block text: two columns of indent, leading spaces
// kept, ANSI colour kept, every unusual glyph kept. All of that was measured
// through the display wire on CC 2.1.257 before this file was written, and
// the shape below is exactly what survived.
//
// The rungs, best first, each failing open to the one below it:
//
//	pixels   a real graphviz picture in kitty's placeholder cells
//	octants  strokes at 2x4 per cell, solid; labels as glyphs
//	braille  the same at 2x4, dotted, and every font has it
//	cells    box-drawing characters, and nothing but this binary
//
// What no rung can do: be right after a resize. CC keeps what a hook
// returned and re-wraps it without asking again, so the drawing is correct
// at the width it was drawn for, shreds narrower, and comes back when the
// window does. A terminal wrapper that sees the window could re-derive the
// drawing every frame and win that row; this takes the trade so that the
// graphs work with nothing but `claude` and the plugin.

package main

import (
	"strings"
)

// drawBlock turns one fence source into the rows that replace it, or nil
// to leave the fence exactly as it arrived. A fence that will not draw at
// this width still says why: the notice rides above the source, so a typo
// and a narrow window stop looking alike.
func drawBlock(src string, width int) []string {
	if width <= 0 {
		width = 100
	}
	rung := pickRung()
	if rung == rungPixels {
		if rows := drawPixels(src, width); rows != nil {
			return fence(rows)
		}
		rung = rungOctants // cairo said no; the glyphs still can
	}
	if rung == rungOctants || rung == rungBraille {
		if rows := drawSubcell(src, width, rung == rungOctants); rows != nil {
			return fence(rows)
		}
	}
	l, h, ok := fit(src, width, 0)
	if ok && h <= drawMaxRows {
		if rows := renderDiagram(l, width, h); rows != nil {
			return fence(trimBlank(rows))
		}
	}
	// Nothing drew. Say why, and leave the source readable under the
	// notice, in the same fence: one fence in, one fence out is the law the
	// oracle holds the wire to, and a reader gets both the reason and the
	// DOT it was about.
	reason := cutReason(src, width, drawMaxRows)
	notice := drawNotice(reason, width, 8)
	if notice == nil {
		return nil
	}
	rows := append(notice, "")
	rows = append(rows, strings.Split(strings.TrimSuffix(src, "\n"), "\n")...)
	return fence(rows)
}

// fence wraps rows in a bare fence: verbatim, monospace, no caption.
func fence(rows []string) []string {
	out := make([]string, 0, len(rows)+2)
	out = append(out, fenceTick)
	out = append(out, rows...)
	out = append(out, fenceTick)
	return out
}

// wantRung picks the rung: cells, braille, octants, pixels, or auto,
// which takes the best the terminal in front of us can show.
var wantRung rung

// pickRung answers which drawing this terminal gets. `auto` reads the
// terminal: pixels want kitty, a cell size in pixels and a rasteriser;
// octants want a terminal that draws them itself, which today means kitty
// or ghostty; everything else gets braille, which every font carries.
func pickRung() rung {
	if wantRung != rungAuto {
		return wantRung
	}
	term := termName()
	kitty := strings.Contains(term, "kitty")
	if kitty && hookGeom.ok() && probeRaster() != nil {
		return rungPixels
	}
	if kitty || strings.Contains(term, "ghostty") {
		return rungOctants
	}
	return rungBraille
}
