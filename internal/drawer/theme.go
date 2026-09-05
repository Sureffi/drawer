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
	"strings"

	"github.com/sureffi/drawer/internal/theme"
)

type inForce struct {
	th *theme.Theme
	// file is what -theme named, and empty where the theme is Claude Code's
	// own. Nothing draws from it; -doctor says it, because a picture in the
	// wrong colours is asking which theme this was.
	file string
	// readErr is why that file did not read, on the doors that carry on
	// without it: the hook, which has a session to open, and -doctor, which
	// is asked exactly when something is wrong.
	readErr error
	// deriveErr is why Claude Code's theme would not derive. get has already
	// said it on stderr and handed back graphviz's defaults; -doctor says
	// which theme actually came out.
	deriveErr error
}

func (f *inForce) get(ctx context.Context) *theme.Theme {
	if f.th == nil {
		th, err := theme.Claude(ctx)
		if err != nil {
			// The DOT being parsed here was built a package away out of a
			// fixed palette, so a parse error is a bug in this tree and
			// nothing a reader did; since the door took a deadline, a
			// process that ran out of time arrives here as well, and that
			// one is only a slow machine. Either is found on the display
			// wire, where the answer is to draw: an empty theme is
			// graphviz's own defaults, so the picture comes out plain
			// rather than not at all, and the line says which happened.
			fmt.Fprintf(os.Stderr, "drawer: theme: %s; drawing in graphviz's defaults\n", firstLine(err.Error()))
			f.deriveErr = err
			th = &theme.Theme{}
		}
		f.th = th
	}
	return f.th
}

// firstLine is the head of a message. graphviz ends its errors with a
// newline, and one line is what a session's stderr and a fact per line both
// have room for.
func firstLine(s string) string { s, _, _ = strings.Cut(s, "\n"); return s }
