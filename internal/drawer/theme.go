// theme.go — the theme this run draws in.
//
// Deriving Claude Code's theme reads its settings and opens graphviz, so it
// is not done until a picture actually asks: most deltas are prose and draw
// nothing, and a hook is one process per delta. -theme fills this in up
// front instead, because a file that will not parse is an answer everywhere
// but the hook.
//
// One process per delta and one goroutine in it, so the memo is a nil check
// and not a sync.Once.

package drawer

import (
	"context"
	"fmt"
	"os"

	"github.com/sureffi/drawer/internal/theme"
)

type inForce struct {
	th *theme.Theme
	// file is where -theme read this theme from, and empty where the theme
	// is Claude Code's own. Nothing draws from it; -doctor says it, because
	// a picture in the wrong colours is asking which theme this was.
	file string
}

func (f *inForce) get(ctx context.Context) *theme.Theme {
	if f.th == nil {
		th, err := theme.Claude(ctx)
		if err != nil {
			// The DOT being parsed here was built a package away out of a
			// fixed palette, so it not parsing is a bug in this tree and
			// nothing a reader did. It would still be found on the display
			// wire, where the answer to any bug is to draw: an empty theme
			// is graphviz's own defaults, so the picture comes out plain
			// rather than not at all, and the line says which happened.
			fmt.Fprintf(os.Stderr, "drawer: theme: %v; drawing in graphviz's defaults\n", err)
			th = &theme.Theme{}
		}
		f.th = th
	}
	return f.th
}
