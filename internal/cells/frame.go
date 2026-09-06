// frame.go — a cluster, drawn.
//
// A frame is a rectangle round a block of slots with the cluster's name on
// it. Where it stands is arithmetic on the block: a cell of air for
// whatever hangs off the boxes inside, then a ring, and another ring for
// every frame standing outside this one on that side. The gaps between the
// slots are sized to hold all of it, which is why a frame never lands on
// anything and nothing that is not a member ever lands inside one.
//
// A frame's line is a wall to the scout: a reader stops a line at a
// border, so an edge drawn through a frame is an edge cut in half. An edge
// that has to leave one stops outside it instead, with a corridor of
// blanks kept clear behind it — wire.go lays that out — and the reader
// walks in across the blanks and the frame alike.

package cells

import (
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// ---------- frames ----------

// bound is one cluster's rectangle on the slot grid, and how many frames
// have to fit between it and whatever is outside it on each side. A
// cluster's frame stands two cells clear of its contents — one for air,
// one for the line — and a cluster inside another needs that room twice.
type bound struct {
	col0, row0, col1, row1   int
	left, right, top, bottom int // frames stacked outside this one's contents
	padL, padR, padT, padB   int // air inside the frame the contents need
	ok                       bool
}

// bounds is where every cluster's frame goes, in slots. Placing put each
// cluster's members in a rectangle of their own, so this is only the
// reading of it — and the depth counts, which come from the outside in.
func bounds(g *layout.Graph, sl slots, room, wide []int) []bound {
	out := make([]bound, len(g.Clusters))
	for i, c := range g.Clusters {
		b := bound{col0: 1 << 30, row0: 1 << 30, col1: -1, row1: -1}
		for _, v := range c.Members {
			b.col0, b.col1 = min(b.col0, sl.col[v]), max(b.col1, sl.col[v])
			b.row0, b.row1 = min(b.row0, sl.row[v]), max(b.row1, sl.row[v])
			b.ok = true
		}
		b.left, b.right, b.top, b.bottom = 1, 1, 1, 1
		// A hoop hangs outside the box it belongs to and inside the frame
		// its node stands in, so the frame has to stand off by that much.
		for _, v := range c.Members {
			if sl.col[v] == b.col0 {
				b.padL = max(b.padL, wide[v])
			}
			if sl.col[v] == b.col1 {
				b.padR = max(b.padR, wide[v])
			}
			if sl.row[v] == b.row0 {
				b.padT = max(b.padT, room[v])
			}
			if sl.row[v] == b.row1 {
				b.padB = max(b.padB, room[v])
			}
		}
		out[i] = b
	}
	// A cluster's frame stands outside its children's, so a side shared
	// with a child is one ring further out. The list is written parents
	// first, so counting backwards counts from the inside.
	for i := len(out) - 1; i >= 0; i-- {
		p := g.Clusters[i].Parent
		if p < 0 || !out[i].ok || !out[p].ok {
			continue
		}
		if out[p].col0 == out[i].col0 {
			out[p].left = max(out[p].left, out[i].left+1)
			out[p].padL = max(out[p].padL, out[i].padL)
		}
		if out[p].col1 == out[i].col1 {
			out[p].right = max(out[p].right, out[i].right+1)
			out[p].padR = max(out[p].padR, out[i].padR)
		}
		if out[p].row0 == out[i].row0 {
			out[p].top = max(out[p].top, out[i].top+1)
			out[p].padT = max(out[p].padT, out[i].padT)
		}
		if out[p].row1 == out[i].row1 {
			out[p].bottom = max(out[p].bottom, out[i].bottom+1)
			out[p].padB = max(out[p].padB, out[i].padB)
		}
	}
	return out
}

// offL and its three sisters are how far outside its contents each side
// of a frame is drawn: the air a hoop needs, then a ring for every frame
// standing outside this one on that side.
func (b bound) offL() int { return b.padL + 2*b.left }
func (b bound) offR() int { return b.padR + 2*b.right }
func (b bound) offT() int { return b.padT + 2*b.top }
func (b bound) offB() int { return b.padB + 2*b.bottom }

// frameRects turns the slot bounds into the rectangles the frames are
// drawn as, once the columns and rows have their real sizes.
func frameRects(g *layout.Graph, bs []bound, xs, ys, colW, rowH []int) []Box {
	out := make([]Box, len(bs))
	for i, b := range bs {
		if !b.ok {
			continue
		}
		x0 := xs[b.col0] - b.offL()
		y0 := ys[b.row0] - b.offT()
		x1 := xs[b.col1] + colW[b.col1] - 1 + b.offR()
		y1 := ys[b.row1] + rowH[b.row1] - 1 + b.offB()
		c := g.Clusters[i]
		out[i] = Box{X: x0, Y: y0, W: x1 - x0 + 1, H: y1 - y0 + 1,
			Pencil: Pencil{Style: styleOf(c.Style, 0), Pen: ParsePen(c.Pen)},
			Ink:    ParsePen(c.FontPen), Title: c.Label}
	}
	return out
}

// drawFrame lays a cluster's frame: four walls, and its name set into the
// top edge where there is room for it and on the air row under the edge
// where there is not. Only the ring is spoken for — everything inside a
// frame belongs to what the frame is round.
// titleRoom is the narrowest frame a cluster's name reads in. Set into the
// top edge the name needs the edge to still read as an edge — a third of
// it drawn is the reader's bar — and set on the air row inside it needs
// only a margin each side.
func titleRoom(title string) int {
	n := grid.Cells(title)
	if n == 0 {
		return 0
	}
	if plainTitle(title) {
		if w := (3*n+7)/2 + 2; w > n+6 {
			return w
		}
		return n + 6
	}
	return n + 6
}

// plainTitle says whether every rune of a name is writing. A name set into
// the top edge is read off the edge itself, so a colon or a hash in it —
// runes a box drawing spends on lines — comes back as a blank and the name
// comes back wrong. Those go on the air row inside the frame instead.
func plainTitle(title string) bool {
	for _, r := range title {
		if r != ' ' && InAlphabet(r) {
			return false
		}
	}
	return true
}

func drawFrame(cv *Canvas, b Box) {
	x1, y1 := b.X+b.W-1, b.Y+b.H-1
	cv.Stroke(b.X, b.Y, x1, b.Y, b.Pencil)
	cv.Stroke(b.X, y1, x1, y1, b.Pencil)
	cv.Stroke(b.X, b.Y, b.X, y1, b.Pencil)
	cv.Stroke(x1, b.Y, x1, y1, b.Pencil)
	if n := grid.Cells(b.Title); n > 0 {
		// A name may be set into the edge only while enough edge is left
		// to read as an edge and while every rune of it is writing; past
		// that it stands on the air row inside, which is where graph-easy
		// puts one and where a reader looks for it second. A name inside
		// keeps a ring of air round it, so no line ever comes close
		// enough for a reader to give the words to that line instead.
		if plainTitle(b.Title) && 2*(b.W-2) >= 3*(n+2) && b.W >= n+6 {
			cv.Blank(b.X+2, b.Y, n+2)
			cv.Text(b.X+3, b.Y, b.Title, b.Ink)
		} else if b.W > n+4 {
			cv.Text(b.X+2, b.Y+1, b.Title, b.Ink)
			cv.Hold(b.X+1, b.Y+1, n+2, 1)
			cv.Hold(b.X+1, b.Y+2, n+2, 1)
		}
	}
	cv.Hold(b.X, b.Y, b.W, 1)
	cv.Hold(b.X, y1, b.W, 1)
	cv.Hold(b.X, b.Y, 1, b.H)
	cv.Hold(x1, b.Y, 1, b.H)
}

// enclosing is the clusters round each node, outermost first.
func enclosing(g *layout.Graph) [][]int {
	out := make([][]int, len(g.Nodes))
	for i, n := range g.Nodes {
		for c := n.Cluster; c >= 0; c = g.Clusters[c].Parent {
			out[i] = append([]int{c}, out[i]...)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
