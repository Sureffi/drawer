// subcell_test.go — laws for the braille and octant rung.

package main

import (
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
)

// A label is glyphs, never dots. The cells a label occupies carry no
// stroke — the outline of an ellipse passes around them, and an edge
// label interrupts its own edge the way the cell renderer's do.
func TestSubcellLabelsAreGlyphsOverClearedStrokes(t *testing.T) {
	rows := drawSubcell("digraph { rankdir=LR; alpha -> beta [label=\"go\"] }\n", 90, false)
	if rows == nil {
		t.Fatal("nothing drawn")
	}
	joined := grid.StripSGR(strings.Join(rows, "\n"))
	for _, want := range []string{" alpha ", " beta ", "go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("label %q not set as glyphs:\n%s", want, joined)
		}
	}
	if !hasSubcellInk(joined) {
		t.Errorf("no strokes at all:\n%s", joined)
	}
	// the cell before and after a node label is air, not the wall: the
	// outline was snapped to the run and sits one cell out
	for _, line := range strings.Split(joined, "\n") {
		if i := strings.Index(line, "alpha"); i > 0 {
			if hasSubcellInk(line[i-1 : i]) {
				t.Errorf("stroke touching the label: %q", line)
			}
		}
	}
}

// What the cell renderer silently flattens, this one draws: a cluster is
// a frame with its name, a dashed edge is dashed, and both heads of a
// dir=both edge are there. The proof is in the ops graphviz hands over,
// so the law checks that they were read rather than what they looked like.
func TestSubcellReadsClustersAndStyles(t *testing.T) {
	src := "digraph { rankdir=LR; subgraph cluster_a { label=\"front\"; ui -> store }; store -> api [style=dashed]; api -> db [dir=both] }\n"
	jg, _, _, ok := fitInk(src, 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	clusters, dashed, twoHeads := 0, 0, 0
	for _, o := range jg.Objects {
		if o.Nodes != nil {
			clusters++
		}
	}
	for _, e := range jg.Edges {
		for _, op := range e.Draw {
			if op.Op == "S" && op.Style == "dashed" {
				dashed++
			}
		}
		if len(e.HDraw) > 0 && len(e.TDraw) > 0 {
			twoHeads++
		}
	}
	if clusters != 1 || dashed != 1 || twoHeads != 1 {
		t.Fatalf("clusters=%d dashed=%d both=%d; graphviz's own drawing was not read", clusters, dashed, twoHeads)
	}
	rows := renderInk(jg, 100, 40, false)
	joined := grid.StripSGR(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "front") {
		t.Errorf("cluster label not drawn:\n%s", joined)
	}
}
