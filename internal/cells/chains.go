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

// links is a graph as the placer needs it: unique successors and
// predecessors, self-loops and repeats taken out, because a chain is about
// shape and a second edge between the same pair is not a second shape.
// What it is a graph *of* is not always the nodes — inside a cluster the
// items are that cluster's own nodes and the clusters nested in it, and
// the placer never has to know the difference.
type links struct {
	succ, pred [][]int
}

func linksOver(n int, edges [][2]int) links {
	l := links{succ: make([][]int, n), pred: make([][]int, n)}
	seen := map[[2]int]bool{}
	for _, e := range edges {
		if e[0] == e[1] || e[0] < 0 || e[1] < 0 || seen[e] {
			continue
		}
		seen[e] = true
		l.succ[e[0]] = append(l.succ[e[0]], e[1])
		l.pred[e[1]] = append(l.pred[e[1]], e[0])
	}
	return l
}

// reach is the length of the longest run of items leading away from each
// one. It is what decides which successor a chain follows: the branch with
// the most left in it gets the straight line, and the short branches bend
// off it. A cycle stops at the item it came back to.
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

// chainsOf cuts a graph into paths, longest first. Every item is in
// exactly one, which is what lets a chain own a lane.
func chainsOf(n int, l links) [][]int {
	r := reach(l)
	// The order chains are started in: an item nothing points at first,
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

// spot is where a thing stands on the slot grid: how far along the flow,
// and which lane across it.
type spot struct{ f, c int }

// place lays the chains out, and the order it takes them in is the whole
// of why the picture is tidy: the longest chain first, then — from its
// last item back to its first — every chain hanging off it, recursively.
// A subtree is finished before the next one is started, so the lanes come
// out in the order a reader would draw them and a tree crosses nowhere.
//
// A chain starts one past the last of its members' already-placed parents,
// so it never has to be reached backwards, and takes the first free lane
// at or past the lane the walk has got to. The lane only ever moves
// forward, which is what keeps two subtrees from interleaving.
//
// An item is not always one slot: a cluster is laid out on its own grid
// first and stands in this one as a rectangle the size of it.
func place(n int, l links, chains [][]int, fw, cw []int) []spot {
	at := make([]spot, n)
	done := make([]bool, n)
	used := map[spot]bool{}
	chainOf := make([]int, n)
	laid := make([]bool, len(chains))
	for i, ch := range chains {
		for _, v := range ch {
			chainOf[v] = i
		}
	}
	cursor := 0
	lay := func(chain []int) {
		// Where along the flow the chain starts, and how far each of its
		// items is from that start once the ones before it have taken
		// their own depth.
		off := make([]int, len(chain))
		d := 0
		for i, v := range chain {
			off[i] = d
			d += fw[v]
		}
		f0, lane := 0, cursor
		for i, v := range chain {
			for _, p := range l.pred[v] {
				if !done[p] {
					continue
				}
				if at[p].f+fw[p]-off[i] > f0 {
					f0 = at[p].f + fw[p] - off[i]
				}
				if i == 0 && at[p].c > lane {
					lane = at[p].c
				}
			}
		}
		wide := 0
		for _, v := range chain {
			if cw[v] > wide {
				wide = cw[v]
			}
		}
		for {
			free := true
			for i, v := range chain {
				for a := 0; a < fw[v] && free; a++ {
					for b := 0; b < wide; b++ {
						if used[spot{f0 + off[i] + a, lane + b}] {
							free = false
							break
						}
					}
				}
			}
			if free {
				break
			}
			lane++
		}
		for i, v := range chain {
			at[v] = spot{f0 + off[i], lane}
			done[v] = true
			for a := 0; a < fw[v]; a++ {
				for b := 0; b < cw[v]; b++ {
					used[spot{at[v].f + a, at[v].c + b}] = true
				}
			}
		}
		cursor = lane + wide
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

// ---------- clusters ----------

// tree is the source's clusters as a family: who is inside whom, and what
// stands directly in each. The graph itself is the root, numbered -1.
type tree struct {
	kids  map[int][]int // cluster -> the clusters directly inside it
	own   map[int][]int // cluster -> the nodes directly inside it
	owner []int         // node -> the item it belongs to at its parent's level
}

func treeOf(g *layout.Graph) tree {
	t := tree{kids: map[int][]int{}, own: map[int][]int{}}
	for i := range g.Clusters {
		t.kids[g.Clusters[i].Parent] = append(t.kids[g.Clusters[i].Parent], i)
	}
	for i, n := range g.Nodes {
		t.own[n.Cluster] = append(t.own[n.Cluster], i)
	}
	return t
}

// blockAt lays out everything under one cluster on a grid of its own: the
// nodes standing directly in it, and the clusters nested in it, each laid
// out first and standing here as one rectangle. The answer is where every
// node under it ended up, relative to the block's own corner, and how big
// the block came out.
//
// A cluster is a rectangle of slots, so nothing that is not a member can
// land inside one — which is the whole of what a frame has to promise.
func blockAt(g *layout.Graph, t tree, c int) (map[int]spot, int, int) {
	nodes := t.own[c]
	kids := t.kids[c]
	// items: the nodes first, then the child clusters.
	inner := make([]map[int]spot, len(kids))
	fw := make([]int, len(nodes)+len(kids))
	cw := make([]int, len(nodes)+len(kids))
	item := map[int]int{} // node -> item index at this level
	for i, v := range nodes {
		fw[i], cw[i] = 1, 1
		item[v] = i
	}
	for j, k := range kids {
		var w, h int
		inner[j], w, h = blockAt(g, t, k)
		fw[len(nodes)+j], cw[len(nodes)+j] = w, h
		for v := range inner[j] {
			item[v] = len(nodes) + j
		}
	}
	n := len(fw)
	if n == 0 {
		return map[int]spot{}, 0, 0
	}
	var edges [][2]int
	for _, e := range g.Edges {
		a, aok := item[e.Tail]
		b, bok := item[e.Head]
		if !aok || !bok {
			continue
		}
		edges = append(edges, [2]int{a, b})
	}
	l := linksOver(n, edges)
	at := place(n, l, chainsOf(n, l), fw, cw)
	out := make(map[int]spot, len(item))
	for i, v := range nodes {
		out[v] = at[i]
	}
	for j := range kids {
		base := at[len(nodes)+j]
		for v, s := range inner[j] {
			out[v] = spot{base.f + s.f, base.c + s.c}
		}
	}
	var maxF, maxC int
	for i := 0; i < n; i++ {
		if at[i].f+fw[i] > maxF {
			maxF = at[i].f + fw[i]
		}
		if at[i].c+cw[i] > maxC {
			maxC = at[i].c + cw[i]
		}
	}
	return out, maxF, maxC
}

// compact drops the flow columns and lanes nobody stands in, and moves the
// grid to the origin. Placing leaves holes — a chain that had to start far
// along the flow leaves everything before it empty — and a hole is a column
// of blank cells in the finished drawing. Renumbering keeps the order, so
// a cluster's members stay the block they were placed as.
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
	loops := make([]int, len(g.Nodes))
	for _, e := range g.Edges {
		if e.Tail == e.Head {
			loops[e.Tail]++
		}
	}
	for i := range g.Nodes {
		bw[i], bh[i] = boxSize(g.Nodes[i])
		// A hoop takes two ports on one wall and each one inside it takes
		// two more, so a node with self-loops needs wall to hang them on.
		// Half go above and half below, and a three-row box has one cell
		// of side wall, which is no wall at all.
		if n := 2 + 2*((loops[i]+1)/2); n > bw[i] {
			bw[i] = n
		}
		if bw[i] > colW[sl.col[i]] {
			colW[sl.col[i]] = bw[i]
		}
		if bh[i] > rowH[sl.row[i]] {
			rowH[sl.row[i]] = bh[i]
		}
	}
	room := make([]int, len(g.Nodes))
	wide := make([]int, len(g.Nodes))
	for i, n := range loops {
		if n > 0 {
			room[i] = (n+1)/2 + 2
		}
	}
	for _, e := range g.Edges {
		if e.Tail == e.Head {
			wide[e.Tail] = max(wide[e.Tail], grid.Cells(e.Label))
		}
	}
	bs := bounds(g, sl, room)
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
	return trimLeft(cv.Rows()), short
}

// spacing is the air between the slots: a base along each axis, whatever
// the ladder is paying on top of it, and room in the gap for the widest
// label that has to ride through it.
func spacing(g *layout.Graph, sl slots, extra gaps, bs []bound, room []int, wide []int) ([]int, []int) {
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
			beforeX[sl.col[v]] = max(beforeX[sl.col[v]], wide[v]+2)
			afterX[sl.col[v]] = max(afterX[sl.col[v]], wide[v]+2)
		}
	}
	for _, b := range bs {
		if !b.ok {
			continue
		}
		beforeX[b.col0] = max(beforeX[b.col0], b.offL())
		afterX[b.col1] = max(afterX[b.col1], b.offR())
		beforeY[b.row0] = max(beforeY[b.row0], b.offT())
		afterY[b.row1] = max(afterY[b.row1], b.offB())
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
func bounds(g *layout.Graph, sl slots, room []int) []bound {
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
			if room[v] == 0 {
				continue
			}
			if sl.col[v] == b.col0 {
				b.padL = max(b.padL, room[v])
			}
			if sl.col[v] == b.col1 {
				b.padR = max(b.padR, room[v])
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
