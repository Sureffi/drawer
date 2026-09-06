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
// `digraph{}` is the other half of it: the door answers a graph, and the
// graph is empty, and an empty graph is nothing to draw.
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
		g, err := Read(t.Context(), src)
		if err == nil && g != nil && len(g.Nodes) > 0 {
			t.Errorf("read nodes out of a source with no graph in it: %q", src)
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

// A drawing answers for the things in it by name, and a name it does not
// carry is nothing rather than the first thing it does carry: the pixels
// rung walks it by name, the strokes read it the same way, and both would
// otherwise draw the wrong object without a word. Its size comes off `bb`,
// which every rung divides by, so a layout with no bounding box is an error
// and not a zero-sized picture.
func TestADrawingIsReadByName(t *testing.T) {
	var d *Drawing
	err := Door(t.Context(), `digraph { a -> b [label="x"] }`, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		var err error
		d, err = Render(ctx, g, graph)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.W <= 0 || d.H <= 0 {
		t.Fatalf("the drawing is %vx%v points", d.W, d.H)
	}
	if d.Object("a") == nil || d.Object("b") == nil {
		t.Fatal("a node the source named is not in the drawing")
	}
	if o := d.Object("c"); o != nil {
		t.Errorf("a name the drawing does not carry answered with %q", o.Name)
	}
	if d.Between("a", "b") == nil {
		t.Error("the edge between two named nodes is not in the drawing")
	}
	if d.Between("b", "a") != nil {
		t.Error("an edge that runs the other way was answered with")
	}
	if TextY(d.Object("a").LDraw) == 0 {
		t.Error("a node's label sets no baseline")
	}
	if TextY(nil) != 0 {
		t.Error("a list that sets no type has a baseline anyway")
	}
}
