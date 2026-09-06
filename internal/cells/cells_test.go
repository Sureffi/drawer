// cells_test.go — laws for the cells rung, one case per rule written into
// the code.
//
// These exist to show that each design actually kills the class of bug it
// claims to; the real oracles are the recorded delta streams (-deltas) and
// the drawings themselves (-dot), and scripts/check.sh runs all of it
// together.
//
// The canvas laws come first and the drawing's laws after, because the
// canvas is the part a layout is written against: a candidate reads the
// first half of this file to learn what it may draw with.

package cells

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// plain is a drawing with its colour taken off. Every law below that
// reads glyphs reads them through this: a row is glyphs plus escapes,
// and a law about glyphs that trips over an escape is testing the wrong
// thing.
func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = grid.StripSGR(r)
	}
	return out
}

// drawn is the whole path a law below tests: the source read through the
// one door, laid out, and drawn. A test that cannot get a drawing has
// nothing to say about one.
func drawn(t *testing.T, src string, w int) []string {
	t.Helper()
	g, err := layout.Read(t.Context(), src)
	if err != nil {
		t.Fatalf("the source did not read: %v", err)
	}
	rows := Draw(g, w, 0)
	if rows == nil {
		t.Fatal("nothing drew")
	}
	return rows
}

func joined(rows []string) string { return strings.Join(plain(rows), "\n") }

// ---------- the vocabulary ----------

// styles is every value an arm can have, none included, in the order
// the walk below reads them.
var styles = []struct {
	name string
	s    Style
	ch   byte
	on   bool
}{
	{"none", 0, '.', false},
	{"light", Light, 'l', true},
	{"dashed", Dashed, ':', true},
	{"heavy", Heavy, 'h', true},
	{"double", Double, '=', true},
}

// armsOf reads a glyph back as the arms it draws. Built from the
// vocabulary's own entries of two arms or more — a one-armed cell is
// drawn as the whole line it continues, so those entries share their
// glyphs with the lines and cannot name them.
var armsOf = func() map[rune]string {
	m := map[rune]string{}
	for k, r := range vocabulary {
		n := 0
		for i := 0; i < 4; i++ {
			if k[i] != '.' {
				n++
			}
		}
		if n < 2 {
			continue
		}
		m[r] = k
	}
	return m
}()

// The table is the vocabulary, so the table has to be readable as one:
// four characters from one alphabet, no key twice, and no glyph twice
// among the entries that name their own arms.
func TestVocabularyIsWellFormed(t *testing.T) {
	seen := map[rune]string{}
	for k, r := range vocabulary {
		if _, ok := maskOf(k); !ok {
			t.Errorf("key %q is not four arms from \".l:h=\"", k)
			continue
		}
		n := 0
		for i := 0; i < 4; i++ {
			if k[i] != '.' {
				n++
			}
		}
		if n < 2 {
			continue
		}
		if other, dup := seen[r]; dup {
			t.Errorf("glyph %q draws both %q and %q", r, other, k)
		}
		seen[r] = k
	}
	if len(lut) != len(vocabulary) {
		t.Errorf("the compiled table lost entries: %d of %d", len(lut), len(vocabulary))
	}
}

// The whole table, walked: every combination of four arms and five
// styles, rounded and not. A mask always resolves to a glyph; the glyph
// always draws exactly the arms that were asked for; and where the
// block cannot say the styles exactly, every arm falls to the heaviest
// style the cell asked for and never past it. Nothing is ever dropped
// and nothing is ever invented.
func TestGlyphWalksTheWholeTable(t *testing.T) {
	weight := func(c byte) int {
		switch c {
		case 'h':
			return 2
		case '=':
			return 3
		case '.':
			return 0
		}
		return 1
	}
	for _, n := range styles {
		for _, e := range styles {
			for _, s := range styles {
				for _, w := range styles {
					want := string([]byte{n.ch, e.ch, s.ch, w.ch})
					var m Mask
					for i, st := range []struct {
						s  Style
						on bool
					}{{n.s, n.on}, {e.s, e.on}, {s.s, s.on}, {w.s, w.on}} {
						if st.on {
							m = m.With(Dir(i), st.s)
						}
					}
					arms := 0
					top := 0
					for i := 0; i < 4; i++ {
						if want[i] != '.' {
							arms++
							if weight(want[i]) > top {
								top = weight(want[i])
							}
						}
					}
					g := Glyph(m)
					if arms == 0 {
						if g != ' ' {
							t.Fatalf("%q: a cell with no arms is air, got %q", want, g)
						}
						continue
					}
					if g == 0 {
						t.Fatalf("%q: no glyph at all", want)
					}
					if arms == 1 {
						continue // walked by its own law below
					}
					got, ok := armsOf[g]
					if !ok {
						t.Fatalf("%q drew %q, which is not in the vocabulary", want, g)
					}
					for i := 0; i < 4; i++ {
						if (got[i] == '.') != (want[i] == '.') {
							t.Fatalf("%q drew %q (%q): the arms are not the ones asked for", want, g, got)
						}
						if got[i] == '.' {
							continue
						}
						if weight(got[i]) < weight(want[i]) || weight(got[i]) > top {
							t.Errorf("%q drew %q (%q): side %d fell out of range", want, g, got, i)
						}
					}
				}
			}
		}
	}
}

// The light-and-heavy half of the block is complete, so nothing in it
// ever falls: every one of those masks is drawn exactly as it was asked
// for. This is what makes penwidth a thing the drawing can actually say.
func TestLightAndHeavyNeverFall(t *testing.T) {
	for _, ns := range []byte{'.', 'l', 'h'} {
		for _, es := range []byte{'.', 'l', 'h'} {
			for _, ss := range []byte{'.', 'l', 'h'} {
				for _, ws := range []byte{'.', 'l', 'h'} {
					k := string([]byte{ns, es, ss, ws})
					m, _ := maskOf(k)
					if m == 0 {
						continue
					}
					if _, ok := lut[m]; !ok {
						t.Errorf("%q has no glyph of its own", k)
					}
				}
			}
		}
	}
}

// A cell with one arm reads as the whole line it continues. ╴ ╵ ╶ ╷
// exist, and they read as damage: a run that starts nowhere should
// still look like a line.
func TestOneArmReadsAsItsLine(t *testing.T) {
	for _, c := range []struct {
		d Dir
		s Style
		r rune
	}{
		{North, Light, '│'}, {South, Light, '│'}, {East, Light, '─'}, {West, Light, '─'},
		{North, Heavy, '┃'}, {East, Heavy, '━'},
		{North, Double, '║'}, {East, Double, '═'},
		{North, Dashed, '┆'}, {East, Dashed, '┄'},
	} {
		if g := Glyph(Mask(0).With(c.d, c.s)); g != c.r {
			t.Errorf("one %v arm on side %d drew %q, want %q", c.s, c.d, g, c.r)
		}
	}
}

// Two light lines crossing are ┼ and nothing else. The reader has one
// glyph to learn for "these two do not meet".
func TestCrossingIsAlwaysThatOneGlyph(t *testing.T) {
	var m Mask
	for d := North; d <= West; d++ {
		m = m.With(d, Light)
	}
	if g := Glyph(m); g != '┼' {
		t.Errorf("a light crossing drew %q, want ┼", g)
	}
}

// Rounding is the box's voice and only the box's: ╭ where the arms make
// a light corner, and nowhere else. A junction has no rounded glyph and
// neither has a heavy corner, so both keep the shape they had — a wall
// that gains a line is still a wall.
func TestRoundingOnlyEverSoftensALightCorner(t *testing.T) {
	corner := Mask(0).With(East, Light).With(South, Light)
	if g := Glyph(corner.Round()); g != '╭' {
		t.Errorf("a rounded light corner drew %q, want ╭", g)
	}
	heavy := Mask(0).With(East, Heavy).With(South, Heavy)
	if g := Glyph(heavy.Round()); g != '┏' {
		t.Errorf("a rounded heavy corner drew %q, want ┏", g)
	}
	tee := corner.With(North, Light)
	if g := Glyph(tee.Round()); g != '├' {
		t.Errorf("a rounded corner that gained an arm drew %q, want ├", g)
	}
}

// Styles that cannot share a cell fall to the heavier, together. A
// double arm across from a light one has no glyph of its own, so the
// whole cell speaks double: every arm still there, one weight up.
func TestMixedStylesFallToTheHeavier(t *testing.T) {
	for _, c := range []struct {
		m    Mask
		want rune
		why  string
	}{
		{Mask(0).With(North, Double).With(South, Light).With(East, Light), '╠',
			"double meeting light down one axis"},
		{Mask(0).With(East, Heavy).With(North, Double), '╚',
			"heavy meeting double"},
		{Mask(0).With(East, Dashed).With(South, Dashed), '┌',
			"dashed has no corner, so the corner is solid"},
		{Mask(0).With(East, Dashed).With(West, Light), '─',
			"a dashed arm and a solid one make a solid line"},
		{Mask(0).With(East, Dashed).With(West, Dashed), '┄',
			"a dashed line all the way through stays dashed"},
		{Mask(0).With(North, Light).With(South, Light).With(East, Double), '╞',
			"a double arm on a light axis is drawn exactly"},
	} {
		if g := Glyph(c.m); g != c.want {
			t.Errorf("%s: drew %q, want %q", c.why, g, c.want)
		}
	}
}

// ---------- the canvas ----------

// A line is never written as a glyph. Two strokes meeting are a corner,
// a tee or a crossing depending on nothing but what else arrives, and
// the cell that has to decide is the one both of them wrote to.
func TestArmsDecideTheGlyphAfterTheFact(t *testing.T) {
	cv := New(5, 3)
	cv.Stroke(0, 1, 4, 1, Pencil{})
	if g := joined(cv.Rows()); g != "─────" {
		t.Fatalf("a horizontal run drew %q", g)
	}
	cv.Stroke(2, 0, 2, 2, Pencil{})
	rows := plain(cv.Rows())
	if len(rows) != 3 || rows[1] != "──┼──" {
		t.Errorf("the second run should have turned one cell into a crossing:\n%s", strings.Join(rows, "\n"))
	}
}

// A run of one cell lays nothing. It is the turning point of the run
// that follows, and stamping a stray arm there is what used to make a
// straight vertical read as ┼┼┼.
func TestARunOfOneCellIsNotALine(t *testing.T) {
	cv := New(3, 3)
	cv.Stroke(1, 1, 1, 1, Pencil{})
	if g := joined(cv.Rows()); g != "" {
		t.Errorf("a run of one cell drew %q", g)
	}
}

// There is no diagonal in this vocabulary, so a run that is not
// straight draws nothing rather than something nobody asked for.
func TestADiagonalRunDrawsNothing(t *testing.T) {
	cv := New(4, 4)
	cv.Stroke(0, 0, 3, 3, Pencil{})
	if g := joined(cv.Rows()); g != "" {
		t.Errorf("a diagonal run drew %q", g)
	}
}

// The drawing is exactly the rows that were drawn on: never padded out
// to the canvas it was given, no blank row above or below it, and no
// trailing spaces on any row. A fence carries what this returns.
func TestRowsAreTheDrawingAndNothingElse(t *testing.T) {
	cv := New(40, 12)
	cv.Box(Box{X: 3, Y: 4, W: 7, H: 3, Round: true, Label: []string{"one"}})
	rows := cv.Rows()
	if len(rows) != 3 {
		t.Fatalf("want the box's own three rows, got %d:\n%s", len(rows), joined(rows))
	}
	for i, r := range plain(rows) {
		if strings.TrimRight(r, " ") != r {
			t.Errorf("row %d ends in air: %q", i, r)
		}
		if !strings.HasPrefix(r, "   ") {
			t.Errorf("row %d lost the box's left margin: %q", i, r)
		}
	}
}

// A wide glyph spends two columns and says so, so every row of a box
// carrying one is as wide as the box drawn around it. Measuring the
// label in cells and emitting it as rune-plus-space is what made a box
// right by the ruler and crooked on screen.
func TestWideTextSpendsBothItsColumns(t *testing.T) {
	cv := New(20, 3)
	cv.Box(Box{X: 0, Y: 0, W: 8, H: 3, Label: []string{"日本語"}})
	rows := plain(cv.Rows())
	w := runewidth.StringWidth(rows[0])
	for i, r := range rows {
		if runewidth.StringWidth(r) != w {
			t.Errorf("row %d is %d cells, the box is %d:\n%s", i,
				runewidth.StringWidth(r), w, strings.Join(rows, "\n"))
		}
	}
}

// A cell is a rune plus the zero-width marks that follow it. Skipping
// them drew a decomposed "á" as a bare accent: right width, wrong word,
// and nothing anywhere said so.
func TestCombiningMarksRideTheirGlyph(t *testing.T) {
	const decomposed = "áccent" // á, spelled as base + mark
	cv := New(20, 1)
	if n := cv.Text(0, 0, decomposed, 0); n != 6 {
		t.Errorf("a six-cell word measured %d", n)
	}
	if g := joined(cv.Rows()); g != decomposed {
		t.Errorf("the mark was dropped: %q", g)
	}
}

// A space written as text is a column of the words, not a hole in
// them: a line running under a label must not show through the gaps
// between its letters.
func TestAWrittenSpaceIsOpaque(t *testing.T) {
	cv := New(7, 1)
	cv.Stroke(0, 0, 6, 0, Pencil{})
	cv.Text(2, 0, "a b", 0)
	if g := joined(cv.Rows()); g != "──a b──" {
		t.Errorf("the stroke showed through the label: %q", g)
	}
}

// A head points somewhere, and the arm under it says a line ends here.
// The mark is invisible under the glyph; it is there so a router
// reading the canvas back sees an ending rather than a mark in air.
func TestAHeadPointsAndEnds(t *testing.T) {
	for _, c := range []struct {
		d Dir
		r rune
	}{{North, '▲'}, {East, '▶'}, {South, '▼'}, {West, '◀'}} {
		if Arrow(c.d) != c.r {
			t.Errorf("side %d points %q, want %q", c.d, Arrow(c.d), c.r)
		}
		cv := New(1, 1)
		cv.Head(0, 0, c.d, 0)
		if g := joined(cv.Rows()); g != string(c.r) {
			t.Errorf("a head on side %d drew %q", c.d, g)
		}
		if !cv.MaskAt(0, 0).Has(c.d) {
			t.Errorf("a head on side %d left no line ending", c.d)
		}
	}
}

// ---------- boxes ----------

// Every box kind, drawn: square corners, rounded corners, a cluster's
// frame with its name in the top edge, and a record ruled into fields.
func TestTheBoxKinds(t *testing.T) {
	square := New(6, 3)
	square.Box(Box{X: 0, Y: 0, W: 6, H: 3, Label: []string{"sq"}})
	if want := "┌────┐\n│ sq │\n└────┘"; joined(square.Rows()) != want {
		t.Errorf("square corners:\n%s", joined(square.Rows()))
	}

	round := New(6, 3)
	round.Box(Box{X: 0, Y: 0, W: 6, H: 3, Round: true, Label: []string{"rd"}})
	if want := "╭────╮\n│ rd │\n╰────╯"; joined(round.Rows()) != want {
		t.Errorf("rounded corners:\n%s", joined(round.Rows()))
	}

	// A cluster's frame and its name are drawn by drawFrame and nameFrame,
	// not here — see TestAClustersNameIsSetIntoItsTopEdge, which asks the
	// question of a real drawing.

	// A record is one box ruled into fields, and the rules join the
	// walls: a line trapped inside a box reads as a second box.
	rec := Box{X: 0, Y: 0, W: 9, H: 5}
	rc := New(9, 5)
	rc.Box(rec)
	rc.Divider(rec, 2, false)
	rc.Divider(rec, 4, true)
	rows := plain(rc.Rows())
	if rows[2] != "├───┼───┤" {
		t.Errorf("a record's dividers do not join its walls:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[0], "┌───┬") || !strings.HasPrefix(rows[4], "└───┴") {
		t.Errorf("a record's column rule does not join the edges:\n%s", strings.Join(rows, "\n"))
	}
}

// A box's walls are arms, not glyphs, so a line arriving at one becomes
// the junction that admits it. Written as glyphs, an edge meeting a
// wall dead-ended against it — and against a corner it dead-ended one
// cell short, because ╰ admits nothing from the left.
func TestALineJoinsTheWallItMeets(t *testing.T) {
	cv := New(12, 3)
	b := Box{X: 4, Y: 0, W: 6, H: 3, Round: true, Label: []string{"b"}}
	cv.Box(b)
	cv.Stroke(0, 1, 3, 1, Pencil{})
	cv.Line(4, 1, West, Pencil{}) // the wall gains the arm that admits it
	rows := plain(cv.Rows())
	if !strings.Contains(rows[1], "────┤") {
		t.Errorf("the line did not join the wall:\n%s", strings.Join(rows, "\n"))
	}
}

// ---------- colour ----------

// A run of cells in one pen costs one escape. Colour that repeats per
// cell is colour a terminal spends its whole budget on.
func TestOnePenIsOneEscape(t *testing.T) {
	cv := New(6, 1)
	cv.Stroke(0, 0, 5, 0, Pencil{Pen: RGB(0xff, 0x40, 0x00)})
	row := cv.Rows()[0]
	if n := strings.Count(row, "\x1b[38;2;"); n != 1 {
		t.Errorf("a six-cell run in one pen wrote %d colour escapes: %q", n, row)
	}
	if grid.StripSGR(row) != "──────" {
		t.Errorf("the colour ate the line: %q", grid.StripSGR(row))
	}
}

// Black is not a colour. #000000 is what graphviz writes when nobody
// asked, so it stays the default pen, and the default pen draws
// structure dim — structure recedes, content stands. A coloured stroke
// is painted, not dimmed: SGR 2 over a set foreground is the terminal's
// own business and several answer it by dropping the colour.
func TestTheDefaultPenIsTheDimOne(t *testing.T) {
	if ParsePen("#000000") != 0 {
		t.Error("graphviz's own black is not a colour anybody asked for")
	}
	if ParsePen("#ffffff00") != 0 {
		t.Error("an invisible fill is not a pen")
	}
	if ParsePen("cornflowerblue") != 0 {
		t.Error("a name the json never writes is not a pen")
	}
	if ParsePen("#010203") != RGB(1, 2, 3) {
		t.Error("a colour somebody did ask for went missing")
	}

	dim := New(4, 1)
	dim.Stroke(0, 0, 3, 0, Pencil{})
	if row := dim.Rows()[0]; !strings.HasPrefix(row, "\x1b[2m") || !strings.HasSuffix(row, "\x1b[22m") {
		t.Errorf("structure nobody coloured is not dim: %q", row)
	}

	lit := New(4, 1)
	lit.Stroke(0, 0, 3, 0, Pencil{Pen: RGB(9, 9, 9)})
	if strings.Contains(lit.Rows()[0], "\x1b[2m") {
		t.Errorf("a coloured stroke was dimmed: %q", lit.Rows()[0])
	}

	text := New(6, 1)
	text.Text(0, 0, "plain", 0)
	if row := text.Rows()[0]; row != "plain" {
		t.Errorf("uncoloured content costs escapes: %q", row)
	}
}

// ---------- the drawing ----------

// walled reports whether a label still has a left and right wall around
// it, across the cell of air a box keeps between its words and its walls.
// The wall is not always `│`: where an edge attaches, the correct
// box-drawing glyph is the T that admits it. Asserting the literal `│`
// made this case fail the moment leaving edges started joining their
// walls, which is a stricter thing than the law it is here to protect.
func walled(row, label string) bool {
	i := strings.Index(row, label)
	if i < 0 {
		return false
	}
	left := []rune(strings.TrimRight(row[:i], " "))
	right := []rune(strings.TrimLeft(row[i+len(label):], " "))
	isWall := func(r rune) bool { return r == '│' || r == '├' || r == '┤' }
	return len(left) > 0 && isWall(left[len(left)-1]) && len(right) > 0 && isWall(right[0])
}

// One ruler. Measuring a label in bytes and its box in runes drew the box
// one cell short and ate its own left border.
func TestLabelKeepsItsBox(t *testing.T) {
	for _, r := range plain(drawn(t, "digraph { rankdir=LR; \"käyttö\" -> \"sivu\" }\n", 100)) {
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
	rows := plain(drawn(t, `digraph { rankdir=LR; "日本語" -> "ok" }`+"\n", 100))
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
//
// Every place a label lands is asked, because for a while only one of them
// answered: a node's label came through labelLines, which unescapes, while
// an edge's words and a cluster's name were folded on their line breaks
// alone and kept the backslash — `Group \#0` printed with the backslash
// still in it, and the DOT said `Group #0`.
func TestLabelEscapesAreNotEaten(t *testing.T) {
	for _, c := range []struct{ what, src, want, never string }{
		{"a node's label",
			`digraph { rankdir=LR; A[label="a\\nb"]; A -> B }`, `a\nb`, "anb"},
		{"an edge's words",
			`digraph { rankdir=LR; A -> B [label="x\\ny"] }`, `x\ny`, "xny"},
		{"a cluster's name",
			`digraph { subgraph cluster0 { label="Group \#0"; A -> B } }`, "Group #0", `\#`},
	} {
		j := joined(drawn(t, c.src+"\n", 100))
		if !strings.Contains(j, c.want) {
			t.Errorf("%s: wanted %q on the page:\n%s", c.what, c.want, j)
		}
		if strings.Contains(j, c.never) {
			t.Errorf("%s: %q is on the page and nobody wrote it:\n%s", c.what, c.never, j)
		}
	}
}

// The field count says whether an edge carries a label; asking whether the
// text looks like a number dropped every numeric one. graphviz had already
// answered by how many fields it wrote.
func TestNumericEdgeLabelDraws(t *testing.T) {
	j := joined(drawn(t, `digraph { rankdir=LR; A -> B [label="42"] }`+"\n", 100))
	if !strings.Contains(j, "42") {
		t.Errorf("numeric edge label not drawn:\n%s", j)
	}
}

// An arrow meets the box it points at. graphviz stops a spline short to
// leave room for an arrowhead it expected to draw itself, and taking that
// end literally left a cell of white between every arrow and its target.
// The boxes are ours; where they are is not something to infer.
func TestArrowMeetsItsBox(t *testing.T) {
	j := joined(drawn(t, "digraph { rankdir=LR; wire -> grid -> paint }\n", 100))
	if strings.Contains(j, "▶ ") {
		t.Errorf("arrowhead left short of its box:\n%s", j)
	}
	if !strings.Contains(j, "▶│") {
		t.Errorf("no arrowhead met a box border:\n%s", j)
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
	j := joined(drawn(t, src, 100))

	// No wall may be a plain corner with a line running into its blind
	// side. Concretely: a horizontal run must never terminate against the
	// left of a `└` or `┌`, which is exactly the dead end this fixes.
	for _, bad := range []string{"─└", "─┌"} {
		if strings.Contains(j, bad) {
			t.Errorf("a line dead-ends against a corner (%q):\n%s", bad, j)
		}
	}
	if !strings.ContainsAny(j, "├┤┬┴") {
		t.Errorf("no edge joined a wall at all; the fix is not reachable here:\n%s", j)
	}
}

// A cell is a rune plus the zero-width marks that follow it, all the way
// out to a real drawing: a label written decomposed ("a" + U+0301) has to
// arrive with its accent and without a column for it.
func TestDiagramLabelKeepsItsCombiningMarks(t *testing.T) {
	const decomposed = "áccent" // á, spelled as base + mark
	j := joined(drawn(t, "digraph { rankdir=LR\n x [label=\""+decomposed+"\"]\n x -> y\n}", 100))
	if !strings.Contains(j, decomposed) {
		t.Errorf("the mark was dropped; label rendered without it:\n%s", j)
	}
	// The mark must not have taken a column of its own: the box is sized in
	// cells — six for the word, one of air each side, a wall each side —
	// and a cluster is one cell.
	if !strings.Contains(j, "╭────────╮") {
		t.Errorf("box is not eight cells wide, so the mark stole a column:\n%s", j)
	}
}

// The axis a drawing flows along is the layout's answer, not a guess from
// where the nodes landed: a top-down tree wider than it is tall used to
// read as left-right and collapse onto its root's row.
func TestTopDownTreeKeepsItsRanks(t *testing.T) {
	src := "digraph { rankdir=TB; root -> parser; root -> checker; root -> emitter; parser -> lexer; parser -> ast; checker -> types; checker -> scopes; emitter -> ir; emitter -> asm }\n"
	rows := plain(drawn(t, src, 116))
	rowOf := func(label string) int {
		for i, r := range rows {
			if walled(r, label) {
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
	if j := joined(drawn(t, "graph { rankdir=LR; a -- b -- c }\n", 80)); strings.ContainsAny(j, "▶◀▲▼") {
		t.Errorf("arrowheads on an undirected graph:\n%s", j)
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
	return plain(drawn(t, src+"\n", w))
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
		g := make([][]rune, len(rows))
		for i, r := range rows {
			g[i] = []rune(r)
		}
		at := func(x, y int) rune {
			if y < 0 || y >= len(g) || x < 0 || x >= len(g[y]) {
				return ' '
			}
			return g[y][x]
		}
		for y := range g {
			for x, r := range g[y] {
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

// A label rides inside its own stroke — ── go ──▶ — with a shoulder of
// air each side, so it can only belong to one edge and a reader can tell
// where the words stop.
func TestInlineLabelRidesItsStroke(t *testing.T) {
	rows := renderOf(t, `digraph { rankdir=LR; a -> b [label="go"] }`, 100)
	if j := strings.Join(rows, "\n"); !strings.Contains(j, "─ go ─") {
		t.Errorf("label does not ride its stroke:\n%s", j)
	}
}

// A self-loop is a hoop out of one wall and back into the same wall: a
// foot joined into the border, a head meeting it, and nothing hugging the
// box. The scout is no help here — its cheapest path from a wall to the
// wall beside it is a scribble — so this is the law that the shape is
// drawn outright.
func TestSelfLoopAttachedOrAbsent(t *testing.T) {
	for _, src := range []string{
		"digraph { rankdir=LR; loopy -> loopy }",
		"digraph { rankdir=LR; b -> b }",
	} {
		j := strings.Join(renderOf(t, src, 100), "\n")
		if !strings.Contains(j, "▼") || !strings.Contains(j, "┴") {
			t.Errorf("self-loop not attached:\n%s", j)
		}
	}
	// Several on one node take a wall each, or nest, and never meet.
	j := strings.Join(renderOf(t, "digraph { rankdir=LR; m -> m; m -> m }", 100), "\n")
	if n := strings.Count(j, "▼") + strings.Count(j, "▲"); n != 2 {
		t.Errorf("want two self-loops drawn, got %d heads:\n%s", n, j)
	}
	if strings.ContainsRune(j, '┼') {
		t.Errorf("two self-loops crossed each other:\n%s", j)
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
	j := strings.Join(rows, "\n")
	if !strings.Contains(j, "╭") || !strings.Contains(j, "╰") {
		t.Errorf("boxes lost their rounded voice:\n%s", j)
	}
	// every box: rounded top-left has a wall directly beneath
	for y, r := range rows {
		for x, g := range []rune(r) {
			if g != '╭' {
				continue
			}
			if y+1 >= len(rows) || x >= len([]rune(rows[y+1])) || []rune(rows[y+1])[x] != '│' {
				t.Errorf("╭ at %d,%d is not a box corner\n%s", x, y, j)
			}
		}
	}
}

// ---------- the chains layout's own laws ----------
//
// Everything below is a thing a reader has to be able to get back out of
// the drawing. They are written as questions about the glyphs because
// that is all a reader has: no drawing may say a thing the graph did not.

// grid reads a drawing back as a rectangle of runes, padded, colour off.
func gridOf(rows []string) [][]rune {
	p := plain(rows)
	w := 0
	for _, r := range p {
		if n := len([]rune(r)); n > w {
			w = n
		}
	}
	out := make([][]rune, len(p))
	for i, r := range p {
		line := []rune(r)
		out[i] = make([]rune, w)
		for j := range out[i] {
			if j < len(line) {
				out[i][j] = line[j]
			} else {
				out[i][j] = ' '
			}
		}
	}
	return out
}

// Two edges cross and never join. A reader takes one connected run of
// line cells as one edge, so a tee standing anywhere but on a wall is two
// edges fused into a shape neither of them is. Every junction in a
// drawing therefore has a box's border under it, and every meeting of two
// lines in the open is `┼`.
func TestLinesCrossAndNeverJoin(t *testing.T) {
	srcs := []string{
		corpusTB, corpusLR,
		"digraph { rankdir=LR; a -> b; a -> c; a -> d; b -> d; c -> d; d -> a }",
		"digraph { rankdir=TB; a -> b; b -> c; c -> a; a -> c; b -> a }",
		// A dashed box and a heavy one, because a box is drawn in the pen
		// it asked for and the wall this law looks for has to know all of
		// them. The alphabet here was the light runes alone, so the law
		// held for solid boxes and could not be pointed at any other kind
		// without going red on a correct drawing.
		"digraph { rankdir=LR; a [style=dashed]; b [style=bold]; a -> b; a -> c; c -> b }",
		"digraph { rankdir=TB; node [style=dashed]; a -> b; a -> c; b -> d; c -> d }",
	}
	tees := "├┤┬┴┝┞┟┠┡┢┥┦┧┨┩┪┭┮┯┰┱┲┵┶┷┸┹┺╞╟╠╡╢╣╤╥╦╧╨╩"
	for _, src := range srcs {
		g := gridOf(drawn(t, src+"\n", 120))
		wall := func(y, x int) bool {
			// a wall cell is one a box's own border passes through: it has
			// a corner or a straight run of the same box on either side —
			// in every pen a box is drawn in, light, dashed and heavy.
			return y >= 0 && y < len(g) && x >= 0 && x < len(g[y]) &&
				strings.ContainsRune("┌┐└┘╭╮╰╯│─┏┓┗┛┃━┄┆╌╎"+tees, g[y][x])
		}
		for y := range g {
			for x, r := range g[y] {
				if !strings.ContainsRune(tees, r) {
					continue
				}
				// A tee is legal only where a box's wall runs through it:
				// the two cells along the wall's own axis are wall too.
				horiz := r == '┬' || r == '┴'
				ok := false
				if horiz {
					ok = wall(y, x-1) && wall(y, x+1)
				} else {
					ok = wall(y-1, x) && wall(y+1, x)
				}
				if !ok {
					t.Errorf("a junction with no wall under it at %d,%d in\n%s", x, y, strings.Join(plain(drawn(t, src+"\n", 120)), "\n"))
				}
			}
		}
	}
}

// A cluster is a frame, its name is on it, and what is inside it is its
// members and nothing else.
func TestClusterIsAFrameRoundItsMembers(t *testing.T) {
	src := `digraph {
	  rankdir=LR
	  subgraph cluster_one { label="Inside:"; a; b }
	  outside
	  a -> b; b -> outside; outside -> a
	}`
	rows := plain(drawn(t, src+"\n", 120))
	j := strings.Join(rows, "\n")
	if !strings.Contains(j, "Inside:") {
		t.Fatalf("the cluster lost its name:\n%s", j)
	}
	// The frame is the square-cornered rectangle; the boxes are the
	// round-cornered ones. Find the frame, then ask what stands in it.
	top, left, right := -1, -1, -1
	for y, r := range rows {
		a := strings.Index(r, "┌")
		b := strings.LastIndex(r, "┐")
		if a >= 0 && b > a {
			top, left, right = y, a, b
			break
		}
	}
	if top < 0 {
		t.Fatalf("no frame drawn:\n%s", j)
	}
	in := func(word string) bool {
		for y := top; y < len(rows); y++ {
			r := []rune(rows[y])
			for k := 0; k+len([]rune(word)) <= len(r); k++ {
				if string(r[k:k+len([]rune(word))]) != word {
					continue
				}
				if k > left && k+len([]rune(word)) < right && walled(rows[y], word) {
					return true
				}
			}
		}
		return false
	}
	for _, w := range []string{"a", "b"} {
		if !in(w) {
			t.Errorf("member %q is not inside the frame:\n%s", w, j)
		}
	}
	if in("outside") {
		t.Errorf("a node that is not a member stands inside the frame:\n%s", j)
	}
}

// A line never crosses a frame. A reader stops at a border, so an edge
// drawn through one is an edge cut in half; it stops outside instead and
// the reader walks in across the blanks.
func TestNoLineCrossesAFrame(t *testing.T) {
	src := `digraph {
	  rankdir=TB
	  subgraph cluster_a { label="A"; p; q }
	  subgraph cluster_b { label="B"; r; s }
	  p -> q; q -> r; r -> s; s -> p
	}`
	rows := plain(drawn(t, src+"\n", 120))
	j := strings.Join(rows, "\n")
	// A frame's own corner runes are square; a node's are round. So any
	// square corner opens a frame, and every cell of that frame's four
	// walls has to be the wall's own rune, the name written into the top
	// edge, or a tee where a line stopped against it. A crossing there, or
	// a line rune lying across the wall's own axis, is a line gone through.
	//
	// The check used to sit inside `if the drawing has a ┼ in it`, and this
	// drawing has none, so it asserted nothing at all for a long time.
	if !strings.Contains(j, "╭") {
		t.Fatalf("want boxes as well as frames:\n%s", j)
	}
	g := gridOf(rows)
	frames := 0
	for y := range g {
		for x, r := range g[y] {
			if r != '┌' {
				continue
			}
			x1, y1 := -1, -1
			for c := x + 1; c < len(g[y]); c++ {
				if g[y][c] == '┐' {
					x1 = c
					break
				}
			}
			for r2 := y + 1; r2 < len(g); r2++ {
				if at(g, x, r2) == '└' {
					y1 = r2
					break
				}
			}
			if x1 < 0 || y1 < 0 || at(g, x1, y1) != '┘' {
				continue
			}
			frames++
			// Across the two horizontal walls: nothing that runs down the
			// page, and no crossing.
			for c := x + 1; c < x1; c++ {
				for _, r2 := range []int{y, y1} {
					if ch := at(g, c, r2); strings.ContainsRune("│┃┆╎┼╂┿╋", ch) {
						t.Errorf("a line crossed a frame's horizontal wall at %d,%d (%q):\n%s", c, r2, ch, j)
					}
				}
			}
			// And down the two vertical walls: nothing that runs across it.
			for r2 := y + 1; r2 < y1; r2++ {
				for _, c := range []int{x, x1} {
					if ch := at(g, c, r2); strings.ContainsRune("─━┄╌┼╂┿╋", ch) {
						t.Errorf("a line crossed a frame's vertical wall at %d,%d (%q):\n%s", c, r2, ch, j)
					}
				}
			}
		}
	}
	if frames < 2 {
		t.Fatalf("found %d frames to ask the question of, want the source's two:\n%s", frames, j)
	}
}

// A record is one box with its fields ruled off inside it.
func TestRecordIsOneBoxWithDividers(t *testing.T) {
	j := joined(drawn(t, `digraph { rankdir=LR; n [shape=record, label="<f0> id | name | age"]; n -> other }`+"\n", 120))
	for _, w := range []string{"id", "name", "age"} {
		if !strings.Contains(j, w) {
			t.Errorf("record field %q missing:\n%s", w, j)
		}
	}
	if !strings.Contains(j, "┬") || !strings.Contains(j, "┴") {
		t.Errorf("record has no dividers joined into its walls:\n%s", j)
	}
}

// The four things an edge's ends can say, and each drawn as itself.
func TestTheEndsSayWhatTheGraphSaid(t *testing.T) {
	heads := func(src string) int {
		j := joined(drawn(t, src+"\n", 100))
		n := 0
		for _, r := range "▶◀▲▼" {
			n += strings.Count(j, string(r))
		}
		return n
	}
	if n := heads(`digraph { rankdir=LR; a -> b }`); n != 1 {
		t.Errorf("a forward edge wants one head, got %d", n)
	}
	if n := heads(`digraph { rankdir=LR; a -> b [dir=none] }`); n != 0 {
		t.Errorf("dir=none wants no head, got %d", n)
	}
	if n := heads(`digraph { rankdir=LR; a -> b [arrowhead=none] }`); n != 0 {
		t.Errorf("arrowhead=none wants no head, got %d", n)
	}
	if n := heads(`digraph { rankdir=LR; a -> b [dir=both] }`); n != 2 {
		t.Errorf("dir=both wants two heads, got %d", n)
	}
	// A back edge points at the tail: the head is on the left of the
	// drawing, where `a` is, not on the right where `b` is.
	j := joined(drawn(t, `digraph { rankdir=LR; a -> b [dir=back] }`+"\n", 100))
	if !strings.Contains(j, "◀") {
		t.Errorf("dir=back did not turn its head round:\n%s", j)
	}
}

// A label a reader would take for a line never goes into one. `v` under a
// line is an arrowhead pointing down and the reader is right about that,
// so the words go somewhere the letter cannot be one.
func TestLabelIsNeverMistakenForTheDrawing(t *testing.T) {
	j := joined(drawn(t, `digraph { rankdir=LR; A -> B [label="very long edge label"] }`+"\n", 120))
	if !strings.Contains(j, "very long edge label") {
		t.Fatalf("label missing:\n%s", j)
	}
	// Set into the line, the run would read `─ very ...` — and a reader
	// gives the words back to the writing only when the writing begins
	// and ends with writing. `v` does not, so this one stands beside.
	if strings.Contains(j, "─ very long edge label ─") {
		t.Errorf("a label beginning with a line rune was set into its own line:\n%s", j)
	}
}

// A multi-line label grows its box rather than losing a line — and a
// record's field is a label, so its breaks count too. They did not: a
// field's `\n` and `\l` folded to spaces, so `+ speak()\l+ fetch()\l` came
// out as one run-on row and the width it cost turned whole drawings
// sideways to fit.
func TestMultiLineLabelGrowsItsBox(t *testing.T) {
	for _, c := range []struct{ what, src string }{
		{"a plain label",
			`digraph { rankdir=LR; a [label="one\ntwo\nthree"]; a -> b }`},
		{"a record's field",
			`digraph { rankdir=LR; a [shape=record, label="{x|one\ltwo\lthree\l}"]; a -> b }`},
	} {
		rows := plain(drawn(t, c.src+"\n", 100))
		for _, w := range []string{"one", "two", "three"} {
			found := false
			for _, r := range rows {
				if walled(r, w) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: line %q is not inside a box of its own:\n%s",
					c.what, w, strings.Join(rows, "\n"))
			}
		}
		// One per row, which is what a break is for: three lines on one
		// row is the fold this law exists to forbid.
		for _, r := range rows {
			n := 0
			for _, w := range []string{"one", "two", "three"} {
				if strings.Contains(r, w) {
					n++
				}
			}
			if n > 1 {
				t.Errorf("%s: %d of the label's lines came out on one row: %q", c.what, n, r)
			}
		}
	}
}

// ---------- the build organ's laws ----------
//
// The blueprint's build organ is five headings — lanes, boxes, edges,
// size and text — and every sentence under them is a law. What follows is
// those sentences, one test each, and every one of them has a fixture in
// the oracle's corpus under `own/` so the reader is asked the same
// question of a real drawing. A law without a fixture is not a law.

// armed is every glyph this rung draws, with the sides it can carry a
// line on: the arms of every cell the vocabulary draws as that glyph,
// taken together. It is how a law below asks the one question a reader
// asks of a cell — does a line leave here, that way — without a table of
// runes written out by hand and drifting from the one that draws them.
var armed = func() map[rune][4]bool {
	out := map[rune][4]bool{}
	add := func(m Mask) {
		if m == 0 {
			return
		}
		for _, g := range []rune{Glyph(m), Glyph(m.Round())} {
			a := out[g]
			for d := North; d <= West; d++ {
				a[d] = a[d] || m.Has(d)
			}
			out[g] = a
		}
	}
	for _, n := range styles {
		for _, e := range styles {
			for _, s := range styles {
				for _, w := range styles {
					var m Mask
					// `on` and not a zero style: Light is 0, so an arm
					// is there because the table says it is.
					for _, c := range []struct {
						d  Dir
						s  Style
						on bool
					}{{North, n.s, n.on}, {East, e.s, e.on}, {South, s.s, s.on}, {West, w.s, w.on}} {
						if c.on {
							m = m.With(c.d, c.s)
						}
					}
					add(m)
				}
			}
		}
	}
	return out
}()

// runeIndex is where a word starts in a row, counted in cells rather
// than bytes: a row of box drawing is three bytes to the glyph, and a
// byte index into one names a cell nobody meant.
func runeIndex(row []rune, word string) int {
	w := []rune(word)
	for i := 0; i+len(w) <= len(row); i++ {
		if string(row[i:i+len(w)]) == word {
			return i
		}
	}
	return -1
}

// arm reports whether the cell at x,y can carry a line leaving it on side
// d. Air, a letter and an arrowhead all answer no: an arrowhead is where
// a line stops, not a cell a line goes on through.
func arm(g [][]rune, x, y int, d Dir) bool {
	return armed[at(g, x, y)][d]
}

// at reads one cell of a drawing, air off the end.
func at(g [][]rune, x, y int) rune {
	if y < 0 || y >= len(g) || x < 0 || x >= len(g[y]) {
		return ' '
	}
	return g[y][x]
}

// LANES. A rank stands two or three cells off the next: room for a line
// and its head, and no more, because every cell between two boxes is a
// cell the reader's eye has to carry the join across. Two boxes never
// touch — a wall against a wall is one box with a rule through it.
//
// The exception is a label, and it is the whole exception: a label rides
// its own line, so the gap it rides through is the words plus a shoulder
// each side, the line stopping around them, and the head — six cells, and
// exactly six. Both halves are asked here. A three-node chain was the only
// graph this used to be asked of, which is the one shape where nothing
// widens a gap at all.
func TestRanksStandTwoOrThreeCellsApart(t *testing.T) {
	// Down the page: the boxes stack, so the gap is rows.
	for _, src := range []string{
		"digraph { rankdir=TB; a -> b -> c }",
		"digraph { rankdir=TB; a -> b; a -> c; b -> d; c -> d }",
		"digraph { rankdir=TB; r -> x; r -> y; r -> z }",
	} {
		rows := plain(drawn(t, src+"\n", 100))
		var tops []int
		for i, r := range rows {
			if strings.Contains(r, "╭") {
				tops = append(tops, i)
			}
		}
		if len(tops) < 2 {
			t.Fatalf("%s: want boxes on more than one row:\n%s", src, strings.Join(rows, "\n"))
		}
		for i := 1; i < len(tops); i++ {
			if gap := tops[i] - tops[i-1] - 3; gap < 2 || gap > 3 {
				t.Errorf("%s: top-down ranks stand %d rows apart, want 2 or 3:\n%s",
					src, gap, strings.Join(rows, "\n"))
			}
		}
	}
	// Across the page: the boxes march, so the gap is columns.
	across := plain(drawn(t, "digraph { rankdir=LR; a -> b -> c }\n", 100))
	var lefts []int
	for i, r := range []rune(across[0]) {
		if r == '╭' {
			lefts = append(lefts, i)
		}
	}
	if len(lefts) != 3 {
		t.Fatalf("want three boxes across the page, found %d:\n%s", len(lefts), strings.Join(across, "\n"))
	}
	for i := 1; i < len(lefts); i++ {
		if gap := lefts[i] - lefts[i-1] - 5; gap < 2 || gap > 3 {
			t.Errorf("left-right ranks stand %d columns apart, want 2 or 3:\n%s", gap, strings.Join(across, "\n"))
		}
	}
	// And the label's exception, to the cell. Three labels of three
	// lengths, so a gap that is merely generous fails as loudly as one
	// that is mean.
	for _, w := range []string{"x", "open", "close and flush"} {
		src := `digraph { rankdir=LR; a -> b [label="` + w + `"] }` + "\n"
		rows := plain(drawn(t, src, 200))
		var xs []int
		for i, r := range []rune(rows[0]) {
			if r == '╭' {
				xs = append(xs, i)
			}
		}
		if len(xs) != 2 {
			t.Fatalf("label %q: want two boxes on one row, found %d:\n%s", w, len(xs), strings.Join(rows, "\n"))
		}
		if gap, want := xs[1]-xs[0]-5, grid.Cells(w)+6; gap != want {
			t.Errorf("label %q rides a gap of %d columns, wants exactly %d:\n%s",
				w, gap, want, strings.Join(rows, "\n"))
		}
	}
}

// LANES. An arrowhead never sits on a junction. A head is where one line
// ends; a line running through the same cell would make the head belong
// to two edges at once, and a reader has no way to say which. So no line
// ever passes across a head: the cells either side of a head, on the axis
// its own line does not use, are never both line.
func TestAnArrowheadNeverSitsOnAJunction(t *testing.T) {
	srcs := []string{
		corpusTB, corpusLR,
		"digraph { rankdir=TB; a -> b; a -> c; a -> d; b -> e; c -> e; d -> e; e -> a }",
		"digraph { rankdir=LR; p -> q; q -> r; r -> p; p -> r; q -> p }",
	}
	for _, src := range srcs {
		g := gridOf(drawn(t, src+"\n", 120))
		for y := range g {
			for x, r := range g[y] {
				var through bool
				switch r {
				case '▶', '◀': // its own line runs across, so a crosser runs down
					through = arm(g, x, y-1, South) && arm(g, x, y+1, North)
				case '▲', '▼':
					through = arm(g, x-1, y, East) && arm(g, x+1, y, West)
				default:
					continue
				}
				if through {
					t.Errorf("a line runs through the arrowhead at %d,%d:\n%s",
						x, y, strings.Join(plain(drawn(t, src+"\n", 120)), "\n"))
				}
			}
		}
	}
}

// LANES. Where two edges cross, the cell is a crossing glyph and never a
// corner or a tee: `┼` where both are light, and the light-and-heavy
// crossing of its own weight where one of them is not. A corner there
// would be two edges turned into one bent line.
func TestACrossingIsACrossingAndNeverACorner(t *testing.T) {
	const crossings = "┼╋┽┾┿╀╁╂╃╄╅╆╇╈╉╊╪╫╬"
	srcs := []string{
		"digraph { rankdir=LR; a -> b; a -> c; b -> d; c -> d; a -> d; b -> c }",
		"digraph { rankdir=TB; a -> b; a -> c; b -> d; c -> d; a -> d; b -> c; c -> b }",
		"digraph { rankdir=TB; a -> b; a -> c; b -> d [style=bold]; c -> d; a -> d; b -> c }",
	}
	found := false
	for _, src := range srcs {
		g := gridOf(drawn(t, src+"\n", 120))
		for y := range g {
			for x, r := range g[y] {
				n := arm(g, x, y-1, South)
				s := arm(g, x, y+1, North)
				e := arm(g, x+1, y, West)
				w := arm(g, x-1, y, East)
				if !(n && s && e && w) {
					continue
				}
				// Four line neighbours and no box under it: this is either
				// a crossing or a corner that ate one.
				if strings.ContainsRune("╭╮╰╯┌┐└┘", r) {
					t.Errorf("two lines met as a corner %q at %d,%d:\n%s", r, x, y,
						strings.Join(plain(drawn(t, src+"\n", 120)), "\n"))
				}
				if strings.ContainsRune(crossings, r) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("no crossing drawn at all; this law is not reachable from these sources")
	}
}

// LANES. An edge owns its lane: two edges share a segment only where they
// share an end, and here they share no end at all, so they share nothing.
// A fan is the shape that provokes it — three edges into one box, three
// out of one box, and six doing both — and the way it fails is that two of
// them fuse into one line, which costs a head and grows a junction with no
// wall under it.
//
// The blueprint's first lane sentence had no test of its own; the fusion
// glyph was asked for by TestLinesCrossAndNeverJoin on other graphs, and
// the head count by TestTheEndsSayWhatTheGraphSaid on single edges.
func TestAnEdgeOwnsItsLane(t *testing.T) {
	tees := "├┤┬┴┝┞┟┠┡┢┥┦┧┨┩┪┭┮┯┰┱┲┵┶┷┸┹┺╞╟╠╡╢╣╤╥╦╧╨╩"
	walls := "┌┐└┘╭╮╰╯│─┏┓┗┛┃━┄┆╌╎" + tees
	for _, c := range []struct {
		src string
		n   int
	}{
		{"digraph { rankdir=LR; a -> b; a -> c; a -> d }", 3},
		{"digraph { rankdir=TB; a -> b; a -> c; a -> d }", 3},
		{"digraph { rankdir=LR; a -> d; b -> d; c -> d }", 3},
		{"digraph { rankdir=TB; a -> d; b -> d; c -> d }", 3},
		{"digraph { rankdir=LR; a -> e; b -> e; c -> e; a -> z; b -> z; c -> z }", 6},
	} {
		rows := plain(drawn(t, c.src+"\n", 160))
		j := strings.Join(rows, "\n")
		heads := 0
		for _, r := range "▶◀▲▼" {
			heads += strings.Count(j, string(r))
		}
		if heads != c.n {
			t.Errorf("%s: %d edges, %d heads — two of them fused:\n%s", c.src, c.n, heads, j)
		}
		// And nothing fused anywhere else either: a junction is legal only
		// where a box's own wall runs through it.
		g := gridOf(rows)
		wall := func(y, x int) bool {
			return strings.ContainsRune(walls, at(g, x, y))
		}
		for y := range g {
			for x, r := range g[y] {
				if !strings.ContainsRune(tees, r) {
					continue
				}
				ok := wall(y-1, x) && wall(y+1, x)
				if r == '┬' || r == '┴' {
					ok = wall(y, x-1) && wall(y, x+1)
				}
				if !ok {
					t.Errorf("%s: two edges met at %q at %d,%d:\n%s", c.src, string(r), x, y, j)
				}
			}
		}
	}
}

// LANES. Two edges between the same pair, one each way, are two lines
// with two heads pointing opposite ways. Sharing the corridor would draw
// one line with a head at each end, which is what `dir=both` means and
// not what the graph said.
func TestOppositeDirectionsNeverShareACorridor(t *testing.T) {
	heads := func(j string) int {
		n := 0
		for _, r := range "▶◀▲▼" {
			n += strings.Count(j, string(r))
		}
		return n
	}
	// One head per edge. Two edges folded into one corridor would draw
	// one line, and one line can only carry one head at each end.
	for _, c := range []struct {
		src string
		n   int
	}{
		{"digraph { rankdir=LR; a -> b; b -> a }", 2},
		{"digraph { rankdir=TB; a -> b; b -> a }", 2},
		{"digraph { rankdir=LR; a -> b; b -> a; a -> b }", 3},
		{"digraph { rankdir=LR; a -> b; b -> c; c -> b; b -> a }", 4},
	} {
		j := joined(drawn(t, c.src+"\n", 120))
		if n := heads(j); n != c.n {
			t.Errorf("%s wants %d heads, drew %d:\n%s", c.src, c.n, n, j)
		}
		// And never one bare run with a head at each end: that is what
		// `dir=both` draws, and neither of these graphs said it.
		//
		// Both ways up. The rows alone were asked for a long time, and the
		// runes they were asked with are horizontal, so the two top-down
		// cases in this table were held to their head count and nothing
		// else — and a shared vertical corridor with `▲` at one end and
		// `▼` at the other has exactly the two heads they wanted.
		if bothEnds.MatchString(j) {
			t.Errorf("%s drew one corridor with a head at each end:\n%s", c.src, j)
		}
		if bothEndsDown.MatchString(strings.Join(columns(plain(drawn(t, c.src+"\n", 120))), "\n")) {
			t.Errorf("%s drew one column with a head at each end:\n%s", c.src, j)
		}
	}
}

// bothEnds is one unbroken run of line with an arrowhead at each end —
// the shape two opposite edges make when they share a corridor, and the
// shape `dir=both` means. Across the page, and down it.
var bothEnds = regexp.MustCompile(`◀[─━┄╌]+▶|▶[─━┄╌]+◀`)
var bothEndsDown = regexp.MustCompile(`▲[│┃┆╎]+▼|▼[│┃┆╎]+▲`)

// columns is a drawing read down the page: column 0 as a string, then
// column 1, and so on, so a law about a run of cells can be asked of a
// column with the same regexp it asks of a row.
func columns(rows []string) []string {
	g := gridOf(rows)
	w := 0
	for _, r := range g {
		if len(r) > w {
			w = len(r)
		}
	}
	out := make([]string, w)
	for x := 0; x < w; x++ {
		var b strings.Builder
		for y := range g {
			b.WriteRune(at(g, x, y))
		}
		out[x] = b.String()
	}
	return out
}

// BOXES. A box is its label with one cell of air each side, and the
// walls outside that. Not two cells, not none: this is the width every
// column is measured from.
func TestABoxIsItsLabelAndACellOfAirEachSide(t *testing.T) {
	// One box to a column here, on purpose: a column comes out as wide as
	// its widest box, so a box sharing a column with a longer one is wider
	// than its own label — that is nodeBox's choice, stated there, and the
	// second half of this law asks for it below. The air each side is what
	// never varies.
	for _, w := range []string{"a", "node", "a longer name", "日本語"} {
		src := `digraph { rankdir=LR; "` + w + `" -> "tail" }` + "\n"
		rows := plain(drawn(t, src, 200))
		seen := false
		for _, r := range rows {
			i := strings.Index(r, w)
			if i < 0 {
				continue
			}
			seen = true
			left := []rune(r[:i])
			right := []rune(r[i+len(w):])
			if len(left) < 2 || left[len(left)-1] != ' ' ||
				!strings.ContainsRune("│├┤", left[len(left)-2]) {
				t.Errorf("%q has no cell of air and a wall on its left: %q", w, r)
			}
			if len(right) < 2 || right[0] != ' ' || !strings.ContainsRune("│├┤", right[1]) {
				t.Errorf("%q has no cell of air and a wall on its right: %q", w, r)
			}
		}
		if !seen {
			t.Errorf("label %q never drew:\n%s", w, strings.Join(rows, "\n"))
		}
	}
	// And in a column, every box in it comes out one width — the widest
	// one's. A column of walls that line up reads as a column, and every
	// extra cell of wall is a place for an edge to attach.
	rows := plain(drawn(t, `digraph { rankdir=LR; a -> z; "a longer name" -> z }`+"\n", 200))
	var tops []int
	for _, r := range rows {
		if i := strings.Index(r, "╭"); i >= 0 {
			if j := strings.Index(r[i:], "╮"); j > 0 {
				tops = append(tops, j)
			}
		}
	}
	if len(tops) < 2 {
		t.Fatalf("want two boxes in one column:\n%s", strings.Join(rows, "\n"))
	}
	for _, n := range tops[1:] {
		if n != tops[0] {
			t.Errorf("boxes in one column came out %v cells wide, want one width:\n%s",
				tops, strings.Join(rows, "\n"))
		}
	}
}

// BOXES. Every shape is some box. The round family — ellipse, circle and
// the rest, and the default, which is ellipse — draws with rounded
// corners; everything else is square. A shape this rung has never heard
// of still draws, because a graph that will not draw is the one failure
// this rung does not have.
func TestEveryShapeIsSomeBox(t *testing.T) {
	for _, c := range []struct {
		shape, style string
		corner       rune
	}{
		{"", "", '╭'}, {"ellipse", "", '╭'}, {"circle", "", '╭'}, {"oval", "", '╭'},
		{"doublecircle", "", '╭'}, {"diamond", "", '╭'},
		{"box", "", '┌'}, {"square", "", '┌'}, {"hexagon", "", '┌'}, {"cylinder", "", '┌'},
		{"nothing_graphviz_has_ever_drawn", "", '┌'},
		// `style=rounded` says the same thing about a box's corners that a
		// round shape says, and it is how a model writes a diagram. It was
		// read for dashed and bold only, so six of the corpus's own model
		// fixtures drew square where their source said round.
		{"box", "rounded", '╭'}, {"", "rounded", '╭'},
		{"box", "rounded,dashed", '╭'}, {"box", "filled", '┌'},
	} {
		src := `digraph { rankdir=LR; n [shape="` + c.shape + `", style="` + c.style + `", label="shape"]; n -> other }` + "\n"
		rows := plain(drawn(t, src, 120))
		if !strings.ContainsRune(rows[0], c.corner) {
			t.Errorf("shape %q style %q drew its top-left corner as %q, want %q:\n%s",
				c.shape, c.style, string([]rune(rows[0])[0]), string(c.corner), strings.Join(rows, "\n"))
		}
	}
}

// EDGES. The line is drawn in the weight the graph asked for: dashed for
// dashed and dotted, heavy for bold and for any pen two points or wider,
// light for everything else. The style is the line's own, not the
// drawing's — one heavy edge among light ones stays the only heavy one.
func TestTheLineIsDrawnInTheWeightTheGraphAsked(t *testing.T) {
	j := joined(drawn(t, `digraph { rankdir=LR
	  a -> b [style=dashed]
	  b -> c [style=dotted]
	  c -> d [style=bold]
	  d -> e [penwidth=3]
	  e -> f
	}`+"\n", 200))
	for _, c := range []struct{ want, why string }{
		{"┄", "dashed and dotted draw a dashed line"},
		{"━", "bold and a wide pen draw a heavy line"},
		{"─", "everything else draws a light line"},
	} {
		if !strings.Contains(j, c.want) {
			t.Errorf("%s: no %q anywhere:\n%s", c.why, c.want, j)
		}
	}
	// Four light cells is the plain edge and nothing else in this graph
	// has them, so the weights did not spread.
	if n := strings.Count(j, "┄"); n < 4 {
		t.Errorf("only %d dashed cells; the dashed edges are not dashed:\n%s", n, j)
	}
	if n := strings.Count(j, "━"); n < 4 {
		t.Errorf("only %d heavy cells; the heavy edges are not heavy:\n%s", n, j)
	}
}

// EDGES. Colour rides the line, the head and the border, and structure
// nobody coloured is dim. A pen named on an edge paints the whole run
// including the arrowhead — a head left in the default pen belongs to a
// different edge as far as the eye is concerned.
func TestColourRidesTheLineTheHeadAndTheBorder(t *testing.T) {
	rows := drawn(t, `digraph { rankdir=LR
	  a [color="#ff0000"]
	  a -> b [color="#00ff00"]
	}`+"\n", 100)
	const red, green = "\x1b[38;2;255;0;0m", "\x1b[38;2;0;255;0m"
	j := strings.Join(rows, "\n")
	if !strings.Contains(j, red) {
		t.Errorf("a node's own pen never reached its border:\n%q", j)
	}
	if !strings.Contains(j, green) {
		t.Errorf("an edge's own pen never reached its line:\n%q", j)
	}
	// The head is inside the edge's own run: between the escape that sets
	// green and the next escape that changes the pen.
	i := strings.Index(j, green)
	run := j[i+len(green):]
	if k := strings.Index(run, "\x1b"); k >= 0 {
		run = run[:k]
	}
	if !strings.Contains(run, "▶") {
		t.Errorf("the arrowhead is not in its edge's pen: run %q\n%q", run, j)
	}
	// And a graph that named no colour spends one escape on being dim.
	if plain := joined(drawn(t, "digraph { rankdir=LR; a -> b }\n", 100)); strings.Contains(plain, "\x1b") {
		t.Errorf("plain rows still carry escapes: %q", plain)
	}
	if lit := strings.Join(drawn(t, "digraph { rankdir=LR; a -> b }\n", 100), "\n"); !strings.Contains(lit, "\x1b[2m") {
		t.Errorf("structure nobody coloured is not dim: %q", lit)
	}
}

// SIZE. The drawing is its own rows: what comes back is the cells that
// were drawn on and nothing else — no blank row above or below, no
// trailing air, never padded out to the region it was given.
func TestTheDrawingIsItsOwnRowsAndNotTheWindows(t *testing.T) {
	g, err := layout.Read(t.Context(), "digraph { rankdir=LR; a -> b }\n")
	if err != nil {
		t.Fatal(err)
	}
	rows := Draw(g, 200, 200)
	if len(rows) != 3 {
		t.Fatalf("a two-box drawing is three rows, got %d:\n%s", len(rows), joined(rows))
	}
	for i, r := range plain(rows) {
		if strings.TrimSpace(r) == "" {
			t.Errorf("row %d is blank", i)
		}
		if strings.TrimRight(r, " ") != r {
			t.Errorf("row %d ends in air: %q", i, r)
		}
	}
}

// SIZE. A graph too wide for the window is turned rather than cut: the
// same graph comes back down the page, inside the width. This is the
// flip the pixels rung makes, made here.
func TestTooWideForTheWindowIsTurned(t *testing.T) {
	const src = "digraph { rankdir=LR; alpha -> beta -> gamma -> delta -> epsilon }\n"
	wide := plain(drawn(t, src, 200))
	if len(wide) != 3 {
		t.Fatalf("with room the chain is one box-row, got %d:\n%s", len(wide), strings.Join(wide, "\n"))
	}
	narrow := plain(drawn(t, src, 30))
	for i, r := range narrow {
		if n := runewidth.StringWidth(r); n > 30 {
			t.Errorf("row %d is %d cells in a 30-cell window: %q", i, n, r)
		}
	}
	if len(narrow) <= 3 {
		t.Errorf("the chain was not turned down the page:\n%s", strings.Join(narrow, "\n"))
	}
	for _, w := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		if !strings.Contains(strings.Join(narrow, "\n"), w) {
			t.Errorf("turning the drawing lost %q:\n%s", w, strings.Join(narrow, "\n"))
		}
	}
}

// SIZE. What will not fit any way up draws nothing, so the caller shows
// the source under a notice. A drawing that is short says so by not
// being there; it never comes back cut.
func TestWhatWillNotFitDrawsNothing(t *testing.T) {
	g, err := layout.Read(t.Context(), "digraph { rankdir=LR; alpha -> beta -> gamma -> delta }\n")
	if err != nil {
		t.Fatal(err)
	}
	if rows := Draw(g, 8, 100); rows != nil {
		t.Errorf("eight columns drew something:\n%s", joined(rows))
	}
	if rows := Draw(g, 200, 2); rows != nil {
		t.Errorf("two rows drew something:\n%s", joined(rows))
	}
}

// TEXT. A cluster's contents stand a cell inside its frame, so the frame
// and a box wall never read as one thick stroke.
func TestAClusterInsetsItsMembersOneCell(t *testing.T) {
	src := `digraph { rankdir=LR
	  subgraph cluster_one { label="group"; a; b }
	  a -> b
	}` + "\n"
	rows := plain(drawn(t, src, 120))
	j := strings.Join(rows, "\n")
	for y, r := range rows {
		run := []rune(r)
		for x, c := range run {
			if c != '┌' && c != '└' {
				continue
			}
			// The cell just inside the frame's corner is air, never a wall.
			dy := 1
			if c == '└' {
				dy = -1
			}
			if in := at(gridOf(rows), x+1, y+dy); in != ' ' {
				t.Errorf("the frame's corner at %d,%d has %q against it, not air:\n%s", x, y, in, j)
			}
		}
	}
}

// BOXES. A cluster is a frame with its label in the top edge — in the edge
// itself, between two stretches of the frame's own line, not on the air row
// under it. Nothing asserted that on a real drawing for a while: the two
// cluster tests that existed both named their cluster something with a
// colon in it, which is a rune the drawing spends on lines, so both took
// the air-row path and the edge path was drawn by nobody.
//
// The exception is the second case, and it is the whole of the exception:
// a name is read back off the edge, so a rune the edge is drawn with comes
// back as a blank, and a name holding one stands on the air row instead.
func TestAClustersNameIsSetIntoItsTopEdge(t *testing.T) {
	// `Services` is the case that used to fail for a plain letter: `v` is
	// an ascii arrowhead as well, and asking the alphabet alone barred it.
	for _, c := range []struct {
		what, name string
		inEdge     bool
	}{
		{"a plain name", "group", true},
		{"a name with a letter that is also a head", "Services", true},
		{"a name with a rune the drawing draws lines with", "namespace: prod", false},
	} {
		src := `digraph { rankdir=LR
		  subgraph cluster_one { label="` + c.name + `"; a; b }
		  a -> b
		}` + "\n"
		rows := plain(drawn(t, src, 120))
		j := strings.Join(rows, "\n")
		row := -1
		for y, r := range rows {
			if strings.Contains(r, c.name) {
				row = y
			}
		}
		if row < 0 {
			t.Errorf("%s: the cluster lost its name:\n%s", c.what, j)
			continue
		}
		// The top edge is the row the frame's own corners stand on.
		got := strings.Contains(rows[row], "┌") && strings.Contains(rows[row], "┐")
		if got != c.inEdge {
			where := "on the air row"
			if got {
				where = "in the top edge"
			}
			t.Errorf("%s: %q came out %s:\n%s", c.what, c.name, where, j)
		}
		if !got {
			continue
		}
		// In the edge means in it: the frame's line carries on either side
		// of the name, with one blank between.
		i := strings.Index(rows[row], c.name)
		r := []rune(rows[row][:i])
		if len(r) < 2 || r[len(r)-1] != ' ' || !strings.ContainsRune("─┬", r[len(r)-2]) {
			t.Errorf("%s: no edge and a blank before the name:\n%s", c.what, j)
		}
	}
}

// BOXES. A head that stops against a cluster's frame meets the frame and
// never the frame's own name. An edge into a cluster stops one cell
// outside it and the reader walks in across the border; a head landing
// against a letter of the name points at the name instead, so the name
// gives way to the lines and takes the stretch of edge that none of them
// crosses.
func TestNoHeadEverLandsOnAClustersName(t *testing.T) {
	for _, src := range []string{
		`digraph { subgraph cluster_a { label="inside"; m1 -> m2 }; start -> m1; m2 -> done }`,
		`digraph { subgraph cluster_o { label="outer"; subgraph cluster_i { label="inner"; i1 -> i2 }; o1 -> i1 }; start -> o1; i2 -> done }`,
		`digraph { rankdir=LR; subgraph cluster_f { label="far"; m1 -> m2 }; a -> m1; a -> z; z -> m1; m2 -> end }`,
	} {
		rows := plain(drawn(t, src+"\n", 160))
		g := gridOf(rows)
		for y := range g {
			for x, r := range g[y] {
				var tx, ty int
				switch r {
				case '▶':
					tx, ty = x+1, y
				case '◀':
					tx, ty = x-1, y
				case '▼':
					tx, ty = x, y+1
				case '▲':
					tx, ty = x, y-1
				default:
					continue
				}
				// Whatever a head points at is a wall, a frame's own line,
				// or the blank a frame keeps around its name. A letter is
				// none of those.
				if c := at(g, tx, ty); c != ' ' && !InAlphabet(c) {
					t.Errorf("the head at %d,%d points at %q:\n%s", x, y, c,
						strings.Join(rows, "\n"))
				}
			}
		}
	}
}

// BOXES. A cluster's name standing inside its frame keeps two cells of
// air on the sides a line comes from. One was not enough: a dashed
// edge that began the cell after "namespace: prod" stood close enough
// for the reader to take the frame's name for that edge's label, and the
// cluster came back nameless with an edge wearing its words.
func TestAClustersNameKeepsItsAir(t *testing.T) {
	const src = `digraph { rankdir=TB
	  node [shape=box, style=rounded]
	  subgraph cluster_ns { label="namespace: prod"
	    Ingress; Service; Deployment; Pod1 [label="Pod 1"]; Pod2 [label="Pod 2"]
	    ConfigMap; Secret }
	  Ingress -> Service; Service -> Pod1; Service -> Pod2
	  Deployment -> Pod1; Deployment -> Pod2
	  ConfigMap -> Pod1 [style=dashed]; Secret -> Pod1 [style=dashed] }` + "\n"
	for _, w := range []int{60, 80, 120} {
		g, err := layout.Read(t.Context(), src)
		if err != nil {
			t.Fatal(err)
		}
		rows := Draw(g, w, 120)
		if rows == nil {
			continue
		}
		gr := gridOf(plain(rows))
		const name = "namespace: prod"
		for y := range gr {
			i := runeIndex(gr[y], name)
			if i < 0 {
				continue
			}
			// Two cells of air each side on the name's own row. The
			// frame's own wall may stand in the second one — that is the
			// frame, and a reader never gives words to a wall.
			for _, x := range []int{i - 1, i - 2, i + len([]rune(name)), i + len([]rune(name)) + 1} {
				if c := at(gr, x, y); c != ' ' && c != '│' && c != '┆' && c != '┃' {
					t.Errorf("at %d columns the name has %q beside it at %d,%d:\n%s",
						w, c, x, y, strings.Join(plain(rows), "\n"))
				}
			}
		}
	}
}

// SIZE. Nothing short is ever drawn. A picture missing one edge says the
// two nodes it joined are not joined, with the same confidence as the
// rest of it, and there is nothing on the page for a reader to notice.
// So at every width the answer is the whole graph or no graph: one head
// per directed edge, every node inside a box, or nil and the notice.
func TestNothingShortIsEverDrawn(t *testing.T) {
	for _, c := range []struct {
		src   string
		nodes []string
		heads int
	}{
		{`digraph { rankdir=TB
		   subgraph cluster_ns { label="namespace: prod"
		     ingress; service; deploy; pod1; pod2; conf; secret }
		   ingress -> service; service -> pod1; service -> pod2
		   deploy -> pod1; deploy -> pod2
		   conf -> pod1 [style=dashed]; secret -> pod1 [style=dashed] }`,
			[]string{"ingress", "service", "deploy", "pod1", "pod2", "conf", "secret"}, 7},
		{`digraph { rankdir=LR
		   closed -> listen [label="passive open"]
		   listen -> syn [label="recv syn"]
		   syn -> est [label="recv ack"]
		   est -> closed [label="close"]
		   listen -> closed [label="close"] }`,
			[]string{"closed", "listen", "syn", "est"}, 5},
	} {
		for w := 40; w <= 200; w += 8 {
			g, err := layout.Read(t.Context(), c.src+"\n")
			if err != nil {
				t.Fatal(err)
			}
			rows := Draw(g, w, 120)
			if rows == nil {
				continue // the notice's job, and honest
			}
			j := strings.Join(plain(rows), "\n")
			n := 0
			for _, r := range "▶◀▲▼" {
				n += strings.Count(j, string(r))
			}
			if n != c.heads {
				t.Errorf("at %d columns the drawing carries %d heads for %d edges:\n%s",
					w, n, c.heads, j)
			}
			for _, name := range c.nodes {
				if !strings.Contains(j, name) {
					t.Errorf("at %d columns the drawing lost %q:\n%s", w, name, j)
				}
			}
		}
	}
}
