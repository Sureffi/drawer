// draw_test.go — laws for Draw and its rungs.

package drawer

import (
	"strings"
	"testing"
)

// Draw hands CC a bare fence. Every row inside it fits the width it was
// drawn for, and nothing but the fence comes back: no caption, no source —
// the reader gets the picture, not the plumbing.
func TestDrawModeEmitsABareFenceThatFits(t *testing.T) {
	defer withRender("cells")()
	src := "digraph { rankdir=LR; parse -> check -> emit; check -> warn }\n"
	rows := drawBlock(src, 90)
	if rows == nil {
		t.Fatal("nothing drawn")
	}
	if rows[0] != FenceTick || rows[len(rows)-1] != FenceTick {
		t.Fatalf("not a bare fence:\n%s", strings.Join(rows, "\n"))
	}
	for _, r := range rows[1 : len(rows)-1] {
		if n := textCells(stripSGR(r)); n > 90 {
			t.Errorf("row is %d cells in 90 columns: %q", n, r)
		}
		if strings.Contains(r, "digraph") {
			t.Errorf("the source reached the reader: %q", r)
		}
	}
	if !strings.Contains(strings.Join(rows, "\n"), "▶") {
		t.Error("no arrowhead: the fence was replaced by something that is not the drawing")
	}
}

// A fence that will not fit says why, and the reader keeps the source in
// the same fence — one fence in, one fence out is what the oracle holds
// the wire to, so the notice may not become a second block.
func TestDrawModeNoticeKeepsTheSourceInOneFence(t *testing.T) {
	defer withRender("cells")()
	// a label wider than the window: no orientation can save it
	src := "digraph { rankdir=LR; alpha -> \"a label far wider than thirty columns of window\" }\n"
	rows := drawBlock(src, 30)
	if rows == nil {
		t.Fatal("a too-narrow window produced nothing, not even a reason")
	}
	joined := strings.Join(rows, "\n")
	if strings.Count(joined, FenceTick+"\n") != 1 || !strings.HasSuffix(joined, FenceTick) {
		t.Fatalf("notice and source are not one fence:\n%s", joined)
	}
	if !strings.Contains(joined, "no diagram") || !strings.Contains(joined, "alpha ->") {
		t.Fatalf("notice without its source, or source without its notice:\n%s", joined)
	}
}

// The hook wire quantises a truecolor foreground, so a picture's id rides
// in a 256-colour index and a third diacritic. The low byte is never zero
// — zero is "no image" — and every cell names its row and column itself,
// because CC re-wraps what a hook returns and a cell that lost its
// neighbour must not lose its place.
func TestPlaceholderRowsNameTheirImageOnEveryCell(t *testing.T) {
	id := hookImageID("digraph { a -> b }", 12, 3)
	if id&0xff == 0 {
		t.Fatal("image id has a zero low byte")
	}
	rows := placeholderRows(id, 12, 3)
	if len(rows) != 3 {
		t.Fatalf("%d rows for a 3-row block", len(rows))
	}
	for r, row := range rows {
		if !strings.HasPrefix(row, "\x1b[38;5;") {
			t.Fatalf("row %d does not open with a 256-colour foreground: %q", r, row)
		}
		plain := stripSGR(row)
		cells := strings.Count(plain, string(PlaceholderRune))
		if cells != 12 {
			t.Fatalf("row %d has %d placeholder cells, want 12", r, cells)
		}
		want := string(PlaceholderRune) + string(RowColumnDiacritics[r]) + string(RowColumnDiacritics[0])
		if !strings.HasPrefix(plain, want) {
			t.Fatalf("row %d does not start with row-then-column marks", r)
		}
		if textCells(plain) != 12 {
			t.Fatalf("row %d measures %d cells; the marks took columns", r, textCells(plain))
		}
	}
}

// A label is glyphs, never dots. The cells a label occupies carry no
// stroke — the outline of an ellipse passes around them, and an edge
// label interrupts its own edge the way the cell renderer's do.
func TestSubcellLabelsAreGlyphsOverClearedStrokes(t *testing.T) {
	rows := drawSubcell("digraph { rankdir=LR; alpha -> beta [label=\"go\"] }\n", 90, false)
	if rows == nil {
		t.Fatal("nothing drawn")
	}
	joined := stripSGR(strings.Join(rows, "\n"))
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
	joined := stripSGR(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "front") {
		t.Errorf("cluster label not drawn:\n%s", joined)
	}
}

// The axis a drawing flows along is the layout's answer, not a guess from
// where the nodes landed: a top-down tree wider than it is tall used to
// read as left-right and collapse onto its root's row.
func TestTopDownTreeKeepsItsRanks(t *testing.T) {
	src := "digraph { rankdir=TB; root -> parser; root -> checker; root -> emitter; parser -> lexer; parser -> ast; checker -> types; checker -> scopes; emitter -> ir; emitter -> asm }\n"
	l, h, ok := fit(src, 116, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 116, h)
	rowOf := func(label string) int {
		for i, r := range rows {
			if strings.Contains(r, "│"+label+"│") || strings.Contains(r, "│"+label+"├") || strings.Contains(r, "┤"+label+"│") || strings.Contains(r, "┤"+label+"├") {
				return i
			}
		}
		return -1
	}
	root, parser, lexer := rowOf("root"), rowOf("parser"), rowOf("lexer")
	if root < 0 || parser < 0 || lexer < 0 {
		t.Fatalf("labels missing:\n%s", strings.Join(rows, "\n"))
	}
	if !(root < parser && parser < lexer) {
		t.Errorf("ranks collapsed: root %d, parser %d, lexer %d\n%s", root, parser, lexer, strings.Join(rows, "\n"))
	}
}

// A `graph { a -- b }` has no heads to draw.
func TestUndirectedGraphHasNoArrowheads(t *testing.T) {
	l, h, ok := fit("graph { rankdir=LR; a -- b -- c }\n", 80, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	if joined := strings.Join(renderDiagram(l, 80, h), "\n"); strings.ContainsAny(joined, "▶◀▲▼") {
		t.Errorf("arrowheads on an undirected graph:\n%s", joined)
	}
}

// withRender sets the rung for one test and puts it back.
func withRender(render string) func() {
	r := Rung
	Rung = render
	return func() { Rung = r }
}

// drawAt is Draw's emit bound to a width, in the cells rung: what the
// transducer laws hand Stream.
func drawAt(w int) func(string) []string {
	return func(src string) []string {
		defer withRender("cells")()
		return Draw.Emit(src, w, 0)
	}
}
