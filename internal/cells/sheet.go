// sheet.go — the slots given their sizes, and the drawing made on them.
//
// Placing answers which slot everything stands in; this answers how big a
// slot is. A column is as wide as its widest box and a gap is as wide as
// whatever has to pass through it — a label riding a line, a hoop hung off
// a wall, the rings of the frames that start or end there. Nothing is
// measured twice: the sizes are worked out once and the boxes, the frames
// and the scout all read the same numbers.
//
// The repair pass is here too, and it is the only one: an attempt that
// loses an edge or a label is made again with more air, and if the graph
// will not come out whole either way up, nothing is drawn — the caller
// shows the source under a notice. A picture that is missing an edge has
// no way of saying so, and a failure here is visible or it is a lie.

package cells

import (
	"strings"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// ---------- boxes ----------

// pad is the air between a label and its wall. One cell, which is what a
// box drawing has always looked like.
const pad = 1

// boxSize is the box a node needs: its label with a cell of air each side,
// or a record's fields side by side with a divider between them.
func boxSize(n layout.GNode) (w, h int) {
	if n.Record {
		w = 1
		for _, f := range n.Label {
			w += grid.Cells(f) + 2*pad + 1
		}
		return w, 3
	}
	for _, l := range n.Label {
		if c := grid.Cells(l); c > w {
			w = c
		}
	}
	h = len(n.Label) + 2
	if h < 3 {
		h = 3
	}
	return w + 2 + 2*pad, h
}

// ---------- the drawing ----------

// gaps is how much air one attempt leaves: along the flow, where the
// arrows and their labels go, and across it, where the lanes stack.
type gaps struct{ flow, cross int }

// ladder is the repair pass, and it is the only one there is: an attempt
// that loses an edge or a label is run again with more room. Every rung
// costs area and buys lanes, and the first that keeps the whole graph is
// the answer.
var ladder = []gaps{{0, 0}, {1, 1}, {2, 2}, {4, 3}, {8, 5}}

// Draw lays a graph out and draws it into a box `width` cells across and
// at most `maxRows` deep. Returns nil where the graph will not come out
// whole — too big for the box, or too tight to route every edge and set
// every label — which is the caller's cue to show the source under a
// notice.
//
// Nothing short is ever drawn. A drawing missing one edge says the two
// nodes it joined are not joined, and says it with the same confidence
// as the rest of the picture; there is nothing on the page for a reader
// to notice. The source under a notice is the honest answer, and it is
// the same answer the pixels rung gives.
func Draw(g *layout.Graph, width, maxRows int) []string {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}
	for _, flip := range orientations(g) {
		h := *g
		h.Horiz, h.Reverse = flip.horiz, flip.reverse
		spots, _, _ := blockAt(&h, treeOf(&h), -1)
		at := make([]spot, len(h.Nodes))
		for i := range at {
			at[i] = spots[i]
		}
		compact(at)
		sl := orient(&h, at)
		for _, extra := range ladder {
			rows, short := attempt(&h, sl, extra, width, maxRows)
			if rows == nil {
				break // too big at this orientation; more air will not help
			}
			if short == 0 {
				return rows
			}
		}
	}
	return nil
}

type facing struct{ horiz, reverse bool }

// orientations is the order a graph is tried in: as it was written, and
// then top-down where it was not already, because rows scroll and columns
// run out.
func orientations(g *layout.Graph) []facing {
	out := []facing{{g.Horiz, g.Reverse}}
	if g.Horiz {
		return append(out, facing{false, false})
	}
	// Down the page a label stands beside its line, and several lines
	// leaving one wall stand a cell apart, so a busy rank has nowhere to
	// put the words. Across the page a label sits in its own line and
	// cannot be mistaken for anything. A graph that comes out whole
	// top-down stays top-down; one that does not is turned.
	return append(out, facing{true, false})
}

// attempt draws one whole graph at one set of gaps. It answers the rows
// and how many edges and labels were lost: the caller decides whether to
// buy more room.
func attempt(g *layout.Graph, sl slots, extra gaps, width, maxRows int) ([]string, int) {
	bw := make([]int, len(g.Nodes))
	bh := make([]int, len(g.Nodes))
	colW := make([]int, sl.nCol)
	rowH := make([]int, sl.nRow)
	loops := make([]int, len(g.Nodes))
	said := make([]int, len(g.Nodes)) // the ones carrying words
	for _, e := range g.Edges {
		if e.Tail != e.Head {
			continue
		}
		loops[e.Tail]++
		if e.Label != "" {
			said[e.Tail]++
		}
	}
	for i := range g.Nodes {
		bw[i], bh[i] = boxSize(g.Nodes[i])
		// A hoop takes two ports on one wall and each one inside it takes
		// two more, so a node with self-loops needs wall to hang them on.
		// The plain ones hang over the top and the bottom, which is width;
		// the ones with words reach out of the side walls, which a
		// three-row box has none of.
		if n := 2 + 2*((loops[i]+1)/2); n > bw[i] {
			bw[i] = n
		}
		if said[i] > 0 {
			if n := 2 + 2*((said[i]+1)/2); n > bh[i] {
				bh[i] = n
			}
		}
		if bw[i] > colW[sl.col[i]] {
			colW[sl.col[i]] = bw[i]
		}
		if bh[i] > rowH[sl.row[i]] {
			rowH[sl.row[i]] = bh[i]
		}
	}
	room := make([]int, len(g.Nodes)) // rows a plain hoop needs above and below
	wide := make([]int, len(g.Nodes)) // columns a hoop with words needs beside
	for i := range loops {
		if loops[i] == 0 {
			continue
		}
		// A hoop that has to hold words stands three cells off the wall
		// rather than one, so the words have somewhere to sit beside it.
		room[i] = (loops[i]+1)/2 + 2
		if said[i] > 0 {
			room[i] += 2
		}
	}
	for _, e := range g.Edges {
		if e.Tail == e.Head && e.Label != "" {
			wide[e.Tail] = max(wide[e.Tail], grid.Cells(e.Label)+7)
		}
	}
	bs := bounds(g, sl, room, wide)
	gapX, gapY := spacing(g, sl, extra, bs, room, wide)
	// A frame carries its cluster's name, so it has to be wide enough to
	// hold it. The room comes out of the last column the frame covers,
	// which is the one place widening cannot push a frame over anything
	// that is not already inside it.
	for i, b := range bs {
		if !b.ok {
			continue
		}
		w := b.offL() + b.offR()
		for c := b.col0; c <= b.col1; c++ {
			w += colW[c]
			if c > b.col0 {
				w += gapX[c]
			}
		}
		if n := titleRoom(g.Clusters[i].Label); n > w {
			colW[b.col1] += n - w
		}
	}
	xs := make([]int, sl.nCol)
	x := 0
	for i := range colW {
		x += gapX[i]
		xs[i] = x
		x += colW[i]
	}
	total := x + gapX[sl.nCol]
	ys := make([]int, sl.nRow)
	y := 0
	for i := range rowH {
		y += gapY[i]
		ys[i] = y
		y += rowH[i]
	}
	deep := y + gapY[sl.nRow]
	if total > width || (maxRows > 0 && deep > maxRows) {
		return nil, 0
	}

	cv := New(total, deep)
	frames := frameRects(g, bs, xs, ys, colW, rowH)
	for i := range frames {
		if bs[i].ok {
			drawFrame(cv, frames[i])
		}
	}
	boxes := make([]Box, len(g.Nodes))
	for i := range g.Nodes {
		boxes[i] = nodeBox(g.Nodes[i], xs[sl.col[i]], ys[sl.row[i]], colW[sl.col[i]], rowH[sl.row[i]])
	}
	for i := range boxes {
		drawNode(cv, boxes[i], g.Nodes[i])
	}
	t := newTerrain(total, deep, boxes)
	for i := range frames {
		if bs[i].ok {
			t.ring(frames[i])
		}
	}
	short := routeAll(cv, g, sl, boxes, frames, enclosing(g), t)
	// The names last: a name in a top edge takes the stretch of it that
	// the fewest lines cross, and which those are is not known until
	// every line is down.
	for i := range frames {
		if bs[i].ok {
			nameFrame(cv, frames[i])
		}
	}
	return trimLeft(cv.Rows()), short
}

// spacing is the air between the slots: a base along each axis, whatever
// the ladder is paying on top of it, and room in the gap for the widest
// label that has to ride through it.
func spacing(g *layout.Graph, sl slots, extra gaps, bs []bound, room []int, wide []int) ([]int, []int) {
	flow, cross := 2, 3 // top-down: ranks stack in rows, lanes spread in columns
	if sl.horiz {
		flow, cross = 3, 1 // left-right: ranks march in columns, lanes stack in rows
	}
	flow += extra.flow
	cross += extra.cross
	gapX := make([]int, sl.nCol+1)
	gapY := make([]int, sl.nRow+1)
	for i := range gapX {
		if sl.horiz {
			gapX[i] = flow
		} else {
			gapX[i] = cross
		}
	}
	for i := range gapY {
		if sl.horiz {
			gapY[i] = cross
		} else {
			gapY[i] = flow
		}
	}
	// What each slot column and row wants outside itself: the air a
	// node's hoops and their words need, and a ring for every frame that
	// starts or ends there. A gap carries both of its neighbours' wants
	// and a cell between them.
	beforeX := make([]int, sl.nCol)
	afterX := make([]int, sl.nCol)
	beforeY := make([]int, sl.nRow)
	afterY := make([]int, sl.nRow)
	for v := range g.Nodes {
		if room[v] > 0 {
			beforeY[sl.row[v]] = max(beforeY[sl.row[v]], room[v])
			afterY[sl.row[v]] = max(afterY[sl.row[v]], room[v])
		}
		if wide[v] > 0 {
			beforeX[sl.col[v]] = max(beforeX[sl.col[v]], wide[v])
			afterX[sl.col[v]] = max(afterX[sl.col[v]], wide[v])
		}
	}
	for _, b := range bs {
		if !b.ok {
			continue
		}
		// One cell past the frame, so an edge that has to stop outside it
		// has somewhere to stop that a passing line has not already taken.
		beforeX[b.col0] = max(beforeX[b.col0], b.offL()+1)
		afterX[b.col1] = max(afterX[b.col1], b.offR()+1)
		beforeY[b.row0] = max(beforeY[b.row0], b.offT()+1)
		afterY[b.row1] = max(afterY[b.row1], b.offB()+1)
	}
	fit := func(gap, before, after []int) {
		for i := range gap {
			n := 1
			if i > 0 {
				n += after[i-1]
			}
			if i < len(before) {
				n += before[i]
			}
			if n > gap[i] {
				gap[i] = n
			}
		}
	}
	fit(gapX, beforeX, afterX)
	fit(gapY, beforeY, afterY)
	// A label rides its own line, so the gap it rides through has to be
	// long enough to hold it with a shoulder each side.
	need := func(g []int, i, n int) {
		if i >= 0 && i < len(g) && n > g[i] {
			g[i] = n
		}
	}
	for _, e := range g.Edges {
		n := grid.Cells(e.Label)
		if n == 0 || e.Tail == e.Head {
			continue
		}
		if sl.horiz {
			a, b := sl.col[e.Tail], sl.col[e.Head]
			if a > b {
				a, b = b, a
			}
			if b-a == 1 {
				need(gapX, b, n+6)
			}
			continue
		}
		// Down the page a label stands beside its line rather than in it,
		// so the room it needs is across the flow.
		need(gapX, sl.col[e.Tail], n+4)
		need(gapX, sl.col[e.Tail]+1, n+4)
	}
	return gapX, gapY
}

// nodeBox fills the slot it stands in. Boxes in one column come out the
// same width on purpose: a column of walls that line up reads as a column,
// and every extra cell of wall is a place for an edge to attach.
func nodeBox(n layout.GNode, x, y, w, h int) Box {
	b := Box{X: x, Y: y, W: w, H: h, Round: n.Round, Label: n.Label}
	b.Pencil = Pencil{Style: styleOf(n.Style, n.PenWidth), Pen: ParsePen(n.Pen)}
	b.Ink = ParsePen(n.FontPen)
	if n.Record {
		b.Label = nil
	}
	return b
}

func drawNode(cv *Canvas, b Box, n layout.GNode) {
	cv.Box(b)
	if !n.Record {
		return
	}
	// A record is one box with its fields ruled off inside it. The walls
	// go down as arms, so each divider joins the top and bottom edges by
	// itself and the whole thing reads as one box with fields in it.
	x := 1
	for i, f := range n.Label {
		w := grid.Cells(f) + 2*pad
		if i > 0 {
			cv.Divider(b, x-1, true)
		}
		cv.Text(b.X+x+(w-grid.Cells(f))/2, b.Y+b.H/2, f, b.Ink)
		x += w + 1
	}
}

func styleOf(style string, penWidth float64) Style {
	switch {
	case strings.Contains(style, "dashed"), strings.Contains(style, "dotted"):
		return Dashed
	case strings.Contains(style, "bold"), penWidth >= 2:
		return Heavy
	}
	return Light
}

// trimLeft takes the left margin off the finished rows. The margin is
// routing room, and where nothing routed through it, it is only air.
func trimLeft(rows []string) []string {
	n := -1
	for _, r := range rows {
		p := grid.StripSGR(r)
		if strings.TrimSpace(p) == "" {
			continue
		}
		i := 0
		for i < len(p) && p[i] == ' ' {
			i++
		}
		if n < 0 || i < n {
			n = i
		}
	}
	if n <= 0 {
		return rows
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		cut := n
		var b strings.Builder
		j := 0
		for j < len(r) && cut > 0 {
			if r[j] == 0x1b {
				k := j + 2
				for k < len(r) && (r[k] < 0x40 || r[k] > 0x7e) {
					k++
				}
				b.WriteString(r[j : k+1])
				j = k + 1
				continue
			}
			j++
			cut--
		}
		b.WriteString(r[j:])
		out[i] = b.String()
	}
	return out
}
