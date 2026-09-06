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

	// A cluster's name is set into the top edge, and the frame's own
	// line comes out from under it — a name with a rule struck through
	// it is not a name.
	cw, _ := Size("group", nil, 0)
	frame := New(cw+2, 4)
	frame.Box(Box{X: 0, Y: 0, W: cw, H: 4, Pencil: Pencil{Style: Dashed}, Title: "group"})
	top := plain(frame.Rows())[0]
	if !strings.Contains(top, " group ") {
		t.Errorf("the cluster's name is not in its top edge: %q", top)
	}
	if strings.Contains(top, "─group") || strings.Contains(top, "┄group") {
		t.Errorf("the frame's line runs through its own name: %q", top)
	}

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
	l, h, ok := layout.Fit(t.Context(), "digraph { rankdir=LR; \"käyttö\" -> \"sivu\" }\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	for _, r := range plain(Draw(l, 100, h)) {
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
	l, h, ok := layout.Fit(t.Context(), `digraph { rankdir=LR; "日本語" -> "ok" }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := plain(Draw(l, 100, h))
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
	l, h, ok := layout.Fit(t.Context(), `digraph { rankdir=LR; A[label="a\nb"]; A -> B }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	for _, r := range plain(Draw(l, 100, h)) {
		if strings.Contains(r, "anb") {
			t.Errorf("escape eaten, invented a word: %q", r)
		}
	}
}

// The field count says whether an edge carries a label; asking whether the
// text looks like a number dropped every numeric one. graphviz had already
// answered by how many fields it wrote.
func TestNumericEdgeLabelDraws(t *testing.T) {
	l, h, ok := layout.Fit(t.Context(), `digraph { rankdir=LR; A -> B [label="42"] }`+"\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	if !strings.Contains(joined(Draw(l, 100, h)), "42") {
		t.Errorf("numeric edge label not drawn:\n%s", joined(Draw(l, 100, h)))
	}
}

// An arrow meets the box it points at. graphviz stops a spline short to
// leave room for an arrowhead it expected to draw itself, and taking that
// end literally left a cell of white between every arrow and its target.
// The boxes are ours; where they are is not something to infer.
func TestArrowMeetsItsBox(t *testing.T) {
	l, h, ok := layout.Fit(t.Context(), "digraph { rankdir=LR; wire -> grid -> paint }\n", 100, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	j := joined(Draw(l, 100, h))
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
	l, h, ok := layout.Fit(t.Context(), src, 100, 40)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := Draw(l, 100, h)
	if rows == nil {
		t.Fatal("nothing drew")
	}
	j := joined(rows)

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
	l, h, ok := layout.Fit(t.Context(), "digraph { rankdir=LR\n x [label=\""+decomposed+"\"]\n x -> y\n}", 100, 40)
	if !ok {
		t.Fatal("the graph did not lay out")
	}
	rows := Draw(l, 100, h)
	if rows == nil {
		t.Fatal("nothing drew")
	}
	j := joined(rows)
	if !strings.Contains(j, decomposed) {
		t.Errorf("the mark was dropped; label rendered without it:\n%s", j)
	}
	// The mark must not have taken a column of its own: the box is sized in
	// cells, and a cluster is one cell.
	if !strings.Contains(j, "╭──────╮") {
		t.Errorf("box is not six cells wide, so the mark stole a column:\n%s", j)
	}
}

// The axis a drawing flows along is the layout's answer, not a guess from
// where the nodes landed: a top-down tree wider than it is tall used to
// read as left-right and collapse onto its root's row.
func TestTopDownTreeKeepsItsRanks(t *testing.T) {
	src := "digraph { rankdir=TB; root -> parser; root -> checker; root -> emitter; parser -> lexer; parser -> ast; checker -> types; checker -> scopes; emitter -> ir; emitter -> asm }\n"
	l, h, ok := layout.Fit(t.Context(), src, 116, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := plain(Draw(l, 116, h))
	rowOf := func(label string) int {
		for i, r := range rows {
			for _, w := range []string{"│" + label + "│", "│" + label + "├", "┤" + label + "│", "┤" + label + "├"} {
				if strings.Contains(r, w) {
					return i
				}
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
	l, h, ok := layout.Fit(t.Context(), "graph { rankdir=LR; a -- b -- c }\n", 80, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	if j := joined(Draw(l, 80, h)); strings.ContainsAny(j, "▶◀▲▼") {
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
	l, h, ok := layout.Fit(t.Context(), src+"\n", w, 0)
	if !ok {
		t.Fatal("layout failed")
	}
	rows := Draw(l, w, h)
	if rows == nil {
		t.Fatal("render failed")
	}
	return plain(rows)
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

// A label rides inside its own stroke — ──go──▶ — so it can only belong
// to one edge.
func TestInlineLabelRidesItsStroke(t *testing.T) {
	rows := renderOf(t, `digraph { rankdir=LR; a -> b [label="go"] }`, 100)
	if j := strings.Join(rows, "\n"); !strings.Contains(j, "─go─") {
		t.Errorf("label does not ride its stroke:\n%s", j)
	}
}

// A self-loop is attached — foot joined into the border, arrow meeting
// the top — or absent when the box has no room. Never a floating hook.
func TestSelfLoopAttachedOrAbsent(t *testing.T) {
	j := strings.Join(renderOf(t, "digraph { rankdir=LR; loopy -> loopy }", 100), "\n")
	if !strings.Contains(j, "▼") || !strings.Contains(j, "┴") {
		t.Errorf("wide self-loop not attached:\n%s", j)
	}
	j = strings.Join(renderOf(t, "digraph { rankdir=LR; b -> b }", 100), "\n")
	if strings.ContainsAny(j, "▼▲◀▶") {
		t.Errorf("narrow self-loop should be absent, not meaningless:\n%s", j)
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
