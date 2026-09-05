// pixel_test.go — laws for the pixels rung: the placeholders, the cut, the
// theme on the picture, the labels on their lines, and the ledger.

package drawer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

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

// ---------- the pixel theme ----------

// svgGroup is the SVG of one titled element: a node, an edge or a cluster.
func svgGroup(svg []byte, title string) string {
	s := string(svg)
	i := strings.Index(s, "<title>"+title+"</title>")
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "</g>")
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

// svgTextY is the baseline of the first text in a group: where graphviz
// put the thing, up the page as it goes negative.
func svgTextY(group string) float64 {
	m := regexp.MustCompile(`<text [^>]*\by="(-?[0-9.]+)"`).FindStringSubmatch(group)
	if m == nil {
		return 0
	}
	return atof(m[1])
}

// Type is measured in Courier, which the wasm's tables know, and set in
// the terminal's face, which fontconfig knows; a label measured in one face
// and set in another runs out of its box. At a known cell width the size is
// the one that puts a glyph in a cell.
func TestPixelTypeIsMeasuredInCourierAndSetInMonospace(t *testing.T) {
	svg, err := renderThemedSVG("digraph { a -> b }", pxFontPt(10), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), "Courier") {
		t.Error("the layout font reached the SVG")
	}
	if !strings.Contains(string(svg), `font-family="monospace"`) {
		t.Error("the SVG does not name the terminal's face")
	}
	if !strings.Contains(string(svg), `font-size="12.50"`) {
		t.Errorf("a 10px cell wants 12.5pt type; got %s", svg)
	}
}

// What the model painted stays painted, and graphviz's own defaults apply
// around its paint: a pink node keeps black text, as `dot` would give it. A
// shape it asked for is drawn, records included. Where it left an attribute
// unset, the theme fills in.
func TestPixelThemeKeepsTheModelsPaint(t *testing.T) {
	src := `digraph {
		a [fillcolor=pink, style=filled, color=red]
		b [shape=ellipse]
		c [shape=record, label="{head|body|tail}"]
		d
		a -> b -> c -> d
	}`
	svg, err := renderThemedSVG(src, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a := svgGroup(svg, "a")
	if !strings.Contains(a, `fill="pink"`) || !strings.Contains(a, `stroke="red"`) {
		t.Errorf("the model's paint was overwritten:\n%s", a)
	}
	th := currentTheme()
	if strings.Contains(a, th.Node["fontcolor"]) {
		t.Errorf("theme text on the model's fill:\n%s", a)
	}
	if !strings.Contains(svgGroup(svg, "b"), "<ellipse") {
		t.Error("the model asked for an ellipse and got a box")
	}
	c := svgGroup(svg, "c")
	if strings.Contains(c, "{head|body|tail}") || !strings.Contains(c, ">body<") {
		t.Errorf("the record printed its markup:\n%s", c)
	}
	d := svgGroup(svg, "d")
	if !strings.Contains(d, th.Node["fillcolor"]) || !strings.Contains(d, th.Node["fontcolor"]) || !strings.Contains(d, th.Node["color"]) {
		t.Errorf("an unpainted node did not get the theme:\n%s", d)
	}
}

// Every cluster is themed, not only the first. A subgraph answers for
// attributes it never set with the root's value, so the clusters have to be
// read before the root is themed; the second cluster is where that showed.
func TestPixelThemeReachesEveryCluster(t *testing.T) {
	src := `digraph {
		subgraph cluster_a { label="a side"; x }
		subgraph cluster_b { label="b side"; y }
		x -> y
	}`
	svg, err := renderThemedSVG(src, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	th := currentTheme()
	for _, name := range []string{"cluster_a", "cluster_b"} {
		g := svgGroup(svg, name)
		if !strings.Contains(g, `stroke="`+th.Graph["color"]+`"`) {
			t.Errorf("%s outline is not themed:\n%s", name, g)
		}
		if !strings.Contains(g, `fill="`+th.Graph["fontcolor"]+`"`) {
			t.Errorf("%s label is not themed:\n%s", name, g)
		}
		if !strings.Contains(g, `font-family="monospace"`) {
			t.Errorf("%s label is not in the terminal's face:\n%s", name, g)
		}
	}
}

// The picture stands on the terminal's own ground: no background unless
// the model asked for one.
func TestPixelBackgroundIsTheTerminalsUnlessSet(t *testing.T) {
	svg, err := renderThemedSVG("digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), `fill="white"`) || strings.Contains(string(svg), `stroke="transparent"`) {
		t.Error("a background was painted under a graph that set none")
	}
	svg, err = renderThemedSVG("digraph { bgcolor=white; a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), `fill="white"`) {
		t.Error("the model's background was dropped")
	}
}

// ---------- edge labels on their lines ----------

// rewritten parses a source and puts its edge labels on their edges, for a
// look at the graph itself. The caller closes both.
func rewritten(t *testing.T, src string) (*graphviz.Graphviz, *cgraph.Graph) {
	t.Helper()
	th := currentTheme() // before the lock: parsing a fresh theme takes it
	graphvizMu.Lock()
	defer graphvizMu.Unlock()
	g, err := graphviz.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil || graph == nil {
		t.Fatal("no graph:", err)
	}
	inlineEdgeLabels(graph, th, 0, true)
	return g, graph
}

// edgeAttr is one attribute of the edge between two named nodes, or "no
// such edge".
func edgeAttr(graph *cgraph.Graph, tail, head, attr string) string {
	for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
		for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
			if t, h := ends(e); t == tail && h == head {
				return e.GetStr(attr)
			}
		}
	}
	return "no such edge"
}

// A labelled graph is spaced as dot spaces one: the ranksep in force is
// halved — the model's, else dot's half inch — and every plain edge's
// minlen is doubled, so it spans a full rank gap and not the half a label
// node's rank left it. The halves of a labelled edge keep the model's
// minlen, the label at their midpoint. A graph with no labels is not
// touched.
func TestPixelLabelsSpaceAsDotDoes(t *testing.T) {
	g, graph := rewritten(t, "digraph { ranksep=\"1 equally\"; a -> b [label=x, minlen=3]; b -> c; c -> d [minlen=3] }")
	defer g.Close()
	defer graph.Close()
	if rs := graph.GetStr("ranksep"); rs != "0.5 equally" {
		t.Errorf("ranksep is %q, want the model's halved", rs)
	}
	for _, c := range []struct{ tail, head, want string }{
		{"b", "c", "2"}, {"c", "d", "6"},
		{"a", labelNodePrefix + "0", "3"}, {labelNodePrefix + "0", "b", "3"},
	} {
		if got := edgeAttr(graph, c.tail, c.head, "minlen"); got != c.want {
			t.Errorf("%s -> %s: minlen %q, want %q", c.tail, c.head, got, c.want)
		}
	}
	g2, bare := rewritten(t, "digraph { a -> b [label=x]; b -> c }")
	defer g2.Close()
	defer bare.Close()
	if rs := bare.GetStr("ranksep"); rs != "0.25" {
		t.Errorf("ranksep is %q with none declared, want dot's half inch halved", rs)
	}
	g3, plain := rewritten(t, "digraph { ranksep=1; a -> b; b -> c }")
	defer g3.Close()
	defer plain.Close()
	if rs, ml := plain.GetStr("ranksep"), edgeAttr(plain, "a", "b", "minlen"); rs != "1" || ml != "" {
		t.Errorf("an unlabelled graph was respaced: ranksep %q, minlen %q", rs, ml)
	}
}

// A labelled edge is drawn as two edges through a node carrying the label,
// each half in the edge's own paint, and a `dir=both` edge keeps a head at
// each end: the back arrow on the first half, the forward on the second.
func TestPixelEdgeLabelSitsOnItsLine(t *testing.T) {
	svg, err := renderThemedSVG(`digraph { a -> b [label="x", color=red, dir=both] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if g := svgGroup(svg, "a&#45;&gt;b"); g != "" {
		t.Errorf("the labelled edge is still drawn whole:\n%s", g)
	}
	first := svgGroup(svg, "a&#45;&gt;"+labelNodePrefix+"0")
	second := svgGroup(svg, labelNodePrefix+"0&#45;&gt;b")
	for name, half := range map[string]string{"first": first, "second": second} {
		if !strings.Contains(half, `stroke="red"`) {
			t.Errorf("the %s half lost the edge's colour:\n%s", name, half)
		}
		if n := strings.Count(half, "<polygon"); n != 1 {
			t.Errorf("the %s half has %d heads, want one:\n%s", name, n, half)
		}
	}
	label := svgGroup(svg, labelNodePrefix+"0")
	th := currentTheme()
	if !strings.Contains(label, ">x<") || !strings.Contains(label, `fill="`+th.Edge["fontcolor"]+`"`) {
		t.Errorf("the label is not on the line in the edge label colour:\n%s", label)
	}
	if strings.Contains(label, th.Node["fillcolor"]) {
		t.Errorf("the label node was filled like a node:\n%s", label)
	}
}

// An undirected labelled edge grows no heads.
func TestPixelUndirectedLabelGrowsNoHeads(t *testing.T) {
	svg, err := renderThemedSVG(`graph { a -- b [label="x"] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"a&#45;&#45;" + labelNodePrefix + "0", labelNodePrefix + "0&#45;&#45;b"} {
		half := svgGroup(svg, title)
		if half == "" {
			t.Errorf("no half titled %s", title)
		}
		if strings.Contains(half, "<polygon") {
			t.Errorf("an undirected half grew a head:\n%s", half)
		}
	}
}

// A labelled edge inside a cluster keeps its label in the cluster, or dot
// would route the edge out of the cluster and back to visit it.
func TestPixelEdgeLabelStaysInItsCluster(t *testing.T) {
	svg, err := renderThemedSVG(`digraph { subgraph cluster_c { a -> b [label="x"] } c -> a }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// The cluster's outline is drawn before its members; the label's group
	// following it inside the same SVG is how graphviz writes membership.
	s := string(svg)
	cluster := strings.Index(s, "<title>cluster_c</title>")
	label := strings.Index(s, "<title>"+labelNodePrefix+"0</title>")
	outside := strings.Index(s, "<title>c</title>")
	if cluster < 0 || label < 0 || outside < 0 {
		t.Fatalf("missing cluster, label or outside node in\n%s", s)
	}
	if !(cluster < label && label < outside) {
		t.Errorf("the label node is not written inside its cluster (cluster %d, label %d, outside %d)", cluster, label, outside)
	}
}

// The label of an edge that closes a cycle sits between the edge's ends,
// and the arrow still points where the model pointed it.
func TestPixelLabelOnABackEdgeSitsBetweenItsEnds(t *testing.T) {
	svg, err := renderThemedSVG(`digraph { a -> b -> c; c -> a [label="no"] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	ya, yc := svgTextY(svgGroup(svg, "a")), svgTextY(svgGroup(svg, "c"))
	yl := svgTextY(svgGroup(svg, labelNodePrefix+"0"))
	if ya == 0 || yc == 0 || yl == 0 {
		t.Fatalf("missing a node: a=%v c=%v label=%v", ya, yc, yl)
	}
	if !(min(ya, yc) < yl && yl < max(ya, yc)) {
		t.Errorf("the label is at %v, not between its ends at %v and %v", yl, ya, yc)
	}
	// The chain runs a -> label -> c, and the head is on the half that
	// touches a: drawn as a back arrow on that half.
	first := svgGroup(svg, "a&#45;&gt;"+labelNodePrefix+"0")
	second := svgGroup(svg, labelNodePrefix+"0&#45;&gt;c")
	if first == "" || second == "" {
		t.Fatalf("the back edge was not chained the other way round:\n%s", svg)
	}
	if strings.Count(first, "<polygon") != 1 || strings.Count(second, "<polygon") != 0 {
		t.Errorf("the arrow moved: half at a has %d heads, half at c has %d", strings.Count(first, "<polygon"), strings.Count(second, "<polygon"))
	}
}

// Inlining labels doubles the ranks, so ranksep is halved as dot does for
// its own label nodes — whichever is in force: the model's, else the
// theme's, else dot's half inch. A model writing dot's default draws the
// same chain as one writing nothing, and a theme's inch is a half.
func TestPixelLabelsHalveRanksepAsDotDoes(t *testing.T) {
	height := func(src string) float64 {
		svg, err := renderThemedSVG(src, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		_, h, err := svgSize(svg)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	bare := height(`digraph { a -> b [label="x"] }`)
	if dflt := height(`digraph { ranksep=0.5; a -> b [label="x"] }`); dflt != bare {
		t.Errorf("a labelled chain is %vpt with dot's default written and %vpt with none; the model's ranksep was not halved", dflt, bare)
	}
	if model := height(`digraph { ranksep=1; a -> b [label="x"] }`); !(model > bare) {
		t.Errorf("the model's ranksep was overridden: %vpt at 1, %vpt at the default", model, bare)
	}
	withTheme(t, `graph [ranksep=1]`)
	if themed := height(`digraph { a -> b [label="x"] }`); !(themed > bare) {
		t.Errorf("the theme's ranksep was overridden: %vpt themed, %vpt at the default", themed, bare)
	}
}

// A picture wider than the window is laid out top-down as well, as the
// glyph rungs do — rows scroll, columns run out — and the orientation that
// keeps more of its type is the picture: top-down when that fits at the
// cell's own type and as written does not, top-down when both are squeezed
// and it is squeezed less, and as written when top-down is too tall for
// the ceiling — which is the picture that was there before this law. The
// cut says which way it went.
func TestPixelCutFlipsTopDownBeforeSqueezing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { setTheme(nil) })
	setTheme(nil)
	var zooms []float64
	r := &Raster{name: "stub", run: func(_ []byte, zoom float64) ([]byte, error) {
		zooms = append(zooms, zoom)
		return []byte("png"), nil
	}}
	geom := PxGeom{CellW: 10, CellH: 24}
	chain := func(n int) string {
		var b strings.Builder
		b.WriteString("digraph { rankdir=LR; ")
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteString(" -> ")
			}
			fmt.Fprintf(&b, "step_number_%02d", i)
		}
		b.WriteString(" }")
		return b.String()
	}
	last := func() float64 { return zooms[len(zooms)-1] }

	wide := chain(6)
	_, p, err := pixelCut(r, wide, 100, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != cgraph.TBRank || p.Cols > 100 {
		t.Errorf("a chain too wide for 100 columns was cut %d wide, laid out %q; want top-down within the width", p.Cols, p.Rankdir)
	}
	if last() < 1 {
		t.Errorf("flipped top-down and still squeezed: zoom %v", last())
	}
	_, p, err = pixelCut(r, wide, len(RowColumnDiacritics), geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != "" || last() < 1 {
		t.Errorf("a chain with room to spare was laid out %q at zoom %v; want as written at the cell's own type", p.Rankdir, last())
	}

	_, p, err = pixelCut(r, wide, 12, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != cgraph.TBRank || p.Cols != 12 || last() >= 1 {
		t.Errorf("a chain too wide either way was laid out %q, %d wide at zoom %v; want top-down, squeezed less", p.Rankdir, p.Cols, last())
	}

	tall := chain(60)
	_, p, err = pixelCut(r, tall, 100, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != "" || p.Cols != 100 || last() >= 1 {
		t.Errorf("a chain too tall top-down was laid out %q, %d wide at zoom %v; want as written, squeezed into 100 columns", p.Rankdir, p.Cols, last())
	}
}

// The offline picture is the hook's picture: cut to whole columns of the
// cell it was asked for. Skipped where there is nothing to rasterise with.
func TestRunPNGWritesTheHooksPicture(t *testing.T) {
	if FindRaster() == nil {
		t.Skip("no rasteriser on the PATH")
	}
	dir := t.TempDir()
	dot := dir + "/g.dot"
	png := dir + "/g.png"
	if err := os.WriteFile(dot, []byte("digraph { rankdir=LR; a -> b [label=\"x\"]; b -> c }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := RunPNG(dot, png, 100, PxGeom{CellW: 10, CellH: 24}); code != 0 {
		t.Fatalf("RunPNG exited %d", code)
	}
	b, err := os.ReadFile(png)
	if err != nil {
		t.Fatal(err)
	}
	w, h, err := pngSize(b)
	if err != nil {
		t.Fatal(err)
	}
	if w%10 != 0 || w > 1000 || h <= 0 {
		t.Errorf("picture is %dx%d px; want a whole number of 10px columns within 100", w, h)
	}
}

// ---------- the ledger ----------

// The pictures a session drew are sent to the terminal again, under their
// own ids, when the theme in force is not the one they stand in — each
// once, however often it was drawn — and not otherwise.
func TestPixelLedgerRepaintsUnderTheOldIDs(t *testing.T) {
	r := FindRaster()
	if r == nil {
		t.Skip("no rasteriser on the PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir()) // the pictures kitty is not here to collect
	t.Cleanup(func() { setTheme(nil) })
	setTheme(nil)
	// the terminal is a file here: transmitFile opens it, it does not make it
	tty := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(tty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	geom := PxGeom{CellW: 10, CellH: 24}
	a := picture{Src: "digraph { a -> b }", Cols: 20, Rows: 3, Geom: geom}
	b := picture{Src: "digraph { c -> d }", Cols: 20, Rows: 3, Geom: geom}
	recordPicture("s1", a)
	recordPicture("s1", b)
	recordPicture("s1", a)
	if n := repaintPictures("s1", tty, r); n != 0 {
		t.Errorf("repainted %d pictures under the theme they were drawn in", n)
	}
	withTheme(t, `node [color=red]`)
	if n := repaintPictures("s1", tty, r); n != 2 {
		t.Fatalf("repainted %d pictures, want 2", n)
	}
	out, err := os.ReadFile(tty)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []picture{a, b} {
		id := ",i=" + strconv.Itoa(int(hookImageID(p.Src, p.Cols, p.Rows))) + ","
		if n := strings.Count(string(out), id); n != 1 {
			t.Errorf("%q was sent %d times under its id, want once", p.Src, n)
		}
	}
	if n := repaintPictures("s1", tty, r); n != 0 {
		t.Errorf("repainted %d pictures with nothing changed", n)
	}
	if n := repaintPictures("s2", tty, r); n != 0 {
		t.Errorf("repainted %d pictures for a session that drew none", n)
	}
}

// A picture drawn top-down is repainted top-down. The ledger carries the
// orientation with the cut, so what kitty gets under the old id is the
// picture that was there, in the new colours, and not the same source laid
// out the other way and squeezed onto the old columns.
func TestPixelLedgerRepaintsAsLaidOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Cleanup(func() { setTheme(nil) })
	setTheme(nil)
	tty := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(tty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var got []byte
	r := &Raster{name: "stub", run: func(svg []byte, _ float64) ([]byte, error) {
		got = svg
		return []byte("png"), nil
	}}
	geom := PxGeom{CellW: 10, CellH: 24}
	recordPicture("s1", picture{Src: "digraph { rankdir=LR; a -> b }", Cols: 12, Rows: 7, Geom: geom, Rankdir: cgraph.TBRank})
	withTheme(t, `node [color=red]`)
	if n := repaintPictures("s1", tty, r); n != 1 {
		t.Fatalf("repainted %d pictures, want 1", n)
	}
	ya, yb := svgTextY(svgGroup(got, "a")), svgTextY(svgGroup(got, "b"))
	if ya == 0 || yb == 0 {
		t.Fatalf("missing a node: a=%v b=%v", ya, yb)
	}
	if ya == yb {
		t.Errorf("repainted left-to-right, a and b both at y=%v; the picture was drawn top-down", ya)
	}
}
