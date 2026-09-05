// cli.go — the offline doors. Every one of them works with no account, no
// pty and no live session, which is deliberate: this feature cost a lot of
// live calls to debug before they existed.

package drawer

import (
	"fmt"
	"os"
)

// ParseSize reads WxH, or hands back the default.
func ParseSize(s string, dw, dh int) (int, int) {
	var w, h int
	if n, _ := fmt.Sscanf(s, "%dx%d", &w, &h); n == 2 && w > 0 && h > 0 {
		return w, h
	}
	return dw, dh
}

// RunDotDump draws a DOT source at a given cell size and prints it — the
// fast loop for judging how a diagram actually reads, without a live
// session in the way.
//
//	drawer -dot graph.dot -size 100x14 -render braille
func RunDotDump(path string, w, h int) int {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-dot:", err)
		return 1
	}
	var rows []string
	if Rung == "braille" || Rung == "octants" {
		rows = drawSubcell(string(b), w, Rung == "octants")
		if rows == nil {
			fmt.Fprintf(os.Stderr, "-dot: will not fit in %d columns as strokes (source would be left alone)\n", w)
			return 1
		}
	}
	l, _, ok := fit(string(b), w, 0)
	if ok && rows == nil {
		rows = renderDiagram(l, w, h)
	}
	if rows == nil {
		// the layout already worked the answer out; reporting only "will
		// not fit" makes the caller hand-search for a size the tool knows
		if l == nil {
			l, _ = layoutDOT(string(b), "")
		}
		if dw, dh := footprint(l); dw > 0 && dh > 0 {
			fmt.Fprintf(os.Stderr, "-dot: needs %dx%d, given %dx%d (source would be left alone)\n",
				dw, dh, w, h)
		} else {
			fmt.Fprintf(os.Stderr, "-dot: will not lay out (source would be left alone)\n")
		}
		return 1
	}
	for _, r := range rows {
		fmt.Println(r)
	}
	return 0
}

// RunPNG draws a DOT file the way the hook would for a terminal `width`
// cells wide with cells of `geom` pixels, and writes the picture to a file:
// a theme, or a graph, looked at without a session. Nonzero when there is
// no picture, with the reason on stderr.
func RunPNG(dotPath, pngPath string, width int, geom PxGeom) int {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	r := FindRaster()
	if r == nil {
		fmt.Fprintln(os.Stderr, "drawer: no rasteriser on the PATH (rsvg-convert or magick)")
		return 1
	}
	png, cols, rows, err := pixelCut(r, string(src), width, geom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	fmt.Printf("%s: %d×%d cells\n", pngPath, cols, rows)
	return 0
}
