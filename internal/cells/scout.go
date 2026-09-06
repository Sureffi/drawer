// scout.go — one edge's way across the drawing.
//
// The scout is an A* over the cells themselves. It knows three things the
// drawing will not forgive it for getting wrong:
//
//   - Two edges may cross and may never join. A reader takes one connected
//     run of line cells as one edge, so a cell where a second line turns or
//     tees is two edges fused into a shape neither of them is. The scout
//     therefore enters an occupied cell only where the line already there
//     runs straight across its own path, and leaves it in the same
//     direction it came: a crossing, and nothing else.
//   - A bend costs the reader more than a cell of distance, and a crossing
//     more than either. The prices below are what "keeps lanes apart" means
//     in one line of arithmetic.
//   - A line laid along a box's own top or bottom row can close a rectangle
//     nobody drew, so those rows are dear to travel along.
//
// It is one edge at a time, and the canvas it reads is the canvas as it
// stands: an edge routed early is a wall to the ones after it, which is why
// the order they are routed in is part of the layout and not an accident.

package cells

import "container/heap"

// The prices. A step is one; everything else is measured against it.
const (
	priceStep  = 1
	priceTurn  = 10
	priceCross = 30
	priceWall  = 6 // travelling along a box's own edge row or column
)

// Route is one edge's path, cell by cell, the ports at both ends included.
type Route []ipt

type ipt struct{ x, y int }

// terrain is what every edge on one canvas shares: the cells no line may
// enter at all, and the rows and columns that are dear to travel along.
type terrain struct {
	w, h    int
	solid   []bool // inside a box: a line there is a hole in a wall
	ringm   []bool // a cluster frame's own line
	rowWall []bool // a row that is some box's top or bottom edge
	colWall []bool // a column that is some box's left or right edge
}

func newTerrain(w, h int, boxes []Box) *terrain {
	t := &terrain{w: w, h: h,
		solid:   make([]bool, w*h),
		ringm:   make([]bool, w*h),
		rowWall: make([]bool, h),
		colWall: make([]bool, w),
	}
	for _, b := range boxes {
		for y := b.Y; y < b.Y+b.H; y++ {
			for x := b.X; x < b.X+b.W; x++ {
				if x >= 0 && y >= 0 && x < w && y < h {
					t.solid[y*w+x] = true
				}
			}
		}
		for _, y := range []int{b.Y, b.Y + b.H - 1} {
			if y >= 0 && y < h {
				t.rowWall[y] = true
			}
		}
		for _, x := range []int{b.X, b.X + b.W - 1} {
			if x >= 0 && x < w {
				t.colWall[x] = true
			}
		}
	}
	return t
}

// ring walls off a cluster's frame and nothing else. A line may not cross
// a frame — a reader takes a line stopped at a border as a line that ended
// there, so an edge drawn through one is an edge cut in half — and
// everything inside the frame is exactly what the frame is round.
func (t *terrain) ring(b Box) {
	mark := func(x, y int) {
		if x >= 0 && y >= 0 && x < t.w && y < t.h {
			t.solid[y*t.w+x] = true
			t.ringm[y*t.w+x] = true
		}
	}
	for x := b.X; x < b.X+b.W; x++ {
		mark(x, b.Y)
		mark(x, b.Y+b.H-1)
	}
	for y := b.Y; y < b.Y+b.H; y++ {
		mark(b.X, y)
		mark(b.X+b.W-1, y)
	}
	if b.Y >= 0 && b.Y < t.h {
		t.rowWall[b.Y] = true
	}
	if b.Y+b.H-1 >= 0 && b.Y+b.H-1 < t.h {
		t.rowWall[b.Y+b.H-1] = true
	}
	if b.X >= 0 && b.X < t.w {
		t.colWall[b.X] = true
	}
	if b.X+b.W-1 >= 0 && b.X+b.W-1 < t.w {
		t.colWall[b.X+b.W-1] = true
	}
}

// isRing says whether a cell is some frame's own line. A reader walks a
// line's end outward across blanks and frames alike, so a corridor kept
// clear for one may pass through a frame and still be read.
func (t *terrain) isRing(x, y int) bool {
	return x >= 0 && y >= 0 && x < t.w && y < t.h && t.ringm[y*t.w+x]
}

func (t *terrain) blocked(x, y int) bool {
	return x < 0 || y < 0 || x >= t.w || y >= t.h || t.solid[y*t.w+x]
}

type pqItem struct{ st, cost, est int }
type pq []pqItem

func (q pq) Len() int           { return len(q) }
func (q pq) Less(a, b int) bool { return q[a].est < q[b].est }
func (q pq) Swap(a, b int)      { q[a], q[b] = q[b], q[a] }
func (q *pq) Push(x any)        { *q = append(*q, x.(pqItem)) }
func (q *pq) Pop() any {
	old := *q
	n := len(old)
	it := old[n-1]
	*q = old[:n-1]
	return it
}

// scoutEdge finds the cheapest way from one port to the other, or nil.
//
// A state is a cell and the direction the scout arrived travelling, because
// a bend is a cost and a cost that depends on where you came from needs the
// direction in the state. The goal is the head port entered the way the
// arrowhead there points, so an arrow never ends up on the wrong side of
// its own line.
func scoutEdge(cv *Canvas, t *terrain, tp, hp port, budget int) Route {
	w := t.w
	if t.blocked(tp.x, tp.y) || t.blocked(hp.x, hp.y) {
		return nil
	}
	if tp.x == hp.x && tp.y == hp.y {
		return nil
	}
	// passable answers the one law: an empty cell takes any line, an
	// occupied one takes a line that crosses it square and keeps going.
	passable := func(x, y int, d Dir) (cost int, cross bool, ok bool) {
		if t.blocked(x, y) {
			return 0, false, false
		}
		if cv.Held(x, y) || cv.Rune(x, y) != ' ' {
			return 0, false, false // a label or an arrowhead already lives here
		}
		m := cv.MaskAt(x, y).Arms()
		if m == 0 {
			c := 0
			if d == East || d == West {
				if t.rowWall[y] {
					c = priceWall
				}
			} else if t.colWall[x] {
				c = priceWall
			}
			return c, false, true
		}
		if d == East || d == West {
			if m.Vert() && !m.Horiz() {
				return priceCross, true, true
			}
			return 0, false, false
		}
		if m.Horiz() && !m.Vert() {
			return priceCross, true, true
		}
		return 0, false, false
	}
	const inf = 1 << 30
	// A state carries the direction and whether the cell it stands on is a
	// crossing, because a crossing may only be left the way it was entered.
	pack := func(x, y int, d Dir) int { return (y*w+x)*4 + int(d) }
	dist := make(map[int]int, 1024)
	prev := make(map[int]int, 1024)
	locked := make(map[int]bool, 64)
	start := pack(tp.x, tp.y, tp.d.Opposite())
	goal := pack(hp.x, hp.y, hp.d)
	dist[start] = 0
	est := func(x, y int, d Dir) int {
		dx, dy := abs(hp.x-x), abs(hp.y-y)
		c := dx + dy
		if dx != 0 && dy != 0 {
			c += priceTurn
		}
		return c
	}
	q := &pq{{start, 0, est(tp.x, tp.y, tp.d.Opposite())}}
	steps := 0
	for q.Len() > 0 {
		it := heap.Pop(q).(pqItem)
		if d, ok := dist[it.st]; !ok || it.cost > d {
			continue
		}
		if it.st == goal {
			return unwind(prev, start, goal, w)
		}
		if steps++; budget > 0 && steps > budget {
			return nil
		}
		d := Dir(it.st & 3)
		x := (it.st >> 2) % w
		y := (it.st >> 2) / w
		for nd := North; nd <= West; nd++ {
			if nd == d.Opposite() {
				continue // a line does not double back on itself
			}
			if locked[it.st] && nd != d {
				continue // a crossing is left the way it was entered
			}
			dx, dy := nd.Step()
			nx, ny := x+dx, y+dy
			if nx == tp.x && ny == tp.y {
				continue
			}
			var c int
			var cross bool
			if nx == hp.x && ny == hp.y {
				if nd != hp.d {
					continue // the head port admits the arrow's direction only
				}
			} else {
				var ok bool
				c, cross, ok = passable(nx, ny, nd)
				if !ok {
					continue
				}
			}
			cost := it.cost + priceStep + c
			if nd != d {
				cost += priceTurn
			}
			ns := pack(nx, ny, nd)
			if old, ok := dist[ns]; !ok || cost < old {
				dist[ns] = cost
				prev[ns] = it.st
				if cross {
					locked[ns] = true
				}
				heap.Push(q, pqItem{ns, cost, cost + est(nx, ny, nd)})
			}
		}
	}
	return nil
}

func unwind(prev map[int]int, start, goal, w int) Route {
	var ps Route
	for st := goal; ; {
		x := (st >> 2) % w
		y := (st >> 2) / w
		ps = append(ps, ipt{x, y})
		if st == start {
			break
		}
		p, ok := prev[st]
		if !ok {
			return nil
		}
		st = p
	}
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
	return ps
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
