// chains.go — the layout: chains place, the scout routes, a repair pass
// fixes what the scout left.
//
// The placing is the graph's own longest paths, taken one after another,
// longest first. A chain runs dead straight along the flow — that is the
// whole of why it is worth finding chains at all — and each one takes the
// lane nearest the neighbours it already has. A node's rank is one past
// the last of its placed parents, so an edge points forward unless the
// graph itself points back.
//
// What comes out of placing is a grid of slots, not of cells: slot columns
// and slot rows, with a gap between each pair. The widths come after, one
// per column, one per row, so a column is as wide as its widest box and a
// gap is as wide as the label that has to ride through it. That is what
// makes a drawing tight without any of it having been measured twice.
//
// Then every edge is routed by the scout, in order of how far it has to
// go, and the labels go on last, where their own line will hold them.

package cells

import (
	"sort"
	"strings"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// ---------- chains ----------

// links is the graph as the placer needs it: unique successors and
// predecessors, self-loops and repeats taken out, because a chain is about
// shape and a second edge between the same pair is not a second shape.
type links struct {
	succ, pred [][]int
}

func linksOf(g *layout.Graph) links {
	n := len(g.Nodes)
	l := links{succ: make([][]int, n), pred: make([][]int, n)}
	seen := map[[2]int]bool{}
	for _, e := range g.Edges {
		if e.Tail == e.Head || seen[[2]int{e.Tail, e.Head}] {
			continue
		}
		seen[[2]int{e.Tail, e.Head}] = true
		l.succ[e.Tail] = append(l.succ[e.Tail], e.Head)
		l.pred[e.Head] = append(l.pred[e.Head], e.Tail)
	}
	return l
}

// reach is the length of the longest run of nodes leading away from each
// one. It is what decides which successor a chain follows: the branch with
// the most left in it gets the straight line, and the short branches bend
// off it. A cycle stops at the node it came back to.
func reach(l links) []int {
	n := len(l.succ)
	out := make([]int, n)
	state := make([]int8, n)
	var walk func(int) int
	walk = func(i int) int {
		if state[i] == 2 {
			return out[i]
		}
		if state[i] == 1 {
			return 0 // a cycle: it has been counted once already
		}
		state[i] = 1
		best := 0
		for _, s := range l.succ[i] {
			if r := walk(s); r > best {
				best = r
			}
		}
		out[i], state[i] = best+1, 2
		return out[i]
	}
	for i := 0; i < n; i++ {
		walk(i)
	}
	return out
}

// chainsOf cuts the graph into paths, longest first. Every node is in
// exactly one, which is what lets a chain own a lane.
func chainsOf(g *layout.Graph, l links) [][]int {
	n := len(g.Nodes)
	r := reach(l)
	// The order chains are started in: a node nothing points at first,
	// then the one with the most graph left in front of it.
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		pa, pb := len(l.pred[ia]) > 0, len(l.pred[ib]) > 0
		if pa != pb {
			return !pa
		}
		if r[ia] != r[ib] {
			return r[ia] > r[ib]
		}
		return ia < ib
	})
	taken := make([]bool, n)
	var out [][]int
	for _, start := range order {
		if taken[start] {
			continue
		}
		chain := []int{start}
		taken[start] = true
		cur := start
		for {
			next, best := -1, -1
			for _, s := range l.succ[cur] {
				if taken[s] {
					continue
				}
				if r[s] > best {
					next, best = s, r[s]
				}
			}
			if next < 0 {
				break
			}
			taken[next] = true
			chain = append(chain, next)
			cur = next
		}
		out = append(out, chain)
	}
	sort.SliceStable(out, func(a, b int) bool { return len(out[a]) > len(out[b]) })
	return out
}

// ---------- placing ----------

// spot is where a node stands on the slot grid: how far along the flow,
// and which lane across it.
type spot struct{ f, c int }

// place lays the chains out, and the order it takes them in is the whole
// of why the picture is tidy: the longest chain first, then — from its
// last node back to its first — every chain hanging off it, recursively.
// A subtree is finished before the next one is started, so the lanes come
// out in the order a reader would draw them and a tree crosses nowhere.
//
// A chain starts one past the last of its members' already-placed parents,
// so it never has to be reached backwards, and takes the first free lane
// at or past the lane the walk has got to. The lane only ever moves
// forward, which is what keeps two subtrees from interleaving.
func place(g *layout.Graph, l links, chains [][]int) []spot {
	at := make([]spot, len(g.Nodes))
	done := make([]bool, len(g.Nodes))
	used := map[spot]bool{}
	chainOf := make([]int, len(g.Nodes))
	laid := make([]bool, len(chains))
	for i, ch := range chains {
		for _, v := range ch {
			chainOf[v] = i
		}
	}
	cursor := 0
	lay := func(chain []int) {
		f0 := 0
		lane := cursor
		for i, v := range chain {
			for _, p := range l.pred[v] {
				if !done[p] {
					continue
				}
				if at[p].f+1-i > f0 {
					f0 = at[p].f + 1 - i
				}
				if i == 0 && at[p].c > lane {
					lane = at[p].c
				}
			}
		}
		for {
			free := true
			for i := range chain {
				if used[spot{f0 + i, lane}] {
					free = false
					break
				}
			}
			if free {
				break
			}
			lane++
		}
		for i, v := range chain {
			at[v] = spot{f0 + i, lane}
			used[at[v]] = true
			done[v] = true
		}
		cursor = lane + 1
	}
	var walk func(int)
	walk = func(ci int) {
		if laid[ci] {
			return
		}
		laid[ci] = true
		chain := chains[ci]
		lay(chain)
		// Deepest first: the chain's own tail carries straight on, so
		// whatever hangs off the tail belongs beside it, and whatever
		// hangs off the head belongs outside all of that.
		for i := len(chain) - 1; i >= 0; i-- {
			for _, s := range l.succ[chain[i]] {
				walk(chainOf[s])
			}
		}
	}
	for i := range chains {
		walk(i)
	}
	return at
}

// compact drops the flow columns and lanes nobody stands in, and moves the
// grid to the origin. Placing leaves holes — a chain that had to start far
// along the flow leaves everything before it empty — and a hole is a column
// of blank cells in the finished drawing.
func compact(at []spot) {
	renumber := func(get func(*spot) *int) {
		seen := map[int]bool{}
		for i := range at {
			seen[*get(&at[i])] = true
		}
		vals := make([]int, 0, len(seen))
		for v := range seen {
			vals = append(vals, v)
		}
		sort.Ints(vals)
		idx := make(map[int]int, len(vals))
		for i, v := range vals {
			idx[v] = i
		}
		for i := range at {
			p := get(&at[i])
			*p = idx[*p]
		}
	}
	renumber(func(s *spot) *int { return &s.f })
	renumber(func(s *spot) *int { return &s.c })
}

// grid is the slot grid turned the way the source asked for: a column and
// a row per node, with the flow running whichever way rankdir says.
type slots struct {
	col, row   []int
	nCol, nRow int
	horiz      bool
}

func orient(g *layout.Graph, at []spot) slots {
	s := slots{col: make([]int, len(at)), row: make([]int, len(at)), horiz: g.Horiz}
	maxF := 0
	for _, p := range at {
		if p.f > maxF {
			maxF = p.f
		}
	}
	for i, p := range at {
		f := p.f
		if g.Reverse {
			f = maxF - f
		}
		if g.Horiz {
			s.col[i], s.row[i] = f, p.c
		} else {
			s.col[i], s.row[i] = p.c, f
		}
	}
	for i := range at {
		if s.col[i]+1 > s.nCol {
			s.nCol = s.col[i] + 1
		}
		if s.row[i]+1 > s.nRow {
			s.nRow = s.row[i] + 1
		}
	}
	return s
}

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
var ladder = []gaps{{0, 0}, {2, 1}, {4, 2}, {8, 4}}

// Draw lays a graph out and draws it into a box `width` cells across and
// at most `maxRows` deep. Returns nil where the drawing will not fit,
// which is the caller's cue to show the source under a notice.
func Draw(g *layout.Graph, width, maxRows int) []string {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}
	var best []string
	bestShort := 1 << 30
	for _, flip := range orientations(g) {
		h := *g
		h.Horiz, h.Reverse = flip.horiz, flip.reverse
		l := linksOf(&h)
		at := place(&h, l, chainsOf(&h, l))
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
			// More room drew more of the graph: keep whichever attempt
			// lost the least, so a drawing that will not come out whole
			// still comes out as complete as it can.
			if best == nil || short < bestShort {
				best, bestShort = rows, short
			}
		}
	}
	return best
}

type facing struct{ horiz, reverse bool }

// orientations is the order a graph is tried in: as it was written, and
// then top-down where it was not already, because rows scroll and columns
// run out.
func orientations(g *layout.Graph) []facing {
	out := []facing{{g.Horiz, g.Reverse}}
	if g.Horiz {
		out = append(out, facing{false, false})
	}
	return out
}

// attempt draws one whole graph at one set of gaps. It answers the rows
// and how many edges and labels were lost: the caller decides whether to
// buy more room.
func attempt(g *layout.Graph, sl slots, extra gaps, width, maxRows int) ([]string, int) {
	bw := make([]int, len(g.Nodes))
	bh := make([]int, len(g.Nodes))
	colW := make([]int, sl.nCol)
	rowH := make([]int, sl.nRow)
	for i := range g.Nodes {
		bw[i], bh[i] = boxSize(g.Nodes[i])
		if bw[i] > colW[sl.col[i]] {
			colW[sl.col[i]] = bw[i]
		}
		if bh[i] > rowH[sl.row[i]] {
			rowH[sl.row[i]] = bh[i]
		}
	}
	gapX, gapY := spacing(g, sl, extra)
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
	boxes := make([]Box, len(g.Nodes))
	for i := range g.Nodes {
		boxes[i] = nodeBox(g.Nodes[i], xs[sl.col[i]], ys[sl.row[i]], colW[sl.col[i]], rowH[sl.row[i]])
	}
	for i := range boxes {
		drawNode(cv, boxes[i], g.Nodes[i])
	}
	t := newTerrain(total, deep, boxes)
	short := routeAll(cv, g, sl, boxes, t)
	return trimLeft(cv.Rows()), short
}

// spacing is the air between the slots: a base along each axis, whatever
// the ladder is paying on top of it, and room in the gap for the widest
// label that has to ride through it.
func spacing(g *layout.Graph, sl slots, extra gaps) ([]int, []int) {
	flow, cross := 3, 4 // top-down: ranks stack in rows, lanes spread in columns
	if sl.horiz {
		flow, cross = 6, 2 // left-right: ranks march in columns, lanes stack in rows
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
	// A label rides its own line, so the gap it rides through has to be
	// long enough to hold it with a shoulder each side.
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
			if b-a == 1 && gapX[b] < n+6 {
				gapX[b] = n + 6
			}
			continue
		}
		// Down the page a label stands beside its line rather than in it,
		// so the room it needs is across the flow.
		c := sl.col[e.Tail] + 1
		if c < len(gapX) && gapX[c] < n+4 {
			gapX[c] = n + 4
		}
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
