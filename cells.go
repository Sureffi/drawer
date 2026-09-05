// cells.go — the cells rung's canvas: box-drawing characters, any terminal.
//
//	```dot
//	digraph { rankdir=LR; A -> B -> C }
//	```
//
// The rule: a diagram renders into the cell box it was measured for, and
// if it does not fit, the source shows under a notice. A failure is
// visible, never silent. route.go puts the edges on this canvas.

package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// A cell does not know what glyph it is until the drawing is finished.
// Lines record which way they leave a cell — up, right, down, left — and
// the glyph is looked up from those four bits at the end. Writing glyphs
// as you go and merging them cannot work: a corner and a crossing are the
// same two strokes meeting, and only the connections tell them apart.
// That is why every bend used to come out as ┼.
const (
	dirUp uint8 = 1 << iota
	dirRight
	dirDown
	dirLeft
)

// Indexed by the four direction bits. Stubs (one bit) read as the line
// they continue, so a run that starts nowhere still looks like a line.
var glyphs = [16]rune{
	0:                                    ' ',
	dirUp:                                '│',
	dirRight:                             '─',
	dirUp | dirRight:                     '└',
	dirDown:                              '│',
	dirUp | dirDown:                      '│',
	dirRight | dirDown:                   '┌',
	dirUp | dirRight | dirDown:           '├',
	dirLeft:                              '─',
	dirUp | dirLeft:                      '┘',
	dirRight | dirLeft:                   '─',
	dirUp | dirRight | dirLeft:           '┴',
	dirDown | dirLeft:                    '┐',
	dirUp | dirDown | dirLeft:            '┤',
	dirRight | dirDown | dirLeft:         '┬',
	dirUp | dirRight | dirDown | dirLeft: '┼',
}

type canvas struct {
	w, h int
	c    []rune   // glyphs written outright: node borders, labels, arrowheads
	comb []string // zero-width marks riding on the glyph in the same cell
	link []uint8  // line connections, resolved to glyphs at the end
	held []bool   // spoken for — nothing may be laid over it
	port []bool   // a border cell an edge legitimately attaches to
}

func newCanvas(w, h int) *canvas {
	cv := &canvas{w: w, h: h, c: make([]rune, w*h), comb: make([]string, w*h),
		link: make([]uint8, w*h), held: make([]bool, w*h), port: make([]bool, w*h)}
	for i := range cv.c {
		cv.c[i] = ' '
	}
	return cv
}

func (cv *canvas) in(x, y int) bool { return x >= 0 && y >= 0 && x < cv.w && y < cv.h }

func (cv *canvas) set(x, y int, r rune) {
	if cv.in(x, y) {
		cv.c[y*cv.w+x] = r
		cv.comb[y*cv.w+x] = "" // a new glyph is a new cluster
	}
}

// addComb hangs a zero-width mark on the glyph already in a cell. A cell
// is a rune plus the marks that follow it; the canvas did not know that,
// and `putStr` dropped every combining mark it was handed — so a decomposed
// "a"+U+0301 label drew as a bare "accent", correct in width and wrong in
// every other way. Bounded: decoration is lost past the bound, never a cell
// without a size.
func (cv *canvas) addComb(x, y int, r rune) {
	if !cv.in(x, y) {
		return
	}
	i := y*cv.w + x
	if len(cv.comb[i]) < maxCombBytes {
		cv.comb[i] += string(r)
	}
}

func (cv *canvas) get(x, y int) rune {
	if !cv.in(x, y) {
		return ' '
	}
	return cv.c[y*cv.w+x]
}

// connect records that a line leaves this cell in the given directions.
// attachTail joins a leaving edge to the wall it leaves from.
//
// A port sits one cell OUTSIDE the box, which is where an arrowhead goes.
// At the head of an edge that arrowhead is the attachment and reads fine.
// A tail has no arrowhead, so the stroke simply began beside the box —
// and when graphviz put the port on a border row (the outer rows of a
// three-row box are its corners) the cell it began beside was `└`, which
// accepts up and right and nothing from the left. The result was a line
// that visibly dead-ended one cell short of the node it came from.
//
// Only tails, and only the one wall cell the port actually points at: an
// edge whose Manhattan path merely crosses a box leaves link bits behind
// too, and honouring those would punch holes through node walls.
func (cv *canvas) attachTail(p port) {
	var bx, by int
	var d uint8
	switch p.head {
	case '▶':
		bx, by, d = p.x+1, p.y, dirLeft
	case '◀':
		bx, by, d = p.x-1, p.y, dirRight
	case '▼':
		bx, by, d = p.x, p.y+1, dirUp
	case '▲':
		bx, by, d = p.x, p.y-1, dirDown
	default:
		return
	}
	if cv.in(bx, by) {
		cv.port[by*cv.w+bx] = true
		cv.connect(bx, by, d)
	}
}

// borderBits reads a box glyph back as the connections it already makes,
// so a line arriving at one can be merged into it instead of dead-ending
// against it. A run that met a box's *corner* used to stop one cell short
// and read as a broken edge: `└` accepts up and right, and an edge coming
// from the left had nowhere to land.
var borderBits = map[rune]uint8{
	'─': dirLeft | dirRight, '│': dirUp | dirDown,
	'┌': dirRight | dirDown, '┐': dirLeft | dirDown,
	'└': dirUp | dirRight, '┘': dirUp | dirLeft,
	'╭': dirRight | dirDown, '╮': dirLeft | dirDown,
	'╰': dirUp | dirRight, '╯': dirUp | dirLeft,
	'├': dirUp | dirRight | dirDown, '┤': dirUp | dirLeft | dirDown,
	'┬': dirLeft | dirRight | dirDown, '┴': dirUp | dirLeft | dirRight,
	'┼': dirUp | dirDown | dirLeft | dirRight,
}

func (cv *canvas) connect(x, y int, d uint8) {
	if cv.in(x, y) {
		cv.link[y*cv.w+x] |= d
	}
}

func (cv *canvas) linksAt(x, y int) uint8 {
	if !cv.in(x, y) {
		return 0
	}
	return cv.link[y*cv.w+x]
}

// hold marks a rectangle as owned, so a label never lands inside a box.
func (cv *canvas) hold(x0, y0, w, h int) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			if cv.in(x, y) {
				cv.held[y*cv.w+x] = true
			}
		}
	}
}

// free reports whether a run of n cells can take text without damaging
// anything already drawn there.
func (cv *canvas) free(x, y, n int) bool {
	if y < 0 || y >= cv.h || x < 0 || x+n > cv.w {
		return false
	}
	for i := 0; i < n; i++ {
		if cv.get(x+i, y) != ' ' || cv.linksAt(x+i, y) != 0 || cv.held[y*cv.w+x+i] {
			return false
		}
	}
	return true
}

// rows resolves every cell: an outright glyph wins, otherwise the line
// connections decide, otherwise blank.
func (cv *canvas) rows() []string {
	out := make([]string, cv.h)
	var b strings.Builder
	for y := 0; y < cv.h; y++ {
		b.Reset()
		for x := 0; x < cv.w; x++ {
			r := cv.c[y*cv.w+x]
			if r == shadow {
				continue // the wide glyph before it already spent this column
			}
			if r != ' ' && r != 0 {
				// A line that reached this border cell on purpose joins it
				// rather than stopping against it.
				if i := y*cv.w + x; cv.port[i] {
					if have, ok := borderBits[r]; ok {
						if all := have | cv.link[i]; all != have {
							r = glyphs[all&0xf]
						}
					}
				}
				b.WriteRune(r)
				b.WriteString(cv.comb[y*cv.w+x])
				continue
			}
			b.WriteRune(glyphs[cv.link[y*cv.w+x]&0xf])
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// nbox is where a node's box is drawn, in cells. drawNode and every edge
// that meets it read it from here, so the box a line aims at and the box
// on screen are the same rectangle rather than two that usually agree.
type nbox struct{ x0, y0, w, h int }

func (b nbox) has(x, y int) bool {
	return x >= b.x0 && x < b.x0+b.w && y >= b.y0 && y < b.y0+b.h
}

// port is where an edge meets a box: the cell just outside the middle of
// one side, plus the arrowhead that side implies. Attaching at a side's
// middle is what keeps a line off the corner glyphs, where it used to read
// as a stroke growing out of `┌`.
type port struct {
	x, y  int
	head  rune
	horiz bool
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func drawNode(cv *canvas, label string, b nbox) {
	x0, y0, bw := b.x0, b.y0, b.w
	if b.h < 3 {
		putStr(cv, x0, y0, label)
		return
	}
	for x := x0; x < x0+bw; x++ {
		cv.set(x, y0, '─')
		cv.set(x, y0+2, '─')
		cv.set(x, y0+1, ' ')
	}
	// Rounded corners are the box's own voice: a box corner and an edge
	// bend used to wear the same glyph, and the reader paid to tell a
	// wall from a turn. Now ╭ is always a box and ┌ is always an edge.
	cv.set(x0, y0, '╭')
	cv.set(x0+bw-1, y0, '╮')
	cv.set(x0, y0+2, '╰')
	cv.set(x0+bw-1, y0+2, '╯')
	cv.set(x0, y0+1, '│')
	cv.set(x0+bw-1, y0+1, '│')
	putStr(cv, x0+1+(bw-2-textCells(label))/2, y0+1, label)
	cv.hold(x0, y0, bw, 3)
}

func putStr(cv *canvas, x, y int, s string) {
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// No column of its own — it rides the glyph before it. Skipping
			// it outright is what made a decomposed label lose its accents.
			cv.addComb(x-1, y, r)
			continue
		}
		cv.set(x, y, r)
		if w == 2 {
			cv.set(x+1, y, shadow)
		}
		x += w
	}
}

type ipt struct{ x, y int }

// drawEdgeLabel puts a label where graphviz put it. graphviz already
// reserved room for the text when it laid the graph out, and the old
// guess — the midpoint of the spline — threw that answer away. Cells
// already spoken for are left alone and the label is nudged; a label
// that cannot be placed cleanly is dropped rather than allowed to
// damage the drawing it annotates.
func drawEdgeLabel(cv *canvas, e dedge, sx, sy func(float64) int) {
	if e.label == "" {
		return
	}
	n := textCells(e.label)
	x0, y0 := sx(e.lx)-n/2, sy(e.ly)
	for _, dy := range []int{0, -1, 1, -2, 2} {
		for _, dx := range []int{0, 1, -1, 2, -2, 3, -3, 4, -4} {
			if cv.free(x0+dx, y0+dy, n) {
				putStr(cv, x0+dx, y0+dy, e.label)
				return
			}
		}
	}
}

// hrun links a horizontal run. A zero-length run is not a line — it is
// the turning point of the vertical that follows, and stamping a stray
// stroke there is what used to make a straight vertical read as ┼┼┼.
func hrun(cv *canvas, x0, x1, y int) {
	if x0 == x1 {
		return
	}
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		var d uint8
		if x > x0 {
			d |= dirLeft
		}
		if x < x1 {
			d |= dirRight
		}
		cv.connect(x, y, d)
	}
}

func vrun(cv *canvas, y0, y1, x int) {
	if y0 == y1 {
		return
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		var d uint8
		if y > y0 {
			d |= dirUp
		}
		if y < y1 {
			d |= dirDown
		}
		cv.connect(x, y, d)
	}
}
