// subcell_test.go — laws for the braille and octant rung.

package subcell

import (
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
)

// A label is glyphs, never dots. The cells a label occupies carry no
// stroke — the outline of an ellipse passes around them, and an edge
// label interrupts its own edge the way the cell renderer's do.
func TestSubcellLabelsAreGlyphsOverClearedStrokes(t *testing.T) {
	rows := Draw(t.Context(), "digraph { rankdir=LR; alpha -> beta [label=\"go\"] }\n", 90, false)
	if rows == nil {
		t.Fatal("nothing drawn")
	}
	joined := grid.StripSGR(strings.Join(rows, "\n"))
	for _, want := range []string{" alpha ", " beta ", "go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("label %q not set as glyphs:\n%s", want, joined)
		}
	}
	if !HasInk(joined) {
		t.Errorf("no strokes at all:\n%s", joined)
	}
	// the cell before and after a node label is air, not the wall: the
	// outline was snapped to the run and sits one cell out
	for _, line := range strings.Split(joined, "\n") {
		if i := strings.Index(line, "alpha"); i > 0 {
			if HasInk(line[i-1 : i]) {
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
	jg, _, _, ok := fitInk(t.Context(), src, 100, 0)
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

// ---------- colour ----------

// escapes lists every SGR in a drawing, in order.
func escapes(rows []string) []string {
	var out []string
	s := strings.Join(rows, "\n")
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			out = append(out, s[i:j+1])
		}
		i = j
	}
	return out
}

// THE CONTROL. A graph nobody coloured draws exactly what it drew before
// this rung could see colour: the only escapes in it are the dim pair,
// and they come in order. Every colour a graph does not declare — black
// included, because black is what graphviz writes when nobody asked —
// leaves the pen alone, so `wire.colour` is only ever called with 0 and
// only ever writes nothing. That is the whole byte-identity argument,
// and equiv.sh's 254 cases hold the other end of it against a build made
// before colour.go existed.
func TestSubcellUncolouredIsTheDrawingFromBefore(t *testing.T) {
	src := "digraph { rankdir=LR; subgraph cluster_a { label=\"front\"; ui -> store }; " +
		"store -> api [style=dashed, label=\"json\"]; api -> db [dir=both]; " +
		"black [color=\"#000000\", fontcolor=\"#000000\"]; db -> black }\n"
	for _, octants := range []bool{false, true} {
		rows := Draw(t.Context(), src, 100, octants)
		if rows == nil {
			t.Fatal("nothing drawn")
		}
		dim := false
		for _, e := range escapes(rows) {
			switch e {
			case "\x1b[2m":
				if dim {
					t.Errorf("dim set twice over")
				}
				dim = true
			case "\x1b[22m":
				if !dim {
					t.Errorf("undimmed while not dim")
				}
				dim = false
			default:
				t.Fatalf("octants=%v: an uncoloured graph emitted %q; "+
					"only the dim pair was ever in this drawing", octants, e)
			}
		}
		if dim {
			t.Errorf("octants=%v: a drawing ended still dim", octants)
		}
	}
}

// Colour is a pen, not a shape: the same graph with every colour attribute
// added strips back to the same characters in the same cells. If this ever
// fails, colour has started moving the drawing.
func TestSubcellColourMovesNoDot(t *testing.T) {
	const plain = "digraph { rankdir=LR; a -> b [label=\"go\"]; b -> c }\n"
	const painted = "digraph { rankdir=LR; a [color=\"#7aa2f7\", fontcolor=\"#7aa2f7\"]; " +
		"c [color=\"#9ece6a\"]; a -> b [label=\"go\", color=\"#f7768e\", fontcolor=\"#f7768e\"]; b -> c }\n"
	for _, octants := range []bool{false, true} {
		p := Draw(t.Context(), plain, 90, octants)
		q := Draw(t.Context(), painted, 90, octants)
		if p == nil || q == nil {
			t.Fatal("nothing drawn")
		}
		a := grid.StripSGR(strings.Join(p, "\n"))
		b := grid.StripSGR(strings.Join(q, "\n"))
		if a != b {
			t.Errorf("octants=%v: colour moved the drawing:\n%s\n---\n%s", octants, a, b)
		}
	}
	// and the pens the graph named actually reach the terminal
	rows := Draw(t.Context(), painted, 90, false)
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"\x1b[38;2;122;162;247m", "\x1b[38;2;158;206;106m", "\x1b[38;2;247;118;142m"} {
		if !strings.Contains(joined, want) {
			t.Errorf("pen %q never reached the row", want)
		}
	}
}

// What graphviz writes, and what each of them means to the pen.
func TestSubcellParseColor(t *testing.T) {
	for _, c := range []struct {
		in   string
		want uint32
	}{
		{"#7aa2f7", penSet | 0x7aa2f7},
		{"#7aa2f7ff", penSet | 0x7aa2f7},
		{"#000000", 0},                 // graphviz's default: nobody asked
		{"#000000ff", 0},               // the same, written long
		{"#ffffff00", 0},               // an invisible fill is no ink
		{"#010000", penSet | 0x010000}, // but very nearly black is a choice
		{"", 0},
		{"red", 0}, // the json never writes a name; if it did, no crash
		{"#zzzzzz", 0},
	} {
		if got := parseColor(c.in); got != c.want {
			t.Errorf("parseColor(%q) = %#x, want %#x", c.in, got, c.want)
		}
	}
}
