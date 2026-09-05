// cli.go — the offline doors. Every one of them works with no account, no
// pty and no live session, which is deliberate: this feature cost a lot of
// live calls to debug before they existed.

package drawer

import (
	"fmt"
	"os"

	"github.com/sureffi/drawer/internal/cells"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/subcell"
	"github.com/sureffi/drawer/internal/term"
)

// runDotDump draws a DOT source at a given cell size and prints it — the
// fast loop for judging how a diagram actually reads, without a live
// session in the way.
//
//	drawer -dot graph.dot -size 100x14 -render braille
func (r run) runDotDump(path string, w, h int) int {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-dot:", err)
		return 1
	}
	var rows []string
	if r.rung == rungBraille || r.rung == rungOctants {
		rows = subcell.Draw(string(b), w, r.rung == rungOctants)
		if rows == nil {
			fmt.Fprintf(os.Stderr, "-dot: will not fit in %d columns as strokes (source would be left alone)\n", w)
			return 1
		}
	}
	l, _, ok := layout.Fit(string(b), w, 0)
	if ok && rows == nil {
		rows = cells.Draw(l, w, h)
	}
	if rows == nil {
		// the layout already worked the answer out; reporting only "will
		// not fit" makes the caller hand-search for a size the tool knows
		if l == nil {
			l, _ = layout.DOT(string(b), "")
		}
		if dw, dh := layout.Footprint(l); dw > 0 && dh > 0 {
			fmt.Fprintf(os.Stderr, "-dot: needs %dx%d, given %dx%d (source would be left alone)\n",
				dw, dh, w, h)
		} else {
			fmt.Fprintf(os.Stderr, "-dot: will not lay out (source would be left alone)\n")
		}
		return 1
	}
	for _, row := range rows {
		fmt.Println(row)
	}
	return 0
}

// runPNG draws a DOT file the way the hook would for a terminal `width`
// cells wide with cells of `geom` pixels, and writes the picture to a file:
// a theme, or a graph, looked at without a session. Nonzero when there is
// no picture, with the reason on stderr.
func (r run) runPNG(dotPath, pngPath string, width int, geom term.Geom) int {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	ras := pixel.Find()
	if ras == nil {
		fmt.Fprintln(os.Stderr, "drawer: no rasteriser on the PATH (rsvg-convert or magick)")
		return 1
	}
	png, p, err := pixel.Cut(r.theme.get(), ras, string(src), width, geom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	how := ""
	if p.Rankdir != "" {
		how = ", laid out top-down to fit"
	}
	fmt.Printf("%s: %d×%d cells%s\n", pngPath, p.Cols, p.Rows, how)
	return 0
}
