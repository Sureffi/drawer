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
