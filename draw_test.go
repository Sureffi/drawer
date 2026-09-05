// draw_test.go — laws for Draw and its rungs.

package drawer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
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

// ---------- the theme file ----------

// withTheme makes a theme the theme for one test.
func withTheme(t *testing.T, src string) {
	old := currentTheme()
	th, err := ParseTheme(src)
	if err != nil {
		t.Fatal(err)
	}
	setTheme(th)
	t.Cleanup(func() { setTheme(old) })
}

// A theme is DOT declarations, and Claude Code's theme is one: with no
// Claude Code settings to read it is the stock dark palette on the picture,
// and what it declares for each kind comes back as the values it wrote.
func TestThemeIsDOTDeclarations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	src := ClaudeThemeDOT()
	th, err := ParseTheme(src)
	if err != nil {
		t.Fatal(err)
	}
	if th.Graph["bgcolor"] != "transparent" || th.Node["fillcolor"] != "#373737" || th.Node["color"] != "#d77757" || th.Edge["fontcolor"] != "#4eba65" {
		t.Errorf("Claude Code's dark theme read back wrong: %+v", th)
	}
	if th.Source != src {
		t.Error("a theme does not keep the DOT it was read from")
	}
	if th.Face() != "monospace" {
		t.Errorf("face is %q with no fontname declared", th.Face())
	}
	if _, err := ParseTheme("node ["); err == nil {
		t.Error("an unclosed declaration parsed")
	}
}

// The theme in force is the one Claude Code was told to use, resolved as
// Claude Code resolves it: the `theme` setting, in settings.json first and
// the older ~/.claude.json where that has none; a stock name is its
// palette, an ansi variant its base's, a custom name the base its file
// declares with the overrides laid over; auto and anything unreadable are
// dark. An override is a colour in a form a picture can use, or nothing.
func TestThemeFollowsClaudeCodesTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(func() { setTheme(nil) })
	if err := os.MkdirAll(filepath.Join(home, ".claude", "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	put := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, rel)
		if body == "" {
			os.Remove(path)
			return
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// what the picture is drawn in: node fill, stroke, text; edge label.
	drawn := func() string {
		t.Helper()
		setTheme(nil)
		th := currentTheme()
		return th.Node["fillcolor"] + " " + th.Node["color"] + " " + th.Node["fontcolor"] + " " + th.Edge["fontcolor"]
	}
	dark := "#373737 #d77757 #ffffff #4eba65"
	light := "#f0f0f0 #d77757 #000000 #2c7a39"
	for _, c := range []struct{ name, settings, older, custom, want string }{
		{"nothing readable", "", "", "", dark},
		{"dark", `{"theme":"dark"}`, "", "", dark},
		{"light", `{"theme":"light"}`, "", "", light},
		{"light-daltonized", `{"theme":"light-daltonized"}`, "", "", "#dcdcdc #ff9933 #000000 #006699"},
		{"dark-ansi draws as dark", `{"theme":"dark-ansi"}`, "", "", dark},
		{"auto is dark", `{"theme":"auto"}`, "", "", dark},
		{"the older file alone", "", `{"theme":"light"}`, "", light},
		{"settings.json over the older file", `{"theme":"dark"}`, `{"theme":"light"}`, "", dark},
		{"custom on a light base, overrides in every form", `{"theme":"custom:x"}`, "",
			`{"name":"x","base":"light","overrides":{"claude":"#123456","text":"rgb(1, 2, 3)","success":"ansi:green","userMessageBackground":"#abc"}}`,
			"#aabbcc #123456 #010203 #2c7a39"},
		{"custom on an ansi base", `{"theme":"custom:x"}`, "", `{"name":"x","base":"dark-ansi"}`, dark},
		{"custom with no file", `{"theme":"custom:x"}`, "", "", dark},
		{"not JSON", `{"theme":`, "", "", dark},
		{"not a string", `{"theme":7}`, "", "", dark},
	} {
		put(".claude/settings.json", c.settings)
		put(".claude.json", c.older)
		put(".claude/themes/x.json", c.custom)
		if got := drawn(); got != c.want {
			t.Errorf("%s: drew %s, want %s", c.name, got, c.want)
		}
	}
}

// A theme file reaches the picture: its fill is the nodes' fill, its edge
// colour the edges', its fontname the face the SVG is set in — and what it
// does not declare is graphviz's default, not the built-in theme's.
func TestThemeFileReachesThePicture(t *testing.T) {
	withTheme(t, `node [fillcolor="#7aa2f71f", fontname="JetBrains Mono"]
	              edge [color=red]`)
	svg, err := renderThemedSVG("digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// graphviz writes an RGBA fill as a colour and an opacity.
	a := svgGroup(svg, "a")
	if !strings.Contains(a, `fill="#7aa2f7" fill-opacity="0.12`) {
		t.Errorf("the theme's fill did not reach the node:\n%s", a)
	}
	if strings.Contains(a, "#c0caf5") {
		t.Errorf("the built-in text colour leaked through a theme that has none:\n%s", a)
	}
	if e := svgGroup(svg, "a&#45;&gt;b"); !strings.Contains(e, `stroke="red"`) {
		t.Errorf("the theme's edge colour did not reach the edge:\n%s", e)
	}
	if !strings.Contains(string(svg), `font-family="JetBrains Mono"`) || strings.Contains(string(svg), "Courier") {
		t.Error("the picture is not set in the theme's face")
	}
}

// The model's paint still wins over a theme file, as it does over the
// built-in one.
func TestThemeFileYieldsToTheModel(t *testing.T) {
	withTheme(t, `node [fillcolor="#000000", color="#111111"]`)
	svg, err := renderThemedSVG("digraph { a [color=red]; a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := svgGroup(svg, "a"); !strings.Contains(a, `stroke="red"`) {
		t.Errorf("the theme overwrote the model:\n%s", a)
	}
	if b := svgGroup(svg, "b"); !strings.Contains(b, `stroke="#111111"`) {
		t.Errorf("the theme did not reach an unpainted node:\n%s", b)
	}
}

// ---------- turns ----------

// A delta's process waits for the one before it: started first with the
// later index, it takes its turn after the earlier delta's process has
// saved, and reads what that one saved. A turn nobody comes to take is
// waited for only so long.
func TestHookDeltasTakeTurns(t *testing.T) {
	t.Setenv("DRAWER_STATE", t.TempDir())
	var mu sync.Mutex
	var order []int
	took := func(i int) { mu.Lock(); order = append(order, i); mu.Unlock() }
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		st, done := takeTurn("m", 1, 2*time.Second)
		defer done()
		took(1)
		if st.Next != 1 || st.Buf != "held by 0" {
			t.Errorf("delta 1 took its turn on state %+v, not the one delta 0 saved", st)
		}
		st.Next = 2
		saveState("m", st, true)
	}()
	time.Sleep(30 * time.Millisecond) // delta 1 is waiting
	go func() {
		defer wg.Done()
		st, done := takeTurn("m", 0, 2*time.Second)
		defer done()
		took(0)
		time.Sleep(40 * time.Millisecond) // the draw
		st.Next, st.Buf = 1, "held by 0"
		saveState("m", st, false)
	}()
	wg.Wait()
	if len(order) != 2 || order[0] != 0 || order[1] != 1 {
		t.Errorf("turns were taken in the order %v, want [0 1]", order)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("DRAWER_STATE"), "m.json.lock")); err == nil {
		t.Error("the lock file outlived the message")
	}
	start := time.Now()
	_, done := takeTurn("nobody", 3, 50*time.Millisecond)
	done()
	if waited := time.Since(start); waited < 50*time.Millisecond || waited > time.Second {
		t.Errorf("a turn nobody takes was waited for %v, want about the patience", waited)
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

// svgTextY is the baseline of the first text in a group: where graphviz
// put the thing, up the page as it goes negative.
func svgTextY(group string) float64 {
	m := regexp.MustCompile(`<text [^>]*\by="(-?[0-9.]+)"`).FindStringSubmatch(group)
	if m == nil {
		return 0
	}
	return atof(m[1])
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

// The offline picture is the hook's picture: cut to whole columns of the
// cell it was asked for. Skipped where there is nothing to rasterise with.
func TestRunPNGWritesTheHooksPicture(t *testing.T) {
	if FindRaster() == nil {
		t.Skip("no rasteriser on the PATH")
	}
	dir := t.TempDir()
	dot := dir + "/g.dot"
	png := dir + "/g.png"
	if err := writeFile(dot, "digraph { rankdir=LR; a -> b [label=\"x\"]; b -> c }\n"); err != nil {
		t.Fatal(err)
	}
	if code := RunPNG(dot, png, 100, PxGeom{CellW: 10, CellH: 24}); code != 0 {
		t.Fatalf("RunPNG exited %d", code)
	}
	b, err := readFile(png)
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

func writeFile(path, text string) error    { return os.WriteFile(path, []byte(text), 0o644) }
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
