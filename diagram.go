// diagram.go — structure, drawn into cells.
//
// The writer emits a graph and the reader sees it drawn: this is the
// floor rung, box-drawing characters that any terminal has.
//
//	```dot
//	digraph { rankdir=LR; A -> B -> C }
//	```
//
// Layout comes from graphviz (pure Go, no cgo) via its `plain` output:
// node centres and sizes in inches, edge splines as point lists. We only
// do the rasterising, because placing boxes so edges do not cross is the
// part that is genuinely hard and is already solved.
//
// The rule: a diagram renders into the cell box it was measured for, and
// if it does not fit, the source shows unchanged. A failure is visible,
// never silent.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// door is the one way through to graphviz: it opens a context, parses the
// source and hands the graph to fn, closing everything after it. A source
// with no graph in it parses to (nil, nil): graphviz reports nothing wrong
// because nothing was asked of it. Every caller dereferences the result, so
// the nil dies here — an empty ```dot fence is one the model opened and
// closed, not a diagram. graphviz.New registers into package-level maps and
// two at once are a fatal error; nothing here runs two, the hook being one
// process per delta. A layout measures about a millisecond.
func door(src string, fn func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error) error {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return err
	}
	defer g.Close()
	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil {
		return err
	}
	if graph == nil {
		return errors.New("no graph in source")
	}
	defer graph.Close()
	return fn(ctx, g, graph)
}

// orientations is the order a graph is tried in: as written, then top-down
// where it was not already, because rows scroll and columns run out.
func orientations(src string) []cgraph.RankDir {
	if rankdirOf(src) == cgraph.TBRank {
		return []cgraph.RankDir{""}
	}
	return []cgraph.RankDir{"", cgraph.TBRank}
}

// labelOf is a node's label as graphviz would print it: what it declares,
// else its name, which is what `\N` means.
func labelOf(n *cgraph.Node) string {
	if l := n.Label(); l != "" && l != `\N` {
		return l
	}
	name, _ := n.Name()
	return name
}

type dnode struct {
	name  string
	x, y  float64
	label string
}

type dedge struct {
	tail, head string
	pts        [][2]float64
	label      string
	lx         float64
	ly         float64
}

type dlayout struct {
	w, h  float64
	nodes []dnode
	edges []dedge
	// horiz is the axis the ranks run along, read from the rankdir the
	// layout was made with. It used to be re-derived from where the nodes
	// landed, and a top-down tree wider than it was tall read as
	// left-right — then `straighten` pulled children onto their parent's
	// row and the hierarchy collapsed. The answer was upstream all along.
	horiz bool
	// directed says whether the edges carry heads. A `graph { a -- b }`
	// used to draw arrows nobody wrote.
	directed bool
}

// Cells per inch. graphviz thinks in inches sized for 14pt type; a
// terminal thinks in cells that are roughly twice as tall as they are
// wide. Rather than scaling its output down — which turns a five-letter
// label into a twenty-cell box — we hand it node sizes already expressed
// in these units, let it do the spacing, and scale back by exactly the
// same factor. The boxes then come out the size we asked for.
const (
	cellsPerInchX = 10.0
	rowsPerInchY  = 6.0
	nodeRows      = 3 // border, label, border
)

// layoutDOT runs graphviz and parses its `plain` output. Coordinates are
// in inches with the origin bottom-left; the rasteriser flips them.
//
// force overrides the orientation the source asked for; empty leaves the
// author's choice alone.
func layoutDOT(src string, force cgraph.RankDir) (*dlayout, error) {
	var l *dlayout
	err := door(src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		rd := force
		if rd == "" {
			rd = rankdirOf(src)
		} else {
			graph.SetRankDir(rd)
		}
		sizeNodesInCells(graph)
		setSeparation(graph, rd)
		var buf bytes.Buffer
		if err := g.Render(ctx, graph, "plain", &buf); err != nil {
			return err
		}
		l = parsePlain(buf.String())
		l.horiz = rd == cgraph.LRRank || rd == cgraph.RLRank
		l.directed = directedRe.MatchString(src)
		return nil
	})
	return l, err
}

// sizeNodesInCells fixes every node's footprint to the space its label
// actually needs, in cell units. This is the whole trick: graphviz keeps
// doing the hard part (placing boxes so edges behave) but stops guessing
// at typography we do not have.
func sizeNodesInCells(g *cgraph.Graph) {
	for n, _ := g.FirstNode(); n != nil; n, _ = g.NextNode(n) {
		label := labelOf(n)
		n.SetLabel(label)
		n.SetShape(cgraph.BoxShape)
		n.SetFixedSize(true)
		n.SetWidth(float64(textCells(label)+2) / cellsPerInchX)
		n.SetHeight(nodeRows / rowsPerInchY)
	}
}

// rankdirOf reports the orientation a source asks for. graphviz exposes
// no getter for it, and the axis is what decides whether a gap measured
// in cells divides by columns-per-inch or rows-per-inch. Absent means TB,
// which is graphviz's own default.
var rankdirRe = regexp.MustCompile(`(?i)rankdir\s*=\s*"?(TB|LR|BT|RL)"?`)

// directedRe reads the graph's own first word. The binding exposes no
// getter for it, and a `graph {` drawn with arrowheads is a graph nobody
// wrote.
var directedRe = regexp.MustCompile(`(?i)^\s*(strict\s+)?digraph\b`)

func rankdirOf(src string) cgraph.RankDir {
	if m := rankdirRe.FindStringSubmatch(src); m != nil {
		return cgraph.RankDir(strings.ToUpper(m[1]))
	}
	return cgraph.TBRank
}

// setSeparation picks the gaps in cells and hands them over in the inches
// graphviz wants. Node sizes were already expressed this way; leaving the
// gaps at graphviz's defaults meant spacing chosen for 14pt type on paper
// — three blank rows between ranks, which is a lot of screen for a gap.
//
// Which axis a gap lives on depends on the orientation: ranks separate
// along the flow, siblings separate across it. A row costs about twice
// what a column costs to a reader, so the two axes do not want the same
// number.
func setSeparation(g *cgraph.Graph, rd cgraph.RankDir) {
	// top-down: ranks stack in rows, siblings spread in columns
	rank, rankPerInch := 2.0, rowsPerInchY
	node, nodePerInch := 4.0, cellsPerInchX
	if rd == cgraph.LRRank || rd == cgraph.RLRank {
		// left-right: ranks march in columns, siblings stack in rows
		rank, rankPerInch = 5.0, cellsPerInchX // room for ───▶
		node, nodePerInch = 1.0, rowsPerInchY
	}
	g.SetRankSeparator(rank / rankPerInch)
	g.SetNodeSeparator(node / nodePerInch)
}

func atof(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

// textCells is the only ruler in this file. Box widths, label lengths and
// the offsets that centre one inside the other all have to agree, and they
// agree by being the same measurement rather than three that usually match.
func textCells(s string) int { return runewidth.StringWidth(s) }

// parsePlain reads graphviz's plain format. Fields are space separated
// with quoted labels; the label is the only field that can contain a
// space, so a small hand parser beats a regexp here.
func parsePlain(s string) *dlayout {
	l := &dlayout{}
	for _, line := range strings.Split(s, "\n") {
		f := splitPlain(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "graph":
			if len(f) >= 4 {
				l.w, l.h = atof(f[2]), atof(f[3])
			}
		case "node":
			if len(f) >= 7 {
				// f[4] and f[5] are graphviz's node width and height in
				// inches. A node box here is sized from its label in cells,
				// not from what graphviz thought it would be, so they are
				// read past rather than stored.
				l.nodes = append(l.nodes, dnode{
					name: f[1], x: atof(f[2]), y: atof(f[3]), label: f[6],
				})
			}
		case "edge":
			if len(f) < 4 {
				continue
			}
			n, _ := strconv.Atoi(f[3])
			e := dedge{tail: f[1], head: f[2]}
			i := 4
			for k := 0; k < n && i+1 < len(f); k++ {
				e.pts = append(e.pts, [2]float64{atof(f[i]), atof(f[i+1])})
				i += 2
			}
			// What trails the points is `style color`, or `label lx ly
			// style color`. The field count says which, exactly. Asking
			// instead whether the text looks like a number dropped every
			// numeric label on the floor — a weight of `42` simply was not
			// drawn — and graphviz had already answered the question by
			// how many fields it wrote.
			if len(f)-i >= 5 {
				e.label, e.lx, e.ly = f[i], atof(f[i+1]), atof(f[i+2])
			}
			l.edges = append(l.edges, e)
		}
	}
	return l
}

func splitPlain(line string) []string {
	var out []string
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '"' {
			i++
			var b strings.Builder
			for i < len(line) && line[i] != '"' {
				// Only `\"` and `\\` are this format's escapes. Eating the
				// backslash of anything else turned a label of `a\nb` into
				// `anb` — a word nobody wrote, drawn with full confidence.
				// Left alone it reads as `a\nb`: the tool plainly did not
				// handle it, which is the accepted failure.
				if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\') {
					i++
				}
				b.WriteByte(line[i])
				i++
			}
			i++
			out = append(out, b.String())
			continue
		}
		start := i
		for i < len(line) && line[i] != ' ' {
			i++
		}
		out = append(out, line[start:i])
	}
	return out
}

// ---------- rasterising ----------

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

// maxCombBytes bounds the combining marks a cell keeps: decoration is lost
// past it, never a cell without a size.
const maxCombBytes = 16

// shadow is the second cell of a wide glyph. A canvas cell is one column,
// but a glyph is not: without a marker for the column the glyph already
// spent, `rows` emitted the rune AND a space and every row carrying a wide
// label came out one column wider than its own box for each one — a
// drawing that measured right and rendered crooked.
const shadow rune = -1

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

// footprint is the cell box a laid-out graph occupies. Reserving and
// drawing both ask it, of the same layout, so the two can never disagree.
func footprint(l *dlayout) (w, h int) {
	if l == nil || len(l.nodes) == 0 || l.w <= 0 || l.h <= 0 {
		return 0, 0
	}
	return int(l.w*cellsPerInchX) + 2, int(l.h*rowsPerInchY) + 1
}

// fit chooses how to draw a graph in the space that actually exists. A
// graph too wide to fit is redrawn top-down before it is given up on:
// vertical costs rows, and rows scroll, where horizontal costs columns,
// and columns simply run out. Only when neither orientation fits does the
// source show, which is still the whole failure policy.
//
// maxRows bounds the answer; 0 is no ceiling.
//
// It returns the layout it settled on. Measuring meant laying the graph
// out, and the drawing that follows needs exactly that layout; running
// graphviz a second time to rediscover what this call already knows would
// be the plainest waste in the file.
func fit(src string, width, maxRows int) (*dlayout, int, bool) {
	for _, rd := range orientations(src) {
		l, err := layoutDOT(src, rd)
		if err != nil {
			continue
		}
		w, h := footprint(l)
		if w <= 0 || h <= 0 || w > width {
			continue
		}
		if maxRows > 0 && h > maxRows {
			continue
		}
		return l, h, true
	}
	return nil, 0, false
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

// A drawing that says why there is no drawing.
//
// The old answer to a fence that would not draw was to leave the source
// showing. That is honest and it is readable, and it is also silent about
// the one thing the reader wants: whether this is source because somebody
// asked for source, or because something failed. A window three columns
// too narrow and a graph with a typo in it looked identical.
//
// So the failure gets drawn too, over the source, in the same fence.

// noticeChrome is the border and padding a notice spends on itself.
const noticeChrome = 4

// DrawNotice renders a bordered box carrying reason, sized for a w by h
// region. Returns nil when there is not enough room to say anything —
// a notice too small to read is worse than the source it replaced.
func DrawNotice(reason string, w, h int) []string {
	if h < 3 || w < 24 {
		return nil
	}
	inner := w - noticeChrome
	if inner > 72 {
		inner = 72
	}
	body := wrapWords(reason, inner)
	if len(body) > h-2 {
		body = body[:h-2]
	}
	if len(body) == 0 {
		return nil
	}
	width := 0
	for _, l := range body {
		if n := textCells(l); n > width {
			width = n
		}
	}
	const title = " no diagram "
	if n := textCells(title); width < n {
		width = n
	}

	rows := make([]string, 0, len(body)+2)
	top := "╭" + title + strings.Repeat("─", width-textCells(title)+2) + "╮"
	rows = append(rows, top)
	for _, l := range body {
		rows = append(rows, "│ "+l+strings.Repeat(" ", width-textCells(l))+" │")
	}
	rows = append(rows, "╰"+strings.Repeat("─", width+2)+"╯")
	return rows
}

// wrapWords breaks a reason at spaces, measured in cells rather than
// bytes — the reasons carry × and box glyphs.
func wrapWords(s string, width int) []string {
	if width < 8 {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case textCells(line)+1+textCells(word) <= width:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// CutReason works out why a source did not become a drawing in the space
// it was given, and says it in terms the reader can act on. Width is the
// only lever they have — rows are bounded by the window and the ladder
// already spent them — so where widening would work, the notice names the
// column count that does it rather than the row count that failed.
//
// Asked only on the failing path: it lays the graph out again to find out.
func CutReason(src string, width, region int) string {
	l, err := layoutDOT(src, "")
	if err != nil {
		return "graphviz could not read this: " + firstLine(err.Error())
	}
	if l == nil {
		return "no graph in this fence"
	}
	lw, lh := footprint(l) // as written, usually left-right
	if lw <= 0 || lh <= 0 {
		return "nothing to draw"
	}
	th := 0
	if td, err := layoutDOT(src, cgraph.TBRank); err == nil && td != nil {
		if tw, h := footprint(td); tw > 0 && tw <= width {
			th = h
		}
	}
	switch {
	case lh <= region && lw > width:
		return fmt.Sprintf("needs %d columns, this window has %d", lw, width)
	case th > 0 && lh <= region:
		return fmt.Sprintf("%d rows top-down; %d columns would draw it sideways in %d",
			th, lw, lh)
	case th > 0:
		return fmt.Sprintf("needs %d rows, this region has %d", th, region)
	}
	return fmt.Sprintf("needs %d by %d, this region is %d by %d", lw, lh, width, region)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
