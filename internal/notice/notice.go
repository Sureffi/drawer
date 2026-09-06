// notice.go — a drawing that says why there is no drawing.
//
// The old answer to a fence that would not draw was to leave the source
// showing. That is honest and it is readable, and it is also silent about
// the one thing the reader wants: whether this is source because somebody
// asked for source, or because something failed. A window three columns
// too narrow and a graph with a typo in it looked identical.
//
// So the failure gets drawn too, over the source, in the same fence.

package notice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// noticeChrome is the border and padding a notice spends on itself.
const noticeChrome = 4

// Draw renders a bordered box carrying reason, sized for a w by h region.
// Returns nil when there is not enough room to say anything — a notice too
// small to read is worse than the source it replaced.
func Draw(reason string, w, h int) []string {
	if h < 3 || w < 24 {
		return nil
	}
	inner := w - noticeChrome
	if inner > 72 {
		inner = 72
	}
	body := wrapWords(reason, inner)
	if len(body) > h-2 {
		body = body[:h-2]
	}
	if len(body) == 0 {
		return nil
	}
	width := 0
	for _, l := range body {
		if n := grid.Cells(l); n > width {
			width = n
		}
	}
	const title = " no diagram "
	if n := grid.Cells(title); width < n {
		width = n
	}

	rows := make([]string, 0, len(body)+2)
	top := "╭" + title + strings.Repeat("─", width-grid.Cells(title)+2) + "╮"
	rows = append(rows, top)
	for _, l := range body {
		rows = append(rows, "│ "+l+strings.Repeat(" ", width-grid.Cells(l))+" │")
	}
	rows = append(rows, "╰"+strings.Repeat("─", width+2)+"╯")
	return rows
}

// wrapWords breaks a reason at spaces, measured in cells rather than
// bytes — the reasons carry × and box glyphs.
func wrapWords(s string, width int) []string {
	if width < 8 {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case grid.Cells(line)+1+grid.Cells(word) <= width:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// Reason works out why a source did not become a drawing in the space it
// was given, and says it in terms the reader can act on. Width is the
// only lever they have — rows are bounded by the window and the ladder
// already spent them — so where widening would work, the notice names the
// column count that does it rather than the row count that failed.
//
// Asked only on the failing path: it lays the graph out again to find out.
func Reason(ctx context.Context, src string, width, region int) string {
	l, err := layout.DOT(ctx, src, "")
	if err != nil {
		// A door that refused on the deadline has read nothing and has
		// nothing to say about the source. Reporting it as graphviz could
		// not read this sends a reader hunting a typo in a fence that is
		// fine, when what happened is that the process ran out of time.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "the layout ran out of time"
		}
		return "graphviz could not read this: " + firstLine(err.Error())
	}
	if l == nil {
		return "no graph in this fence"
	}
	lw, lh := layout.Footprint(l) // as written, usually left-right
	if lw <= 0 || lh <= 0 {
		return "nothing to draw"
	}
	th := 0
	if td, err := layout.DOT(ctx, src, cgraph.TBRank); err == nil && td != nil {
		if tw, h := layout.Footprint(td); tw > 0 && tw <= width {
			th = h
		}
	}
	switch {
	case lw <= width && lh <= region:
		// graphviz's footprint fits and the drawing still did not come out.
		// The rung sizes its own boxes and its own gaps and fails for its
		// own reasons — every lane short at every rung of the ladder, both
		// ways up — and graphviz's inches know nothing about that. Naming a
		// width the reader already has is worse than saying plainly what
		// happened. Width is still the only lever, so the notice names the
		// one the reader has rather than inventing a smaller one that
		// would not help.
		return fmt.Sprintf("will not come out whole in %d columns; a wider window may", width)
	case lh <= region && lw > width:
		return fmt.Sprintf("needs %d columns, this window has %d", lw, width)
	case th > 0 && lh <= region:
		return fmt.Sprintf("%d rows top-down; %d columns would draw it sideways in %d",
			th, lw, lh)
	case th > 0:
		return fmt.Sprintf("needs %d rows, this region has %d", th, region)
	}
	return fmt.Sprintf("needs %d by %d, this region is %d by %d", lw, lh, width, region)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
