// cells.go — the cells rung's canvas: box-drawing characters, any terminal.
//
//	```dot
//	digraph { rankdir=LR; A -> B -> C }
//	```
//
// This is the surface every drawing on this rung is made on, and the
// whole of its vocabulary: lines with a style per side, boxes square or
// rounded or framed round a cluster or ruled into a record's fields,
// arrowheads, text laid by the terminal's own width rules, and a colour
// per run. What it does not have is an opinion about where anything
// goes — that is the layout's, and layouts are meant to be swapped.
// glyph.go turns a cell's arms into its glyph; chains.go, sheet.go,
// frame.go, scout.go and wire.go are one layout drawn here.
//
// The rule that outranks the rest: a drawing renders into the cell box
// it was measured for, and if it does not fit, the source shows under a
// notice. A failure is visible, never silent.

package cells

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/sureffi/drawer/internal/grid"
)

// ---------- the pen ----------

// A Pen is 0 for "whatever the terminal was already using", or
// 0x01rrggbb. The high bit is what lets black be a colour nobody asked
// for and 0x000001 still be a colour somebody did.
//
// Black is not a colour: #000000 is what graphviz writes when nobody
// said, so it stays the default pen — and the default pen draws
// structure dim. Structure recedes, content stands.
//
// Measured on the rig: a 24-bit foreground arrives exact outside a tmux
// pane and snapped to the xterm-256 cube inside one, and a line one step
// off is still that line, so the pen is written truecolor and left to the
// wire.
type Pen uint32

const penSet Pen = 0x01000000

// RGB is the pen for one colour.
func RGB(r, g, b uint8) Pen {
	return penSet | Pen(r)<<16 | Pen(g)<<8 | Pen(b)
}

// ParsePen reads one graphviz pen colour: `#rrggbb`, or `#rrggbbaa`
// where the graph asked for transparency. Anything else — a name the
// json never writes, an empty string — is the default pen.
func ParsePen(s string) Pen {
	if len(s) < 7 || s[0] != '#' {
		return 0
	}
	v, err := strconv.ParseUint(s[1:7], 16, 32)
	if err != nil {
		return 0
	}
	// Nearly transparent is nothing anybody meant to see; graphviz
	// writes `#ffffff00` for an invisible fill.
	if len(s) >= 9 {
		a, err := strconv.ParseUint(s[7:9], 16, 32)
		if err != nil || a < 0x20 {
			return 0
		}
	}
	if v == 0 { // graphviz's own black: the default pen, undeclared
		return 0
	}
	return penSet | Pen(v)
}

// A Pencil is what a line is drawn with. The zero pencil is a light
// line in the default pen, which is what most of a drawing is.
type Pencil struct {
	Style Style
	Pen   Pen
}

// ---------- the canvas ----------

// A Canvas is a rectangle of cells. Every cell holds a glyph written
// outright (text, an arrowhead), the arms of any lines through it, the
// pen it is drawn in, and whether it is spoken for. Nothing resolves
// until Rows.
type Canvas struct {
	w, h int
	r    []rune   // a glyph written outright; 0 for none
	comb []string // zero-width marks riding the glyph in the same cell
	mask []Mask   // the lines, resolved to glyphs at the end
	pen  []Pen
	text []bool // the glyph is content: it never dims
	held []bool // spoken for — nothing may be laid over it
}

// New is a blank canvas w cells across and h rows down.
func New(w, h int) *Canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return &Canvas{w: w, h: h,
		r: make([]rune, w*h), comb: make([]string, w*h), mask: make([]Mask, w*h),
		pen: make([]Pen, w*h), text: make([]bool, w*h), held: make([]bool, w*h)}
}

// W and H are the canvas's size in cells.
func (cv *Canvas) W() int { return cv.w }
func (cv *Canvas) H() int { return cv.h }

// In reports whether a cell is on the canvas. Everything below is safe
// off it: a drawing that runs over the edge loses what ran over, never
// the run.
func (cv *Canvas) In(x, y int) bool { return x >= 0 && y >= 0 && x < cv.w && y < cv.h }

func (cv *Canvas) at(x, y int) int { return y*cv.w + x }

// ink sets a cell's pen. A colour is a claim and the absence of one is
// not, so a set pen replaces anything and the default pen replaces
// nothing: where two coloured strokes cross, the later one owns the
// cell, and where a coloured one crosses an uncoloured one, the colour
// does.
func (cv *Canvas) ink(i int, p Pen) {
	if p != 0 {
		cv.pen[i] = p
	}
}

// ---------- lines ----------

// Line records that a line leaves this cell on side d. The glyph is not
// decided here; it cannot be, until every other line has arrived.
func (cv *Canvas) Line(x, y int, d Dir, p Pencil) {
	if !cv.In(x, y) {
		return
	}
	i := cv.at(x, y)
	cv.mask[i] = cv.mask[i].With(d, p.Style)
	cv.ink(i, p.Pen)
}

// Stroke lays a straight run between two cells, both ends included,
// each cell getting the arms that reach its neighbours in the run. A
// run of one cell lays nothing: that cell is the turning point of the
// run that follows, and stamping a stray arm there is what used to make
// a straight vertical read as ┼┼┼. A run that is not straight lays
// nothing at all — there is no diagonal in this vocabulary.
func (cv *Canvas) Stroke(x0, y0, x1, y1 int, p Pencil) {
	switch {
	case y0 == y1 && x0 != x1:
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		for x := x0; x <= x1; x++ {
			if x > x0 {
				cv.Line(x, y0, West, p)
			}
			if x < x1 {
				cv.Line(x, y0, East, p)
			}
		}
	case x0 == x1 && y0 != y1:
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		for y := y0; y <= y1; y++ {
			if y > y0 {
				cv.Line(x0, y, North, p)
			}
			if y < y1 {
				cv.Line(x0, y, South, p)
			}
		}
	}
}

// MaskAt is the arms a cell has. Off the canvas is no arms, so a router
// may ask about the cell past the edge without checking first.
func (cv *Canvas) MaskAt(x, y int) Mask {
	if !cv.In(x, y) {
		return 0
	}
	return cv.mask[cv.at(x, y)]
}

// ---------- heads ----------

// Head is an arrowhead pointing d, and the arm that reaches it. The arm
// is invisible under the head; it is there so a router reading the
// canvas back sees a line ending, not a mark floating in air.
func (cv *Canvas) Head(x, y int, d Dir, p Pen) {
	if !cv.In(x, y) {
		return
	}
	cv.put(x, y, Arrow(d), p, false)
	i := cv.at(x, y)
	cv.mask[i] = cv.mask[i].With(d, Light)
}

// ---------- text ----------

// Text lays a string at (x, y) by the terminal's own width rules: a wide
// glyph spends two columns and marks the second spent, a combining mark
// rides the glyph before it rather than taking a column of its own.
// Returns the cells spent, so a caller laying two strings knows where
// the second starts.
func (cv *Canvas) Text(x, y int, s string, p Pen) int {
	n := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// No column of its own — it rides the glyph before it.
			// Skipping it outright is what made a decomposed label lose
			// its accents while keeping its width.
			cv.addComb(x-1, y, r)
			continue
		}
		cv.put(x, y, r, p, true)
		if w == 2 && cv.In(x+1, y) {
			i := cv.at(x+1, y)
			cv.r[i] = grid.Shadow
			cv.comb[i] = ""
			cv.mask[i] = 0
		}
		x += w
		n += w
	}
	return n
}

// Blank rubs out n cells: the glyph, its marks and the lines under it.
// A cluster's name sits in the top edge of its frame, which means the
// frame's own line has to come out from under it.
func (cv *Canvas) Blank(x, y, n int) {
	for i := 0; i < n; i++ {
		if !cv.In(x+i, y) {
			continue
		}
		j := cv.at(x+i, y)
		cv.r[j], cv.comb[j], cv.mask[j] = 0, "", 0
	}
}

// put writes one glyph outright. text says whether it is content, which
// is the whole of what keeps a label out of the dim the structure
// draws in.
func (cv *Canvas) put(x, y int, r rune, p Pen, text bool) {
	if !cv.In(x, y) {
		return
	}
	i := cv.at(x, y)
	cv.r[i] = r
	cv.comb[i] = "" // a new glyph is a new cluster
	cv.text[i] = text
	cv.ink(i, p)
}

// addComb hangs a zero-width mark on the glyph already in a cell. A cell
// is a rune plus the marks that follow it. Bounded: decoration is lost
// past the bound, never a cell without a size.
func (cv *Canvas) addComb(x, y int, r rune) {
	if !cv.In(x, y) {
		return
	}
	i := cv.at(x, y)
	if len(cv.comb[i]) < grid.MaxCombBytes {
		cv.comb[i] += string(r)
	}
}

// Rune is the glyph written outright in a cell, or ' ' where none is —
// a line's glyph is not one of these, because a line has not chosen its
// glyph yet.
func (cv *Canvas) Rune(x, y int) rune {
	if !cv.In(x, y) {
		return ' '
	}
	if r := cv.r[cv.at(x, y)]; r != 0 {
		return r
	}
	return ' '
}

// ---------- ownership ----------

// Hold marks a rectangle as owned, so nothing lands inside a box that
// did not mean to be there.
func (cv *Canvas) Hold(x0, y0, w, h int) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			if cv.In(x, y) {
				cv.held[cv.at(x, y)] = true
			}
		}
	}
}

// Held reports whether a cell is spoken for.
func (cv *Canvas) Held(x, y int) bool {
	return cv.In(x, y) && cv.held[cv.at(x, y)]
}

// Free reports whether a run of n cells can take text without damaging
// anything already drawn there.
func (cv *Canvas) Free(x, y, n int) bool {
	if y < 0 || y >= cv.h || x < 0 || x+n > cv.w {
		return false
	}
	for i := 0; i < n; i++ {
		j := cv.at(x+i, y)
		if cv.r[j] != 0 || cv.mask[j] != 0 || cv.held[j] {
			return false
		}
	}
	return true
}

// ---------- boxes ----------

// A Box is every box this vocabulary draws: a node, a record, a frame
// round a cluster. Its walls go down as arms rather than as glyphs, so
// a line arriving at one becomes the junction that admits it without
// anybody asking — which is the whole reason edges used to dead-end
// against corners.
type Box struct {
	X, Y, W, H int
	Pencil          // the border's style and pen
	Round      bool // ╭ ╮ ╰ ╯ rather than ┌ ┐ └ ┘: a wall, never a bend
	Ink        Pen  // the text's pen
	// Title is a cluster's name. The canvas does not draw it: a frame is
	// laid by drawFrame and named by nameFrame, after every line is down,
	// because where the name goes depends on where the lines went.
	Title string
	Label []string
}

// Has reports whether a cell is inside this box, wall included.
func (b Box) Has(x, y int) bool {
	return x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H
}

// Box draws one. The label is centred in what the walls leave; a box
// with no room for walls is its label alone, because half a wall reads
// as damage and a missing one reads as text.
func (cv *Canvas) Box(b Box) {
	if b.W < 2 || b.H < 2 {
		cv.label(b, b.X, b.Y, b.W, b.H)
		cv.Hold(b.X, b.Y, b.W, b.H)
		return
	}
	x1, y1 := b.X+b.W-1, b.Y+b.H-1
	cv.Stroke(b.X, b.Y, x1, b.Y, b.Pencil)
	cv.Stroke(b.X, y1, x1, y1, b.Pencil)
	cv.Stroke(b.X, b.Y, b.X, y1, b.Pencil)
	cv.Stroke(x1, b.Y, x1, y1, b.Pencil)
	if b.Round {
		for _, c := range [4][2]int{{b.X, b.Y}, {x1, b.Y}, {b.X, y1}, {x1, y1}} {
			if cv.In(c[0], c[1]) {
				i := cv.at(c[0], c[1])
				cv.mask[i] = cv.mask[i].Round()
			}
		}
	}
	cv.label(b, b.X+1, b.Y+1, b.W-2, b.H-2)
	cv.Hold(b.X, b.Y, b.W, b.H)
}

// label centres the lines in the rectangle the walls left.
func (cv *Canvas) label(b Box, x0, y0, w, h int) {
	if len(b.Label) == 0 || w <= 0 || h <= 0 {
		return
	}
	y := y0 + (h-len(b.Label))/2
	for _, l := range b.Label {
		cv.Text(x0+(w-grid.Cells(l))/2, y, l, b.Ink)
		y++
	}
}

// Divider rules a box into fields — a record's. `at` is the offset from
// the box's own top-left; down rules a column, across rules a row. Both
// ends are arms into the walls they meet, so the rule reads as one
// piece with the box rather than a line trapped inside it.
func (cv *Canvas) Divider(b Box, at int, down bool) {
	if down {
		if at <= 0 || at >= b.W-1 {
			return
		}
		cv.Stroke(b.X+at, b.Y, b.X+at, b.Y+b.H-1, b.Pencil)
		return
	}
	if at <= 0 || at >= b.H-1 {
		return
	}
	cv.Stroke(b.X, b.Y+at, b.X+b.W-1, b.Y+at, b.Pencil)
}

// ---------- the drawing ----------

// Rows is the finished drawing, and exactly that: the rows that were
// drawn on, never padded out to the window, no blank row above or
// below, no trailing spaces. Every cell resolves here — an outright
// glyph wins, otherwise the arms decide, otherwise the cell is air —
// and colour goes on in runs: one escape where a run starts, one where
// the row ends, nothing at all on a row nobody coloured.
func (cv *Canvas) Rows() []string {
	out := make([]string, cv.h)
	var b strings.Builder
	for y := 0; y < cv.h; y++ {
		b.Reset()
		w := wire{b: &b}
		gap := 0 // spaces held back, so a row never ends in air
		for x := 0; x < cv.w; x++ {
			i := cv.at(x, y)
			g, content := cv.r[i], false
			switch {
			case g == grid.Shadow:
				continue // the wide glyph before it already spent this column
			case g != 0:
				// A glyph written outright wins, a space in a label
				// included: the space is a column of the label, and a
				// line showing through it is a line nobody drew there.
				content = cv.text[i]
			default:
				g = Glyph(cv.mask[i])
			}
			if g == ' ' {
				gap++
				continue
			}
			for ; gap > 0; gap-- {
				b.WriteByte(' ')
			}
			w.set(cv.pen[i], content)
			b.WriteRune(g)
			b.WriteString(cv.comb[i])
		}
		w.off()
		out[y] = b.String()
	}
	return grid.TrimBlank(out)
}

// wire writes one row's escapes. It holds what the terminal is set to,
// so a run of cells in one pen costs one escape and a row with no
// colour and no structure costs none at all.
type wire struct {
	b   *strings.Builder
	col Pen  // the foreground the terminal is on
	dim bool // SGR 2 in force
}

// set puts the terminal into the state one cell wants: its colour, and
// the dim that structure nobody coloured is drawn in. A coloured stroke
// is painted, not dimmed — SGR 2 over a set foreground is the
// terminal's own business and several answer it by dropping the colour,
// and structure only has to recede where nobody said what colour it is.
func (w *wire) set(p Pen, content bool) {
	w.faint(p == 0 && !content)
	w.colour(p)
}

func (w *wire) colour(c Pen) {
	if c == w.col {
		return
	}
	if c == 0 {
		w.b.WriteString("\x1b[39m")
	} else {
		w.b.WriteString("\x1b[38;2;")
		w.b.WriteString(strconv.Itoa(int(c >> 16 & 0xff)))
		w.b.WriteByte(';')
		w.b.WriteString(strconv.Itoa(int(c >> 8 & 0xff)))
		w.b.WriteByte(';')
		w.b.WriteString(strconv.Itoa(int(c & 0xff)))
		w.b.WriteByte('m')
	}
	w.col = c
}

func (w *wire) faint(on bool) {
	if on == w.dim {
		return
	}
	if on {
		w.b.WriteString("\x1b[2m")
	} else {
		w.b.WriteString("\x1b[22m")
	}
	w.dim = on
}

// off returns the terminal to what it was, and only if this row moved
// it: the drawing owns its own rows and nothing past them.
func (w *wire) off() {
	w.faint(false)
	w.colour(0)
}
