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
//	pixels   a real graphviz picture in kitty's placeholder cells —
//	         kitty draws them, and so does ghostty
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

package drawer

import (
	"context"
	"strings"

	"github.com/sureffi/drawer/internal/cells"
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/notice"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/subcell"
	"github.com/sureffi/drawer/internal/term"
)

// drawBlock turns one fence source into the rows that replace it, or nil
// to leave the fence exactly as it arrived. A fence that will not draw at
// this width still says why: the notice rides above the source, so a typo
// and a narrow window stop looking alike.
func (r run) drawBlock(ctx context.Context, src string, width int) []string {
	if width <= 0 {
		width = 100
	}
	pick := r.pickRung()
	if pick == rungPixels {
		if rows := r.drawPixels(ctx, src, width); rows != nil {
			return bare(rows)
		}
		pick = rungOctants // cairo said no; the glyphs still can
	}
	if pick == rungOctants || pick == rungBraille {
		if rows := subcell.Draw(ctx, src, width, pick == rungOctants); rows != nil {
			return bare(rows)
		}
	}
	l, h, ok := layout.Fit(ctx, src, width, 0)
	if ok && h <= grid.MaxRows {
		if rows := cells.Draw(l, width, h); rows != nil {
			return bare(grid.TrimBlank(rows))
		}
	}
	// Nothing drew. Say why, and leave the source readable under the
	// notice, in the same fence: one fence in, one fence out is the law the
	// oracle holds the wire to, and a reader gets both the reason and the
	// DOT it was about.
	reason := notice.Reason(ctx, src, width, grid.MaxRows)
	box := notice.Draw(reason, width, 8)
	if box == nil {
		return nil
	}
	rows := append(box, "")
	rows = append(rows, strings.Split(strings.TrimSuffix(src, "\n"), "\n")...)
	return bare(rows)
}

// fenceTick is the bare fence a drawing is handed back in.
const fenceTick = "```"

// bare wraps rows in a bare fence: verbatim, monospace, no caption.
func bare(rows []string) []string {
	out := make([]string, 0, len(rows)+2)
	out = append(out, fenceTick)
	out = append(out, rows...)
	out = append(out, fenceTick)
	return out
}

// pickRung answers which drawing this terminal gets. `auto` reads the
// terminal: pixels want a terminal that draws placeholder cells, a cell
// size in pixels and a rasteriser; octants want a terminal that draws them
// itself, which is the same list; everything else gets braille, which every
// font carries.
//
// The list is pixel.Placeholders and it lives there alone — one predicate,
// read by the gate and by the doctor, so a terminal joins the rung in one
// place. ghostty joined it on 2026-09-06, measured on the rig: its
// placeholder cells hold streaming, complete and scrolled, inside tmux and
// out. It was on the octants tier only because this gate had asked for the
// word kitty.
//
// raster is a thunk because looking for a rasteriser means walking the
// PATH: it is asked only where the terminal and the geometry have already
// said yes. Everything else it needs is what the run already knows, so the
// judgement can be read — and tested — without a terminal anywhere near it.
func pickRung(want rung, name string, geom term.Geom, raster func() bool) rung {
	if want != rungAuto {
		return want
	}
	if !pixel.Placeholders(name) {
		return rungBraille
	}
	if geom.OK() && raster() {
		return rungPixels
	}
	return rungOctants
}

func (r run) pickRung() rung {
	return pickRung(r.rung, r.term, r.geom, func() bool { return pixel.Probe() != nil })
}
