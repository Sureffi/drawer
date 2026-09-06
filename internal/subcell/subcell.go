// subcell.go — strokes below the cell, labels above it.
//
// A terminal cell is one glyph, and a glyph can be a 2x4 grid of dots:
// braille, which every font has, or the Unicode 16 octants, solid blocks
// that kitty and ghostty draw themselves. Eight sub-pixels per cell is
// enough for a curve to read as a curve and an arrowhead as a triangle.
// It is nowhere near enough for a letter — rasterised text at this size is
// mush, measured on screen — so the labels are not rasterised. They are
// set as real glyphs in real cells, over the strokes, and the strokes are
// cleared beneath them.
//
// Everything drawn here comes from graphviz's own drawing operations —
// its json output carries every polygon, ellipse, bezier and text anchor
// it would have painted — so the picture is graphviz's picture, only
// quantised. Clusters, shapes, multi-line labels, dashed edges and both
// arrowheads of a `dir=both` edge arrive for free, because they were
// already in the answer and the cell renderer only ever read the node
// centres out of it.
//
// The scale is the cell renderer's: ten cells to the inch across, six
// rows to the inch down, and nodes sized in those units so a label fits
// its box. Graphviz measures the type as Courier at 12 points, which is
// 7.2 points a character — one cell — and 12 points a line — one row.

package subcell

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/mattn/go-runewidth"
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// The type graphviz measures with. Courier is one of the three faces it
// carries width tables for when no font system exists — the wasm has none
// — and it is monospace, which is the whole point: a character is 0.6 of
// the size, and at 12 points that is 7.2 points, one cell.
const (
	inkFont   = "Courier"
	inkSize   = 12.0
	ptPerCell = 72.0 / layout.CellsPerInchX
	ptPerRow  = 72.0 / layout.RowsPerInchY
)

// layoutInk runs graphviz and reads back what it would have drawn.
func layoutInk(ctx context.Context, src string, force cgraph.RankDir) (*layout.Drawing, error) {
	var d *layout.Drawing
	err := layout.Door(ctx, src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		rd := force
		if rd == "" {
			rd = layout.RankdirOf(src)
		} else {
			graph.SetRankDir(rd)
		}
		graph.SetFontName(inkFont)
		graph.SetFontSize(inkSize)
		for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
			label := layout.LabelOf(n)
			n.SetLabel(label)
			n.SetFontName(inkFont)
			n.SetFontSize(inkSize)
			// Minimums, not fixed sizes: an ellipse or a diamond needs more
			// room than a box for the same label, and graphviz knows how much.
			n.SetWidth(float64(grid.Cells(label)+2) / layout.CellsPerInchX)
			n.SetHeight(layout.NodeRows / layout.RowsPerInchY)
			for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
				e.SetFontName(inkFont)
				e.SetFontSize(inkSize)
			}
		}
		layout.SetSeparation(graph, rd)
		// A cluster's frame sits eight points off its nodes by default — one
		// cell, so the frame and a box wall share a cell and read as one thick
		// stroke. Two cells is air.
		for sg, _ := graph.FirstSubGraph(); sg != nil; sg, _ = sg.NextSubGraph() {
			sg.SafeSet("margin", strconv.FormatFloat(2*ptPerCell, 'f', 1, 64), "")
		}
		var err error
		d, err = layout.Render(ctx, g, graph)
		return err
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// inkFootprint is the cell box the drawing occupies.
func inkFootprint(d *layout.Drawing) (cols, rows int) {
	return int(math.Ceil(d.W/ptPerCell)) + 1, int(math.Ceil(d.H/ptPerRow)) + 1
}

// fitInk is layout.Fit for this renderer: as written, then top-down, the
// first that fits the width and the height wins.
func fitInk(ctx context.Context, src string, width, maxRows int) (*layout.Drawing, int, int, bool) {
	for _, rd := range layout.Orientations(src) {
		d, err := layoutInk(ctx, src, rd)
		if err != nil {
			continue
		}
		cols, rows := inkFootprint(d)
		if cols > width || (maxRows > 0 && rows > maxRows) {
			continue
		}
		return d, cols, rows, true
	}
	return nil, 0, 0, false
}

// ---------- the sub-cell canvas ----------

// ink is a drawing at 2x4 per cell: dots for the strokes, glyphs for the
// labels, and one bit per cell saying which wins.
type ink struct {
	cols, rows int
	dots       []bool   // (cols*2) x (rows*4)
	dotc       []uint32 // the same grid: what colour each dot was laid in
	glyph      []rune   // per cell: a label glyph, or 0
	comb       []string // marks riding on that glyph
	gcol       []uint32 // per cell: what colour that glyph is set in
	label      []bool   // per cell: spoken for by a label
	pen        uint32   // the colour in force, as parseColor encodes it
	// where the picture's points land: origin top-left, sub-pixels
	sx, sy func(float64) float64
	// off shifts every point of the object being drawn, in sub-pixels: a
	// node's outline is moved onto the cells its label was snapped to
	dx, dy float64
}

func newInk(cols, rows int, h float64) *ink {
	k := &ink{cols: cols, rows: rows,
		dots:  make([]bool, cols*2*rows*4),
		dotc:  make([]uint32, cols*2*rows*4),
		glyph: make([]rune, cols*rows),
		comb:  make([]string, cols*rows),
		gcol:  make([]uint32, cols*rows),
		label: make([]bool, cols*rows),
	}
	k.sx = func(x float64) float64 { return x/ptPerCell*2 + k.dx }
	k.sy = func(y float64) float64 { return (h-y)/ptPerRow*4 + k.dy }
	return k
}

func (k *ink) dot(x, y int) {
	if x < 0 || y < 0 || x >= k.cols*2 || y >= k.rows*4 {
		return
	}
	k.dots[y*k.cols*2+x] = true
	k.dotc[y*k.cols*2+x] = k.pen
}

// line draws between two sub-pixel points. The dash state carries across
// segments of one stroke so a dashed curve stays evenly dashed.
type pen struct {
	dashed, dotted bool
	along          float64 // distance walked, for the dash pattern
}

func (k *ink) line(p *pen, x0, y0, x1, y1 float64) {
	dx, dy := x1-x0, y1-y0
	n := int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy))))
	if n == 0 {
		n = 1
	}
	step := math.Hypot(dx, dy) / float64(n)
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		on := true
		if p.dashed {
			on = math.Mod(p.along, 6) < 4
		} else if p.dotted {
			on = math.Mod(p.along, 3) < 1
		}
		if on {
			k.dot(int(math.Floor(x0+dx*t)), int(math.Floor(y0+dy*t)))
		}
		if i < n {
			p.along += step
		}
	}
}

// polyline draws a run of points; closed joins the last to the first.
func (k *ink) polyline(p *pen, pts [][2]float64, closed bool) {
	for i := 0; i+1 < len(pts); i++ {
		k.line(p, k.sx(pts[i][0]), k.sy(pts[i][1]), k.sx(pts[i+1][0]), k.sy(pts[i+1][1]))
	}
	if closed && len(pts) > 2 {
		l := len(pts) - 1
		k.line(p, k.sx(pts[l][0]), k.sy(pts[l][1]), k.sx(pts[0][0]), k.sy(pts[0][1]))
	}
}

// fill paints a polygon solid: an arrowhead is a triangle, not an outline.
func (k *ink) fill(pts [][2]float64) {
	if len(pts) < 3 {
		return
	}
	xs := make([]float64, len(pts))
	ys := make([]float64, len(pts))
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i, p := range pts {
		xs[i], ys[i] = k.sx(p[0]), k.sy(p[1])
		minX, maxX = math.Min(minX, xs[i]), math.Max(maxX, xs[i])
		minY, maxY = math.Min(minY, ys[i]), math.Max(maxY, ys[i])
	}
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		for x := int(math.Floor(minX)); x <= int(math.Ceil(maxX)); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			in := false
			for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
				if (ys[i] > py) != (ys[j] > py) &&
					px < (xs[j]-xs[i])*(py-ys[i])/(ys[j]-ys[i])+xs[i] {
					in = !in
				}
			}
			if in {
				k.dot(x, y)
			}
		}
	}
	k.polyline(&pen{}, pts, true) // and the outline, so a thin head still shows
}

// bezier flattens graphviz's cubic runs: 3n+1 points, n curves.
func (k *ink) bezier(p *pen, pts [][2]float64) {
	if len(pts) < 4 {
		k.polyline(p, pts, false)
		return
	}
	for i := 0; i+3 < len(pts); i += 3 {
		p0, p1, p2, p3 := pts[i], pts[i+1], pts[i+2], pts[i+3]
		prevX, prevY := k.sx(p0[0]), k.sy(p0[1])
		const steps = 24
		for s := 1; s <= steps; s++ {
			t := float64(s) / steps
			u := 1 - t
			x := u*u*u*p0[0] + 3*u*u*t*p1[0] + 3*u*t*t*p2[0] + t*t*t*p3[0]
			y := u*u*u*p0[1] + 3*u*u*t*p1[1] + 3*u*t*t*p2[1] + t*t*t*p3[1]
			cx, cy := k.sx(x), k.sy(y)
			k.line(p, prevX, prevY, cx, cy)
			prevX, prevY = cx, cy
		}
	}
}

// ellipse draws the outline of graphviz's [cx cy rx ry].
func (k *ink) ellipse(p *pen, r []float64) {
	if len(r) < 4 {
		return
	}
	cx, cy, rx, ry := r[0], r[1], r[2], r[3]
	n := int(math.Max(24, 2*math.Pi*math.Max(k.sx(rx), k.sy(0)-k.sy(ry))))
	prevX, prevY := k.sx(cx+rx), k.sy(cy)
	for i := 1; i <= n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		x, y := k.sx(cx+rx*math.Cos(a)), k.sy(cy+ry*math.Sin(a))
		k.line(p, prevX, prevY, x, y)
		prevX, prevY = x, y
	}
}

// place is where a label's run lands: its first column and its row. The
// anchor is graphviz's: x at the left, centre or right of the run by
// align, y on the baseline. The row is chosen from the middle of the
// x-height, which is where the eye puts a line of type, and the width is
// measured with the one ruler everything else here uses rather than the
// one graphviz guessed with. The run is centred as a whole and then
// snapped: snapping the anchor first and centring after put every
// odd-width label half a cell right of its box.
func (k *ink) place(op layout.Op, size float64) (col, row, n int, ok bool) {
	if len(op.Pt) < 2 || op.Text == "" {
		return 0, 0, 0, false
	}
	n = grid.Cells(op.Text)
	if n == 0 {
		return 0, 0, 0, false
	}
	if size <= 0 {
		size = inkSize
	}
	cx := k.sx(op.Pt[0]) / 2
	switch op.Align {
	case "c":
		cx -= float64(n) / 2
	case "r":
		cx -= float64(n)
	}
	col = int(math.Floor(cx + 0.5))
	row = int(math.Floor(k.sy(op.Pt[1]+size*0.35) / 4))
	return col, row, n, row >= 0 && row < k.rows
}

// text sets a label as glyphs at the cells place chose.
func (k *ink) text(op layout.Op, size float64) {
	col, row, _, ok := k.place(op, size)
	if !ok {
		return
	}
	x := col
	for _, r := range op.Text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			if x-1 >= 0 && x-1 < k.cols {
				i := row*k.cols + x - 1
				if len(k.comb[i]) < grid.MaxCombBytes {
					k.comb[i] += string(r)
				}
			}
			continue
		}
		if x >= 0 && x < k.cols {
			i := row*k.cols + x
			k.glyph[i], k.comb[i], k.label[i], k.gcol[i] = r, "", true, k.pen
			if w == 2 && x+1 < k.cols {
				k.glyph[i+1], k.label[i+1], k.gcol[i+1] = grid.Shadow, true, k.pen
			}
		}
		x += w
	}
}

// snapToLabel moves a node's outline onto the cells its label was
// snapped to. The label and the shape are quantised separately, and a
// wall that lands one cell out on one side reads as a mistake; the cell
// renderer settled this long ago — the box is sized from the label, the
// label does not fit itself into the box — and the same rule holds here.
// Returns the shift in sub-pixels; zero for a node with no label.
func (k *ink) snapToLabel(ldraw []layout.Op) (dx, dy float64) {
	size := inkSize
	var sumDX, sumDY float64
	lines := 0
	for _, op := range ldraw {
		switch op.Op {
		case "F":
			size = op.Size
		case "T":
			col, row, n, ok := k.place(op, size)
			if !ok {
				continue
			}
			want := (float64(col) + float64(n)/2) * 2
			have := k.sx(op.Pt[0])
			if op.Align == "l" {
				have = k.sx(op.Pt[0]) + float64(n)
			} else if op.Align == "r" {
				have = k.sx(op.Pt[0]) - float64(n)
			}
			sumDX += want - have
			sumDY += (float64(row)+0.5)*4 - k.sy(op.Pt[1]+size*0.35)
			lines++
		}
	}
	if lines == 0 {
		return 0, 0
	}
	return sumDX / float64(lines), sumDY / float64(lines)
}

// ops runs one drawing list. Fill is for arrowheads; a node's own shape is
// outlined only, because a solid box would blot the label inside it.
func (k *ink) ops(list []layout.Op, fill bool) {
	p := &pen{}
	size := inkSize
	// One drawing list is one object; the pen it sets is its own, and the
	// next object starts from whatever it inherited rather than from
	// whatever its neighbour happened to leave behind.
	was := k.pen
	defer func() { k.pen = was }()
	for _, op := range list {
		switch op.Op {
		case "c":
			k.pen = parseColor(op.Color)
		case "C":
			// A fill colour is only the pen where the fill is drawn. A node
			// outline is drawn from `c`; taking `C` there would paint every
			// box in the colour of the paint it never got.
			if fill {
				k.pen = parseColor(op.Color)
			}
		case "S":
			p.dashed = op.Style == "dashed"
			p.dotted = op.Style == "dotted"
			if op.Style == "invis" {
				return
			}
		case "F":
			size = op.Size
		case "p", "P":
			if fill && op.Op == "P" {
				k.fill(op.Points)
			} else {
				k.polyline(p, op.Points, true)
			}
		case "L":
			k.polyline(p, op.Points, false)
		case "b", "B":
			k.bezier(p, op.Points)
		case "e", "E":
			k.ellipse(p, op.Rect)
		case "T":
			k.text(op, size)
		}
	}
}

// ---------- glyphs ----------

// braille dots are numbered down the left column then the right, with
// the fourth row added later at 7 and 8; the bit for (col, row) follows.
var brailleBit = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// rows resolves the picture: a label glyph wins its cell, otherwise the
// dots become a braille or octant glyph, otherwise blank. Strokes are dim
// and labels are not — structure recedes, content stands — and the SGR
// rides through CC's display wire intact, measured.
func (k *ink) rowsOut(octants bool) []string {
	out := make([]string, 0, k.rows)
	var b strings.Builder
	var lit [8]uint32 // the pens of one cell's dots, for major
	for y := 0; y < k.rows; y++ {
		b.Reset()
		w := wire{b: &b}
		for x := 0; x < k.cols; x++ {
			i := y*k.cols + x
			if g := k.glyph[i]; g != 0 {
				if g == grid.Shadow {
					continue
				}
				w.faint(false)
				w.colour(k.gcol[i])
				b.WriteRune(g)
				b.WriteString(k.comb[i])
				continue
			}
			var bits, obits rune
			n := 0
			for c := 0; c < 2; c++ {
				for r := 0; r < 4; r++ {
					j := (y*4+r)*k.cols*2 + x*2 + c
					if !k.dots[j] {
						continue
					}
					bits |= brailleBit[c][r]
					obits |= 1 << uint(r*2+c)
					lit[n], n = k.dotc[j], n+1
				}
			}
			if bits == 0 {
				w.faint(false)
				w.colour(0)
				b.WriteRune(' ')
				continue
			}
			// A stroke nobody coloured recedes, as it always has; a stroke
			// somebody coloured is that colour and nothing else.
			if c := major(lit[:n]); c == 0 {
				w.colour(0)
				w.faint(true)
			} else {
				w.faint(false)
				w.colour(c)
			}
			if octants {
				b.WriteRune(octantGlyphs[obits])
			} else {
				b.WriteRune(0x2800 + bits)
			}
		}
		w.faint(false)
		w.colour(0)
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return grid.TrimBlank(out)
}

// ---------- composition ----------

// renderInk draws a laid-out graph. Clusters first, then edges with their
// heads, then node outlines, and every label last so it wins its cells;
// the dots beneath a label are cleared, which is what lets an edge label
// sit in its own stroke the way the cell renderer's do.
func renderInk(d *layout.Drawing, cols, rows int, octants bool) []string {
	k := newInk(cols, rows, d.H)
	for _, o := range d.Objects {
		if o.Nodes != nil {
			k.ops(o.Draw, false)
		}
	}
	for _, e := range d.Edges {
		k.ops(e.Draw, false)
		k.ops(e.HDraw, true)
		k.ops(e.TDraw, true)
	}
	for _, o := range d.Objects {
		if o.Nodes == nil {
			k.dx, k.dy = k.snapToLabel(o.LDraw)
			k.ops(o.Draw, false)
			k.dx, k.dy = 0, 0
		}
	}
	for _, o := range d.Objects {
		k.ops(o.LDraw, false)
	}
	for _, e := range d.Edges {
		k.ops(e.LDraw, false)
	}
	k.ops(d.LDraw, false)
	// labels win: clear the dots in every cell a glyph occupies
	for i, l := range k.label {
		if !l {
			continue
		}
		x, y := i%k.cols, i/k.cols
		for r := 0; r < 4; r++ {
			for c := 0; c < 2; c++ {
				k.dots[(y*4+r)*k.cols*2+x*2+c] = false
			}
		}
	}
	return k.rowsOut(octants)
}

// Draw is the braille and octant rung: rows, or nil when nothing fits and
// the caller steps down.
func Draw(ctx context.Context, src string, width int, octants bool) []string {
	d, cols, rows, ok := fitInk(ctx, src, width, grid.MaxRows)
	if !ok {
		return nil
	}
	return renderInk(d, cols, rows, octants)
}

// HasInk reports a braille or octant stroke in a row.
func HasInk(s string) bool {
	for _, r := range s {
		if (r > 0x2800 && r <= 0x28FF) || (r >= 0x1CD00 && r <= 0x1CDE5) || (r >= 0x1CEA0 && r <= 0x1CEAF) {
			return true
		}
	}
	return false
}
