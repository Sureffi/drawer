// draw.go — the hook draws, and hands CC a finished picture.
//
// This is the whole output path. A ```dot fence goes
// in; what comes back is a fence labelled text holding the drawing, and CC
// lays it out as ordinary code-block text: leading spaces kept, ANSI colour
// kept, every unusual glyph kept. All of that was measured through the
// display wire on CC 2.1.257 before this file was written, and the shape
// below is exactly what survived.
//
// The fence is labelled text. Claude Code 2.1.280 and later colours a
// fence that names no language like inline code: every run that is not a
// space, in its `permission` colour, over whatever the rows asked for —
// measured on 2.1.283, the dim structure and the uncoloured labels of a
// glyph drawing came out in that colour, and only the strokes a graph
// coloured kept theirs. A fence that names a language goes to the
// highlighter instead, and `text` is a language it knows and colours
// nothing in: the rows come out as they went in.
//
// The rungs, best first, the upper failing open to the lower:
//
//	pixels   a real graphviz picture in kitty's placeholder cells —
//	         kitty draws them, and so does ghostty
//	cells    box-drawing glyphs, which every terminal draws with its own
//	         hand, so a line is a line
//
// There is no third. A mosaic of sub-cell blocks — octants, braille —
// cannot draw a thin line or an arrowhead, because both are smaller than
// the block it is made of, and those two are the whole of a graph. The
// glyphs are lines already.
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
	// Too narrow, too tall, out of time, or no way through to the
	// terminal: the glyphs still can, so the picture falls to them rather
	// than to a notice.
	if r.pickRung() == rungPixels {
		if rows := r.drawPixels(ctx, src, width); rows != nil {
			return fenced(rows)
		}
	}
	if rows, _ := drawRows(ctx, src, width); rows != nil {
		return fenced(rows)
	}
	return nil
}

// drawRows is the drawing as text: the glyphs, or a notice with the source
// under it — which the second answer says — or nil where not even a notice
// fits. Both doors draw text this way: the display hook in a fence, the
// element door as runs.
func drawRows(ctx context.Context, src string, width int) (rows []string, isNotice bool) {
	if g, err := layout.Read(ctx, src); err == nil {
		if rows := cells.Draw(g, width, grid.MaxRows); rows != nil {
			return grid.TrimBlank(rows), false
		}
	}
	// Nothing drew. Say why, and leave the source readable under the
	// notice, in the same fence: one fence in, one fence out is the law the
	// oracle holds the wire to, and a reader gets both the reason and the
	// DOT it was about.
	reason := notice.Reason(ctx, src, width, grid.MaxRows)
	box := notice.Draw(reason, width, 8)
	if box == nil {
		return nil, false
	}
	rows = append(box, "")
	return append(rows, strings.Split(strings.TrimSuffix(src, "\n"), "\n")...), true
}

// The fence a drawing is handed back in: opened as text, closed bare.
const (
	fenceOpen = "```text"
	fenceTick = "```"
)

// fenced wraps rows in the fence: verbatim, monospace, no caption.
func fenced(rows []string) []string {
	out := make([]string, 0, len(rows)+2)
	out = append(out, fenceOpen)
	out = append(out, rows...)
	out = append(out, fenceTick)
	return out
}

// pickRung answers which drawing this terminal gets. `auto` reads the
// terminal, and nothing else: pixels want a terminal that draws
// placeholder cells and a cell size in pixels to cut the picture to.
// Everything else is cells — a terminal that draws no placeholders, and a
// terminal that draws them but would not say how big a cell is, which is
// what a pipe looks like. Cells is not the fallback in the sense of the
// lesser thing: it is what most terminals get, and it is held to the same
// bar as the best box drawing anyone has made of a graph.
//
// The list is pixel.Placeholders and it lives there alone — one predicate,
// read by the gate and by the doctor, so a terminal joins the rung in one
// place. ghostty joined it on 2026-09-06, measured on the rig: its
// placeholder cells hold streaming, complete and scrolled, inside tmux and
// out.
//
// Everything the judgement needs is what the run already knows, so it can
// be read — and tested — without a terminal anywhere near it.
func pickRung(want rung, name string, geom term.Geom) rung {
	if want != rungAuto {
		return want
	}
	if pixel.Placeholders(name) && geom.OK() {
		return rungPixels
	}
	return rungCells
}

func (r run) pickRung() rung {
	return pickRung(r.rung, r.term, r.geom)
}
