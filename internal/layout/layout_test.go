// layout_test.go — laws for the one door to graphviz.

package layout

import (
	"context"
	"testing"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// A fence the model opened and closed has no graph in it, and graphviz does
// not call that an error: ParseBytes answers (nil, nil), because nothing was
// wrong with what it was asked. Everything downstream dereferenced that nil,
// so one empty ```dot fence took the process with it. Every other law in the
// tree passed the whole time it was live, which is the argument for this one.
func TestEmptySourceIsRefusedNotFatal(t *testing.T) {
	for _, src := range []string{
		"",
		"   ",
		"\t\n \n",
		"\ufeff",
		"// just a comment\n",
		"/* nothing */",
		"digraph{}",
	} {
		if _, _, ok := Fit(t.Context(), src, 90, 0); ok {
			t.Errorf("laid out a source with no graph in it: %q", src)
		}
	}
}

// A context whose time is up stops the door, and says so. The binding does
// not: measured on go-graphviz v0.2.10, New, ParseBytes and Render each run
// a cancelled context to completion and hand back a whole answer, so the
// only place the deadline can be honoured is the door itself. It has to
// come back as an error rather than as a wait, because the caller is a hook
// with a message half drawn and the fail-open path — the source under a
// notice — is reached through an error and nowhere else.
func TestACancelledContextIsAnErrorNotAWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opened := false
	err := Door(ctx, "digraph { a -> b }", func(context.Context, *graphviz.Graphviz, *cgraph.Graph) error {
		opened = true
		return nil
	})
	if err == nil {
		t.Fatal("the door opened on a cancelled context")
	}
	if opened {
		t.Error("graphviz was asked for a layout nobody was still waiting for")
	}
	if _, err := DOT(ctx, "digraph { a -> b }", ""); err == nil {
		t.Error("DOT laid out under a cancelled context")
	}
	if Complete(ctx, "digraph { a -> b }") {
		t.Error("a graph nobody could have parsed was called complete")
	}
}
