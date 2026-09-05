// route.go — edges live on the grid.
//
// graphviz places; this routes. The old rasteriser projected graphviz's
// beziers onto the cell grid after the fact, and every seam in that
// projection was an artifact with a name: staircases, arrowheads on
// corners, stubs dangling past a border, a loop's wall drawn twice.
// Routing natively makes those unrepresentable. An edge here is born as
// a path of cells, found by a cost search that already knows where the
// walls are, which cells other edges own, and which wall cell its
// arrowhead has to meet.
//
// Composition is normalised before anything routes: ranks snap to
// shared rows and columns, and a node with a single parent pulls onto
// its parent's axis when nothing collides. A chain renders as one
// straight line of arrows, because straightness is most of what "drawn
// on purpose" looks like; the search's costs — a bend is dear, a
// crossing dearer than distance — buy the rest.
//
// The division holds unchanged: graphviz still answers the genuinely
// hard question (which boxes go where so edges can behave), and
// everything after that answer is derived in cell space, never carried.

package cells

import (
	"container/heap"
	"sort"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// ---------- composition ----------

// centres projects graphviz's placement into cells once. Every decision
// after this line is made in cell space.
func centres(l *layout.Plain, sx, sy func(float64) int) ([]int, []int) {
	cx := make([]int, len(l.Nodes))
	cy := make([]int, len(l.Nodes))
	for i, n := range l.Nodes {
		cx[i], cy[i] = sx(n.X), sy(n.Y)
	}
	return cx, cy
}

// snapAxis clusters values that landed within a cell of each other and
// pins each cluster to its median. graphviz's real-valued centres carry
// sub-cell noise, and two boxes meant to share a rank must share it
// exactly, or nothing downstream reads as aligned.
func snapAxis(v []int) {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	for i := 0; i < len(idx); {
		j := i + 1
		for j < len(idx) && v[idx[j]]-v[idx[j-1]] <= 1 {
			j++
		}
		m := v[idx[(i+j)/2]]
		for k := i; k < j; k++ {
			v[idx[k]] = m
		}
		i = j
	}
}

// straighten pulls a single-parent node onto its parent's axis, so a
// chain runs dead straight. Only when nothing collides: the gutter of
// blank cells between boxes is part of what is being bought.
func straighten(l *layout.Plain, cx, cy []int, byName map[string]int, horiz bool) {
	indeg := make([]int, len(cx))
	parent := make([]int, len(cx))
	for i := range parent {
		parent[i] = -1
	}
	for _, e := range l.Edges {
		a, aok := byName[e.Tail]
		b, bok := byName[e.Head]
		if !aok || !bok || a == b {
			continue
		}
		indeg[b]++
		parent[b] = a
	}
	bw := func(i int) int { return grid.Cells(l.Nodes[i].Label) + 2 }
	order := make([]int, len(cx))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		if horiz {
			return cx[order[a]] < cx[order[b]]
		}
		return cy[order[a]] < cy[order[b]]
	})
	// three passes, in flow order, so an alignment propagates down a chain
	for pass := 0; pass < 3; pass++ {
		for _, b := range order {
			if indeg[b] != 1 || parent[b] < 0 {
				continue
			}
			a := parent[b]
			want, cur := cy[a], cy[b]
			if !horiz {
				want, cur = cx[a], cx[b]
			}
			if want == cur {
				continue
			}
			ok := true
			for o := range cx {
				if o == b {
					continue
				}
				if horiz {
					if abs(cx[o]-cx[b]) < (bw(o)+bw(b))/2+2 && abs(cy[o]-want) < 4 {
						ok = false
						break
					}
				} else {
					if abs(cy[o]-cy[b]) < 4 && abs(cx[o]-want) < (bw(o)+bw(b))/2+2 {
						ok = false
						break
					}
				}
			}
			if ok {
				if horiz {
					cy[b] = want
				} else {
					cx[b] = want
				}
			}
		}
	}
}

// nodeBoxAt is nodeBox in cell space: the box a label needs, centred at
// (cx, cy), clamped into the canvas.
func nodeBoxAt(w, h, cx, cy int, label string) nbox {
	bw := grid.Cells(label) + 2
	if bw > w {
		bw = w
	}
	x0 := cx - bw/2
	if x0 < 0 {
		x0 = 0
	}
	if x0+bw > w {
		x0 = w - bw
	}
	if h < 3 {
		return nbox{x0, cy, bw, 1}
	}
	y0 := cy - 1
	if y0 < 0 {
		y0 = 0
	}
	if y0+2 >= h {
		y0 = h - 3
	}
	return nbox{x0, y0, bw, 3}
}

// ---------- ports ----------
//
// One edge attaches at the middle of the wall that faces its partner;
// several sharing a wall spread outward from the middle, one cell apart.
// A port never lands on a corner — the corner glyphs have no wall to
// admit a stroke, which is where arrows used to grow out of ┌.

type portKey struct {
	x0, y0 int
	side   int8
	slot   int8
}

type porter struct{ used map[portKey]bool }

func newPorter() *porter { return &porter{used: map[portKey]bool{}} }

// claim picks the wall of b facing (tx, ty) and the first free slot on
// it, middle first, spreading outward. The aspect correction matters: a
// row reads about twice a column, so a partner one rank away and a
// little aside still deserves the flow-facing wall, not the side.
func (pt *porter) claim(b nbox, tx, ty int, boxes []nbox) port {
	cxx, cyy := b.x0+b.w/2, b.y0+b.h/2
	dx, dy := tx-cxx, ty-cyy
	var side int8 // 0 right, 1 left, 2 bottom, 3 top
	if abs(dx) > 2*abs(dy) {
		if dx < 0 {
			side = 1
		}
	} else {
		side = 2
		if dy < 0 {
			side = 3
		}
	}
	inAny := func(x, y int) bool {
		for _, o := range boxes {
			if o.has(x, y) {
				return true
			}
		}
		return false
	}
	base := 0
	switch side {
	case 0, 1:
		lo, hi := b.y0+1, b.y0+b.h-2
		if b.h < 3 {
			lo, hi = b.y0, b.y0
		}
		base = clamp(ty, lo, hi) - cyy
	default:
		base = clamp(tx, b.x0+1, b.x0+b.w-2) - cxx
	}
	mk := func(slot int) (port, bool) {
		switch side {
		case 0, 1:
			lo, hi := b.y0+1, b.y0+b.h-2
			if b.h < 3 {
				lo, hi = b.y0, b.y0
			}
			y := cyy + slot
			if y < lo || y > hi {
				return port{}, false
			}
			if side == 0 {
				return port{b.x0 + b.w, y, '◀', true}, true
			}
			return port{b.x0 - 1, y, '▶', true}, true
		default:
			lo, hi := b.x0+1, b.x0+b.w-2
			x := cxx + slot
			if x < lo || x > hi {
				return port{}, false
			}
			if side == 2 {
				return port{x, b.y0 + b.h, '▲', false}, true
			}
			return port{x, b.y0 - 1, '▼', false}, true
		}
	}
	for _, off := range []int{0, 1, -1, 2, -2, 3, -3, 4, -4} {
		p, ok := mk(base + off)
		if !ok || inAny(p.x, p.y) {
			continue
		}
		k := portKey{b.x0, b.y0, side, int8(base + off)}
		if pt.used[k] {
			continue
		}
		pt.used[k] = true
		return p
	}
	// every slot spoken for: share the middle. Two strokes joining into
	// one arrow reads as a join, which is the honest picture of it.
	p, _ := mk(0)
	return p
}

// ---------- the search ----------

const (
	costStep  = 1
	costTurn  = 12
	costCross = 8
)

// directions indexed like the link bits: up, right, down, left
var dxs = [4]int{0, 1, 0, -1}
var dys = [4]int{-1, 0, 1, 0}

func outDir(head rune) int {
	switch head {
	case '◀':
		return 1
	case '▶':
		return 3
	case '▲':
		return 2
	default:
		return 0
	}
}

func inDir(head rune) int {
	switch head {
	case '▶':
		return 1
	case '◀':
		return 3
	case '▼':
		return 2
	default:
		return 0
	}
}

type pqItem struct{ st, cost int }
type pq []pqItem

func (q pq) Len() int           { return len(q) }
func (q pq) Less(a, b int) bool { return q[a].cost < q[b].cost }
func (q pq) Swap(a, b int)      { q[a], q[b] = q[b], q[a] }
func (q *pq) Push(x any)        { *q = append(*q, x.(pqItem)) }
func (q *pq) Pop() any {
	old := *q
	n := len(old)
	it := old[n-1]
	*q = old[:n-1]
	return it
}

// route finds the cheapest orthogonal path from the tail port to the
// head port. Bends are dear — the reader pays for every one — and a
// crossing costs more than distance, so the search trades length for
// calm on the reader's terms. A cell another edge already runs along
// the same way is simply a wall: two strokes sharing a lane would fuse
// into one line nobody drew. Returns nil when every route is blocked.
func route(cv *canvas, boxes []nbox, tp, hp port) []ipt {
	W, H := cv.w, cv.h
	if !cv.in(tp.x, tp.y) || !cv.in(hp.x, hp.y) {
		return nil
	}
	goalD := inDir(hp.head)
	inBox := func(x, y int) bool {
		for _, b := range boxes {
			if b.has(x, y) {
				return true
			}
		}
		return false
	}
	blocked := func(x, y, d int) bool {
		if x < 0 || y < 0 || x >= W || y >= H {
			return true
		}
		if x == hp.x && y == hp.y {
			return d != goalD // the goal admits only the arrow's direction
		}
		if inBox(x, y) {
			return true
		}
		i := y*W + x
		if cv.held[i] {
			return true
		}
		if r := cv.c[i]; r != ' ' && r != 0 {
			return true // an arrowhead or a label already lives here
		}
		lb := cv.link[i]
		if d == 1 || d == 3 {
			return lb&(dirLeft|dirRight) != 0
		}
		return lb&(dirUp|dirDown) != 0
	}
	crossAt := func(x, y, d int) int {
		lb := cv.linksAt(x, y)
		if d == 1 || d == 3 {
			if lb&(dirUp|dirDown) != 0 {
				return costCross
			}
		} else if lb&(dirLeft|dirRight) != 0 {
			return costCross
		}
		return 0
	}
	const inf = 1 << 30
	dist := make([]int, W*H*4)
	prev := make([]int, W*H*4)
	for i := range dist {
		dist[i] = inf
		prev[i] = -1
	}
	pack := func(x, y, d int) int { return (y*W+x)*4 + d }
	sd := pack(tp.x, tp.y, outDir(tp.head))
	dist[sd] = 0
	q := &pq{{sd, 0}}
	goal := -1
	for q.Len() > 0 {
		it := heap.Pop(q).(pqItem)
		if it.cost > dist[it.st] {
			continue
		}
		d := it.st & 3
		x := (it.st >> 2) % W
		y := (it.st >> 2) / W
		if x == hp.x && y == hp.y && d == goalD {
			goal = it.st
			break
		}
		for nd := 0; nd < 4; nd++ {
			if nd == (d+2)%4 {
				continue // no reversing in place
			}
			nx, ny := x+dxs[nd], y+dys[nd]
			if blocked(nx, ny, nd) {
				continue
			}
			c := it.cost + costStep + crossAt(nx, ny, nd)
			if nd != d {
				c += costTurn
			}
			ns := pack(nx, ny, nd)
			if c < dist[ns] {
				dist[ns] = c
				prev[ns] = it.st
				heap.Push(q, pqItem{ns, c})
			}
		}
	}
	if goal < 0 {
		return nil
	}
	var ps []ipt
	for st := goal; st >= 0; st = prev[st] {
		x := (st >> 2) % W
		y := (st >> 2) / W
		if len(ps) == 0 || ps[len(ps)-1] != (ipt{x, y}) {
			ps = append(ps, ipt{x, y})
		}
	}
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
	return ps
}

// elbow is the fallback when the search finds nothing: a direct L in
// unit steps, overlaps accepted. A line that shares a lane is worse
// than a routed one and better than an edge that silently is not there.
func elbow(tp, hp port) []ipt {
	ps := []ipt{{tp.x, tp.y}}
	x, y := tp.x, tp.y
	for x != hp.x {
		if hp.x > x {
			x++
		} else {
			x--
		}
		ps = append(ps, ipt{x, y})
	}
	for y != hp.y {
		if hp.y > y {
			y++
		} else {
			y--
		}
		ps = append(ps, ipt{x, y})
	}
	return ps
}

// commitRoute writes a path onto the canvas as link bits, joins the tail
// to its wall, and ends the head in its arrow.
func commitRoute(cv *canvas, ps []ipt, tp, hp port, directed bool) {
	for i := 0; i+1 < len(ps); i++ {
		a, b := ps[i], ps[i+1]
		switch {
		case b.x > a.x:
			cv.connect(a.x, a.y, dirRight)
			cv.connect(b.x, b.y, dirLeft)
		case b.x < a.x:
			cv.connect(a.x, a.y, dirLeft)
			cv.connect(b.x, b.y, dirRight)
		case b.y > a.y:
			cv.connect(a.x, a.y, dirDown)
			cv.connect(b.x, b.y, dirUp)
		default:
			cv.connect(a.x, a.y, dirUp)
			cv.connect(b.x, b.y, dirDown)
		}
	}
	cv.attachTail(tp)
	// the port cell itself carries the wall-ward bit, or the stroke
	// under a ┬ starts one cell adrift of the border it grew from
	cv.connect(tp.x, tp.y, arrowBit(tp.head))
	if !directed {
		// no head to draw: the stroke simply reaches the far wall too
		cv.attachTail(hp)
		cv.connect(hp.x, hp.y, arrowBit(hp.head))
		return
	}
	cv.set(hp.x, hp.y, hp.head)
}

// arrowBit is the link bit pointing the way an arrowhead rune points.
func arrowBit(head rune) uint8 {
	switch head {
	case '▲':
		return dirUp
	case '▼':
		return dirDown
	case '◀':
		return dirLeft
	default:
		return dirRight
	}
}

// ---------- labels in the stroke ----------

// placeInline writes a label inside its own line — ── bytes ──▶ — with a
// blank shoulder each side. A word floating beside three edges belongs
// to whichever one the reader guesses; a word interrupting a stroke
// belongs to that stroke. graphviz already spaced the ranks for the
// label's width, so the straight run is usually there to spend.
func placeInline(cv *canvas, ps []ipt, label string) bool {
	need := grid.Cells(label)
	if need == 0 {
		return false
	}
	// longest horizontal run, interior cells only: ports and arrow stay
	best, bi, bj := 0, -1, -1
	for i := 1; i < len(ps)-1; {
		j := i
		for j+1 < len(ps)-1 && ps[j+1].y == ps[i].y {
			j++
		}
		if l := j - i + 1; l > best {
			best, bi, bj = l, i, j
		}
		i = j + 1
	}
	if bi < 0 || best < need {
		return false
	}
	y := ps[bi].y
	lo, hi := ps[bi].x, ps[bj].x
	if lo > hi {
		lo, hi = hi, lo
	}
	x0 := (lo+hi+1)/2 - need/2
	if x0 < lo {
		x0 = lo
	}
	if x0+need-1 > hi {
		x0 = hi - need + 1
	}
	// never sever another edge: a crossing inside the window keeps its
	// stroke, and the label slides along its own run instead
	clear := func(x0 int) bool {
		for k := 0; k < need; k++ {
			if cv.linksAt(x0+k, y)&(dirUp|dirDown) != 0 {
				return false
			}
		}
		return true
	}
	placed := false
	for _, try := range []int{x0, lo, hi - need + 1} {
		if try >= lo && try+need-1 <= hi && clear(try) {
			x0, placed = try, true
			break
		}
	}
	if !placed {
		return false
	}
	putStr(cv, x0, y, label)
	cv.hold(x0, y, need, 1)
	return true
}

// ---------- self-loops ----------

// routeSelfLoop hooks an edge over the top of its own box, feet joined
// into the border it leaves and returns to. It needs an interior column
// on each side and two rows of air; a box too narrow or too high up
// gets no loop — absent beats meaningless.
func routeSelfLoop(cv *canvas, b nbox) {
	if b.y0 < 2 || b.w < 4 {
		return
	}
	lx, rx := b.x0+1, b.x0+b.w-2
	top := b.y0 - 2
	hrun(cv, lx, rx, top)
	vrun(cv, top, b.y0-1, lx)
	vrun(cv, top, b.y0-1, rx)
	cv.set(rx, b.y0-1, '▼')
	cv.port[b.y0*cv.w+lx] = true
	cv.connect(lx, b.y0, dirUp)
}

// ---------- composition root ----------

// Draw lays a DOT source out and draws it into a w x h cell box.
// graphviz's placement arrives in inches; everything visible is decided
// here, in cells: the composition is normalised, every edge is routed
// natively, labels ride their own strokes. Returns nil if it will not
// fit — the caller then leaves the source alone, which is still the whole
// failure policy: a failure is visible, never silent.
func Draw(l *layout.Plain, w, h int) []string {
	if w < 12 || h < 3 || l == nil {
		return nil
	}
	nw, nh := layout.Footprint(l)
	if nw <= 0 || nh <= 0 || nw > w || nh > h {
		return nil // does not fit: the source speaks for itself
	}
	// left-aligned: a diagram sitting beside the prose that introduced it
	// reads better than one floating in the middle of the window
	offX, offY := 0, (h-nh)/2
	sx := func(x float64) int { return offX + int(x*layout.CellsPerInchX) }
	sy := func(y float64) int { return offY + int((l.H-y)*layout.RowsPerInchY) }

	byName := make(map[string]int, len(l.Nodes))
	for i, n := range l.Nodes {
		byName[n.Name] = i
	}
	cx, cy := centres(l, sx, sy)
	horiz := l.Horiz
	snapAxis(cx)
	snapAxis(cy)
	straighten(l, cx, cy, byName, horiz)

	boxes := make(map[string]nbox, len(l.Nodes))
	boxList := make([]nbox, 0, len(l.Nodes))
	for i, n := range l.Nodes {
		b := nodeBoxAt(w, h, cx[i], cy[i], n.Label)
		boxes[n.Name] = b
		boxList = append(boxList, b)
	}

	cv := newCanvas(w, h)
	pt := newPorter()

	// straight edges first: they own the direct lanes, and everything
	// else bends around what is already true.
	order := make([]int, len(l.Edges))
	for i := range order {
		order[i] = i
	}
	cost := func(e layout.Edge) (int, int) {
		a, aok := byName[e.Tail]
		b, bok := byName[e.Head]
		if !aok || !bok {
			return 2, 1 << 20
		}
		s := 1
		if (horiz && cy[a] == cy[b]) || (!horiz && cx[a] == cx[b]) {
			s = 0
		}
		return s, abs(cx[a]-cx[b]) + abs(cy[a]-cy[b])
	}
	sort.SliceStable(order, func(a, b int) bool {
		sa, la := cost(l.Edges[order[a]])
		sb, lb := cost(l.Edges[order[b]])
		if sa != sb {
			return sa < sb
		}
		return la < lb
	})

	var floated []*layout.Edge // labels that found no straight run to ride
	for _, ei := range order {
		e := &l.Edges[ei]
		if e.Tail != "" && e.Tail == e.Head {
			if b, ok := boxes[e.Tail]; ok {
				routeSelfLoop(cv, b)
			}
			continue
		}
		tb, tok := boxes[e.Tail]
		hb, hok := boxes[e.Head]
		if !tok || !hok {
			continue
		}
		tp := pt.claim(tb, hb.x0+hb.w/2, hb.y0+hb.h/2, boxList)
		hp := pt.claim(hb, tb.x0+tb.w/2, tb.y0+tb.h/2, boxList)
		ps := route(cv, boxList, tp, hp)
		if ps == nil {
			ps = elbow(tp, hp)
		}
		commitRoute(cv, ps, tp, hp, l.Directed)
		if e.Label != "" && !placeInline(cv, ps, e.Label) {
			floated = append(floated, e)
		}
	}
	for _, n := range l.Nodes {
		drawNode(cv, n.Label, boxes[n.Name])
	}
	for _, e := range floated {
		drawEdgeLabel(cv, *e, sx, sy)
	}
	return cv.rows()
}

// clamp pins v into [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
