// cells_test.go — laws for the cells rung and for Draw, one case per rule
// written into the code.
//
// These exist to show that each design actually kills the class of bug it
// claims to; the real oracles are the recorded delta streams (-deltas) and
// the drawings themselves (-dot), and check.sh runs all of it together.

package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// A fence the model opened and closed has no graph in it, and graphviz does
// not call that an error: ParseBytes answers (nil, nil), because nothing was
// wrong with what it was asked. Everything downstream dereferenced that nil,
// so one empty ```dot fence took the process with it. Every other case in
// this file passed the whole time it was live, which is the argument for
// this one.
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
		if _, _, ok := fit(src, 90, 0); ok {
			t.Errorf("laid out a source with no graph in it: %q", src)
		}
	}
}

// walled reports whether a label still has a left and right wall around
// it. The wall is not always `│`: where an edge attaches, the correct
// box-drawing glyph is the T that admits it. Asserting the literal `│`
// made this case fail the moment leaving edges started joining their
// walls, which is a stricter thing than the law it is here to protect.
func walled(row, label string) bool {
	i := strings.Index(row, label)
	if i <= 0 || i+len(label) >= len(row) {
		return false
	}
	left := []rune(row[:i])
	right := []rune(row[i+len(label):])
	isWall := func(r rune) bool { return r == '│' || r == '├' || r == '┤' }
	return len(left) > 0 && isWall(left[len(left)-1]) && len(right) > 0 && isWall(right[0])
}

// One ruler. Measuring a label in bytes and its box in runes drew the box
// one cell short and ate its own left border.
func TestLabelKeepsItsBox(t *testing.T) {
	l, h, ok := fit("digraph { rankdir=LR; \"käyttö\" -> \"sivu\" }\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 100, h)
	for _, r := range rows {
		if strings.Contains(r, "käyttö") && !walled(r, "käyttö") {
			t.Errorf("label lost its border: %q", r)
		}
	}
}

// One glyph, one measurement, all the way to the row. A wide label was
// measured in cells for its box and emitted as rune-plus-space by the
// rasteriser, so every row carrying one came out wider than the box drawn
// around it — right by the ruler, crooked on screen.
func TestWideLabelKeepsItsColumns(t *testing.T) {
	l, h, ok := fit(`digraph { rankdir=LR; "日本語" -> "ok" }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 100, h)
	var widths []int
	for _, r := range rows {
		if strings.TrimSpace(r) != "" {
			widths = append(widths, runewidth.StringWidth(r))
		}
	}
	if len(widths) < 3 {
		t.Fatalf("expected a three-row box, got %d drawn rows", len(widths))
	}
	for _, w := range widths {
		if w != widths[0] {
			t.Errorf("rows disagree about their width: %v in\n%s", widths, strings.Join(rows, "\n"))
			break
		}
	}
}

// `\"` and `\\` are this format's only escapes. Eating the backslash of
// everything else turned a label of `a\nb` into `anb` — a word nobody
// wrote, drawn with full confidence. Absent is survivable; invented is not.
func TestLabelEscapesAreNotEaten(t *testing.T) {
	l, h, ok := fit(`digraph { rankdir=LR; A[label="a\nb"]; A -> B }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	for _, r := range renderDiagram(l, 100, h) {
		if strings.Contains(r, "anb") {
			t.Errorf("escape eaten, invented a word: %q", r)
		}
	}
}

// The field count says whether an edge carries a label; asking whether the
// text looks like a number dropped every numeric one. graphviz had already
// answered by how many fields it wrote.
func TestNumericEdgeLabelDraws(t *testing.T) {
	l, h, ok := fit(`digraph { rankdir=LR; A -> B [label="42"] }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 100, h)
	if !strings.Contains(strings.Join(rows, "\n"), "42") {
		t.Errorf("numeric edge label not drawn:\n%s", strings.Join(rows, "\n"))
	}
}

// An arrow meets the box it points at. graphviz stops a spline short to
// leave room for an arrowhead it expected to draw itself, and taking that
// end literally left a cell of white between every arrow and its target.
// The boxes are ours; where they are is not something to infer.
func TestArrowMeetsItsBox(t *testing.T) {
	l, h, ok := fit("digraph { rankdir=LR; wire -> grid -> paint }\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 100, h)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "▶ ") {
		t.Errorf("arrowhead left short of its box:\n%s", joined)
	}
	if !strings.Contains(joined, "▶│") {
		t.Errorf("no arrowhead met a box border:\n%s", joined)
	}
}

// An edge leaving a node joins the wall it leaves from. A port sits one
// cell outside the box, which is where an arrowhead goes — at the head of
// an edge the arrowhead is the attachment and reads fine. A tail has no
// arrowhead, so when graphviz put the port on a border row (the outer rows
// of a three-row box are its corners) the stroke began beside a `└`, which
// admits up and right and nothing from the left. The line dead-ended one
// cell short of the node it came from, and nothing said so.
func TestALeavingEdgeJoinsItsWall(t *testing.T) {
	// b's tail edge back to a has to leave b and cross the whole drawing,
	// which is what pushes its port onto a border row.
	src := "digraph { rankdir=LR; a -> b; a -> c; c -> d; d -> b; b -> a }\n"
	l, h, ok := fit(src, 100, 40)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, 100, h)
	if rows == nil {
		t.Fatal("nothing drew")
	}
	joined := strings.Join(rows, "\n")

	// No wall may be a plain corner with a line running into its blind
	// side. Concretely: a horizontal run must never terminate against the
	// left of a `└` or `┌`, which is exactly the dead end this fixes.
	for _, bad := range []string{"─└", "─┌"} {
		if strings.Contains(joined, bad) {
			t.Errorf("a line dead-ends against a corner (%q):\n%s", bad, joined)
		}
	}
	if !strings.ContainsAny(joined, "├┤┬┴") {
		t.Errorf("no edge joined a wall at all; the fix is not reachable here:\n%s", joined)
	}
}

// A cell is a rune plus the zero-width marks that follow it. The canvas
// did not know that, and `putStr` skipped every zero-width rune it was
// handed, so a label written decomposed ("a" + U+0301) drew as a bare
// "accent": right width, wrong word, and nothing anywhere said so.
func TestDiagramLabelKeepsItsCombiningMarks(t *testing.T) {
	const decomposed = "áccent" // á, spelled as base + mark
	l, h, ok := fit("digraph { rankdir=LR\n x [label=\""+decomposed+"\"]\n x -> y\n}", 100, 40)
	if !ok {
		t.Fatal("the graph did not lay out")
	}
	rows := renderDiagram(l, 100, h)
	if rows == nil {
		t.Fatal("nothing drew")
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, decomposed) {
		t.Errorf("the mark was dropped; label rendered without it:\n%s", joined)
	}
	// The mark must not have taken a column of its own: the box is sized in
	// cells, and a cluster is one cell.
	if !strings.Contains(joined, "╭──────╮") {
		t.Errorf("box is not six cells wide, so the mark stole a column:\n%s", joined)
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

// ---------- routed drawings ----------
//
// Beauty as laws, not vibes. Each property below is what "drawn on
// purpose" means, written where it can go red: a chain is straight, an
// arrowhead meets a flat wall, a label rides its own stroke, a loop is
// attached or absent, and the same source is the same picture every
// time. The corpus is the set of shapes that used to come out scrappy.

const corpusTB = `digraph {
  parse -> check -> lower
  check -> warn
  lower -> emit
  warn -> emit
  emit -> link
}`

const corpusLR = `digraph { rankdir=LR
  a -> b
  b -> b
  b -> c [label="retry"]
  a -> c
}`

func renderOf(t *testing.T, src string, w int) []string {
	t.Helper()
	l, h, ok := fit(src+"\n", w, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := renderDiagram(l, w, h)
	if rows == nil {
		t.Fatal("render failed")
	}
	return rows
}

// A chain renders as one straight line of arrows: three drawn rows and
// not one more, because every bend would cost the reader a row.
func TestChainRendersStraight(t *testing.T) {
	rows := renderOf(t, "digraph { rankdir=LR; alpha -> beta -> gamma }", 100)
	drawn := 0
	arrows := 0
	for _, r := range rows {
		if strings.TrimSpace(r) == "" {
			continue
		}
		drawn++
		arrows += strings.Count(r, "▶")
	}
	if drawn != 3 {
		t.Errorf("a chain should be one box-row: %d drawn rows\n%s", drawn, strings.Join(rows, "\n"))
	}
	if arrows != 2 {
		t.Errorf("want 2 arrowheads on the straight line, got %d", arrows)
	}
}

// An arrowhead only ever meets a flat wall — never a corner, never air.
// This is the law the projection era could not keep.
func TestArrowheadsMeetWalls(t *testing.T) {
	for _, src := range []string{corpusTB, corpusLR} {
		rows := renderOf(t, src, 100)
		grid := make([][]rune, len(rows))
		width := 0
		for i, r := range rows {
			grid[i] = []rune(r)
			if len(grid[i]) > width {
				width = len(grid[i])
			}
		}
		at := func(x, y int) rune {
			if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
				return ' '
			}
			return grid[y][x]
		}
		for y := range grid {
			for x, r := range grid[y] {
				switch r {
				case '▶':
					if n := at(x+1, y); n != '│' && n != '┤' {
						t.Errorf("▶ at %d,%d meets %q, not a wall\n%s", x, y, n, strings.Join(rows, "\n"))
					}
				case '◀':
					if n := at(x-1, y); n != '│' && n != '├' {
						t.Errorf("◀ at %d,%d meets %q, not a wall\n%s", x, y, n, strings.Join(rows, "\n"))
					}
				case '▼':
					if n := at(x, y+1); n != '─' && n != '┬' {
						t.Errorf("▼ at %d,%d meets %q, not a wall\n%s", x, y, n, strings.Join(rows, "\n"))
					}
				case '▲':
					if n := at(x, y-1); n != '─' && n != '┴' {
						t.Errorf("▲ at %d,%d meets %q, not a wall\n%s", x, y, n, strings.Join(rows, "\n"))
					}
				}
			}
		}
	}
}

// A label rides inside its own stroke — ──go──▶ — so it can only belong
// to one edge.
func TestInlineLabelRidesItsStroke(t *testing.T) {
	rows := renderOf(t, `digraph { rankdir=LR; a -> b [label="go"] }`, 100)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "─go─") {
		t.Errorf("label does not ride its stroke:\n%s", joined)
	}
}

// A self-loop is attached — foot joined into the border, arrow meeting
// the top — or absent when the box has no room. Never a floating hook.
func TestSelfLoopAttachedOrAbsent(t *testing.T) {
	rows := renderOf(t, "digraph { rankdir=LR; loopy -> loopy }", 100)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "▼") || !strings.Contains(joined, "┴") {
		t.Errorf("wide self-loop not attached:\n%s", joined)
	}
	rows = renderOf(t, "digraph { rankdir=LR; b -> b }", 100)
	joined = strings.Join(rows, "\n")
	if strings.ContainsAny(joined, "▼▲◀▶") {
		t.Errorf("narrow self-loop should be absent, not meaningless:\n%s", joined)
	}
}

// The same source is the same picture, every frame, every session.
func TestRenderIsDeterministic(t *testing.T) {
	a := strings.Join(renderOf(t, corpusTB, 100), "\n")
	b := strings.Join(renderOf(t, corpusTB, 100), "\n")
	if a != b {
		t.Error("two renders of one source disagree")
	}
}

// One glyph grammar: ╭ is always a box, ┌ is always an edge bend. The
// reader never pays to tell a wall from a turn.
func TestCornersNameTheirOwner(t *testing.T) {
	rows := renderOf(t, corpusTB, 100)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "╭") || !strings.Contains(joined, "╰") {
		t.Errorf("boxes lost their rounded voice:\n%s", joined)
	}
	// every box: rounded top-left has a wall directly beneath
	grid := strings.Split(joined, "\n")
	for y, r := range grid {
		for x, g := range []rune(r) {
			if g != '╭' {
				continue
			}
			if y+1 >= len(grid) || x >= len([]rune(grid[y+1])) || []rune(grid[y+1])[x] != '│' {
				t.Errorf("╭ at %d,%d is not a box corner\n%s", x, y, joined)
			}
		}
	}
}

// Source that will not draw used to look exactly like source somebody
// wanted to read. A window three columns too narrow and a graph with a
// typo in it were the same picture, and neither said so.
func TestAFenceThatWillNotDrawSaysWhy(t *testing.T) {
	cases := []struct {
		name, src   string
		w, region   int
		wantNumbers bool
	}{
		{"too wide", "digraph { rankdir=LR; alpha -> beta -> gamma -> delta }", 26, 6, true},
		{"will not parse", "digraph { a -> ", 90, 8, false},
	}
	for _, c := range cases {
		rows := DrawNotice(CutReason(c.src, c.w, c.region), c.w, c.region)
		if rows == nil {
			t.Fatalf("%s: nothing drawn, and the reader learns nothing", c.name)
		}
		if len(rows) > c.region {
			t.Fatalf("%s: notice is %d rows in a %d-row region", c.name, len(rows), c.region)
		}
		for _, r := range rows {
			if textCells(r) > c.w {
				t.Fatalf("%s: notice is %d cells wide in %d columns: %q",
					c.name, textCells(r), c.w, r)
			}
		}
		joined := strings.Join(rows, " ")
		if c.wantNumbers && !strings.ContainsAny(joined, "0123456789") {
			t.Fatalf("%s: notice carries no measurement: %q", c.name, joined)
		}
	}
}

// A notice too small to read is worse than the source it would cover, so
// there is a floor below which nothing is drawn at all.
func TestATinyRegionKeepsItsSource(t *testing.T) {
	if rows := DrawNotice("needs 44 columns, this window has 12", 12, 6); rows != nil {
		t.Fatalf("drew a notice into 12 columns: %q", rows)
	}
	if rows := DrawNotice("needs 44 columns", 60, 2); rows != nil {
		t.Fatalf("drew a notice into 2 rows: %q", rows)
	}
}

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
