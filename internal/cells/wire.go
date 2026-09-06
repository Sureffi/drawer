// wire.go — where an edge attaches, in what order the edges are drawn, and
// where the label goes.
//
// A port is one cell outside one wall, and no two edges ever get the same
// one: two lines that meet at a wall are one line with a fork in it, and a
// fork is a thing the drawing would be saying that the graph never said. A
// node's walls hold as many ports as they have cells that are not corners
// — a corner admits nothing from the side, and a wall that is not a corner
// admits exactly one line — so a busy node spills onto the wall beside the
// one it wanted, which is what the picture should look like anyway.
//
// The order matters and is not an accident: the short edges go first and
// take the direct lanes, and everything after them bends around what is
// already true.

package cells

import (
	"sort"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// port is where an edge meets a box: the cell just outside one wall, and
// the way from there into the box — which is the way an arrowhead there
// points.
type port struct {
	x, y int
	d    Dir
}

type porter struct{ used map[ipt]bool }

func newPorter() *porter { return &porter{used: map[ipt]bool{}} }

// sideOrder is which wall an edge would rather leave by: the one facing
// its partner, then the one across the other axis, then their opposites. A
// row reads about two columns wide, so the comparison is weighted.
func sideOrder(b Box, tx, ty int) [4]Dir {
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	dx, dy := tx-cx, ty-cy
	hor, ver := East, South
	if dx < 0 {
		hor = West
	}
	if dy < 0 {
		ver = North
	}
	if abs(dx) >= 2*abs(dy) {
		return [4]Dir{hor, ver, ver.Opposite(), hor.Opposite()}
	}
	return [4]Dir{ver, hor, hor.Opposite(), ver.Opposite()}
}

// slotsOn is every cell outside one wall that an edge may attach to,
// nearest the partner first. The corners are left out: a corner glyph has
// no wall to admit a line, which is where arrows used to grow out of ┌.
func slotsOn(b Box, side Dir, tx, ty int) []ipt {
	var out []ipt
	switch side {
	case East, West:
		x := b.X + b.W
		if side == West {
			x = b.X - 1
		}
		lo, hi := b.Y+1, b.Y+b.H-2
		if b.H < 3 {
			lo, hi = b.Y, b.Y+b.H-1
		}
		for y := lo; y <= hi; y++ {
			out = append(out, ipt{x, y})
		}
		want := clamp(ty, lo, hi)
		sort.SliceStable(out, func(i, j int) bool { return abs(out[i].y-want) < abs(out[j].y-want) })
	default:
		y := b.Y + b.H
		if side == North {
			y = b.Y - 1
		}
		lo, hi := b.X+1, b.X+b.W-2
		if b.W < 3 {
			lo, hi = b.X, b.X+b.W-1
		}
		for x := lo; x <= hi; x++ {
			out = append(out, ipt{x, y})
		}
		want := clamp(tx, lo, hi)
		sort.SliceStable(out, func(i, j int) bool { return abs(out[i].x-want) < abs(out[j].x-want) })
	}
	return out
}

// claim takes the first free port on the first wall that will have it.
func (p *porter) claim(cv *Canvas, t *terrain, b Box, tx, ty int) (port, bool) {
	for _, side := range sideOrder(b, tx, ty) {
		for _, c := range slotsOn(b, side, tx, ty) {
			if p.used[c] || t.blocked(c.x, c.y) || cv.Held(c.x, c.y) {
				continue
			}
			if cv.MaskAt(c.x, c.y) != 0 || cv.Rune(c.x, c.y) != ' ' {
				continue
			}
			p.used[c] = true
			return port{c.x, c.y, side.Opposite()}, true
		}
	}
	return port{}, false
}

// budget bounds one search. A graph big enough to hit it is a graph whose
// drawing was never going to be read anyway, and an edge that runs out is
// reported short rather than left to spend the session.
const budget = 300000

// routeAll routes every edge onto the canvas and answers how much of the
// graph did not arrive — the labels included, because a label that was not
// drawn is an edge the reader cannot name.
func routeAll(cv *Canvas, g *layout.Graph, sl slots, boxes, frames []Box, encl [][]int, t *terrain) int {
	order := make([]int, len(g.Edges))
	for i := range order {
		order[i] = i
	}
	span := func(e layout.GEdge) (int, int) {
		d := abs(sl.col[e.Tail]-sl.col[e.Head]) + abs(sl.row[e.Tail]-sl.row[e.Head])
		straight := 1
		if sl.row[e.Tail] == sl.row[e.Head] || sl.col[e.Tail] == sl.col[e.Head] {
			straight = 0
		}
		return d, straight
	}
	sort.SliceStable(order, func(a, b int) bool {
		ea, eb := g.Edges[order[a]], g.Edges[order[b]]
		// A self-loop is decoration hung on one box and needs no lane of
		// its own, so it goes last: an edge that has to cross the drawing
		// should not find the wall it wanted spent on a hoop.
		la, lb := ea.Tail == ea.Head, eb.Tail == eb.Head
		if la != lb {
			return !la
		}
		da, sa := span(ea)
		db, sb := span(eb)
		if da != db {
			return da < db
		}
		return sa < sb
	})
	pt := newPorter()
	// A node's self-loops nest, so each has to know how many there are and
	// which one it is: the first gets the widest pair of ports and the
	// tallest hoop, and the rest sit inside it.
	hoops := map[[2]int]int{}
	short := 0
	for _, ei := range order {
		e := g.Edges[ei]
		tb, hb := boxes[e.Tail], boxes[e.Head]
		var tp, hp port
		var ps Route
		if e.Tail == e.Head {
			tp, hp, ps = selfLoop(cv, t, pt, tb, e.Tail, hoops)
		} else {
			// A node inside a frame the other end is not inside cannot
			// have the line come to it: a frame cuts a line in two. The
			// edge stops outside the frame instead, with a corridor of
			// blanks kept clear behind it, which is exactly what a reader
			// walks when it looks for the box an end meant.
			ok := true
			tp, ok = reachOut(cv, t, pt, tb, frames, outside(encl[e.Tail], encl[e.Head]), hb.X+hb.W/2, hb.Y+hb.H/2)
			if ok {
				hp, ok = reachOut(cv, t, pt, hb, frames, outside(encl[e.Head], encl[e.Tail]), tb.X+tb.W/2, tb.Y+tb.H/2)
			}
			if ok {
				ps = scoutEdge(cv, t, tp, hp, budget)
			}
		}
		if ps == nil {
			short++
			continue
		}
		commit(cv, ps, tp, hp, e)
		if e.Label != "" && !placeLabel(cv, ps, e.Label) {
			short++
		}
	}
	return short
}

// selfLoop hoops a line out of one wall and back into the same wall. The
// scout is no use here — its cheapest path from a wall to the wall beside
// it hugs the box and reads as damage — so the shape is drawn outright: out
// at two ports, out one leg each, across, and back.
//
// Where a node carries several, they nest: the first takes the two middle
// ports and hops one cell out, and each after it takes the pair outside
// that one and hops a cell further, so no two ever meet.
func selfLoop(cv *Canvas, t *terrain, pt *porter, b Box, node int, hoops map[[2]int]int) (port, port, Route) {
	for _, side := range []Dir{North, South, East, West} {
		all := slotsIn(b, side)
		j := hoops[[2]int{node, int(side)}]
		lo, hi := len(all)/2-1-j, len(all)/2+j
		if lo < 0 || hi >= len(all) {
			continue
		}
		a, z := all[lo], all[hi]
		if pt.used[a] || pt.used[z] || cv.MaskAt(a.x, a.y) != 0 || cv.MaskAt(z.x, z.y) != 0 {
			continue
		}
		dx, dy := side.Step()
		ps := hoop(a, z, dx, dy, j+1)
		if !clearRun(cv, t, ps) {
			continue
		}
		hoops[[2]int{node, int(side)}] = j + 1
		pt.used[a], pt.used[z] = true, true
		return port{a.x, a.y, side.Opposite()}, port{z.x, z.y, side.Opposite()}, ps
	}
	return port{}, port{}, nil
}

// hoop is the loop's own path: out from one port, across, and back in.
func hoop(a, z ipt, dx, dy, k int) Route {
	var ps Route
	step := func(p ipt) { ps = append(ps, p) }
	for j := 0; j <= k; j++ {
		step(ipt{a.x + dx*j, a.y + dy*j})
	}
	if dx == 0 {
		for x := a.x; ; {
			if x < z.x {
				x++
			} else if x > z.x {
				x--
			} else {
				break
			}
			step(ipt{x, a.y + dy*k})
		}
	} else {
		for y := a.y; ; {
			if y < z.y {
				y++
			} else if y > z.y {
				y--
			} else {
				break
			}
			step(ipt{a.x + dx*k, y})
		}
	}
	for j := k - 1; j >= 0; j-- {
		step(ipt{z.x + dx*j, z.y + dy*j})
	}
	return ps
}

// clearRun says whether a drawn-outright path can be laid: every cell on
// the canvas, empty, and nobody else's.
func clearRun(cv *Canvas, t *terrain, ps Route) bool {
	for _, p := range ps {
		if t.blocked(p.x, p.y) || cv.Held(p.x, p.y) || cv.MaskAt(p.x, p.y) != 0 || cv.Rune(p.x, p.y) != ' ' {
			return false
		}
	}
	return true
}

// slotsIn is every attachable cell outside one wall, in order along it.
func slotsIn(b Box, side Dir) []ipt {
	out := slotsOn(b, side, b.X+b.W/2, b.Y+b.H/2)
	sort.Slice(out, func(i, j int) bool {
		if out[i].x != out[j].x {
			return out[i].x < out[j].x
		}
		return out[i].y < out[j].y
	})
	return out
}

// commit writes one path onto the canvas: the run as arms, the tail joined
// into the wall it leaves, and whichever ends carry a head.
func commit(cv *Canvas, ps Route, tp, hp port, e layout.GEdge) {
	p := Pencil{Style: styleOf(e.Style, e.PenWidth), Pen: ParsePen(e.Pen)}
	for i := 0; i+1 < len(ps); i++ {
		a, b := ps[i], ps[i+1]
		var d Dir
		switch {
		case b.x > a.x:
			d = East
		case b.x < a.x:
			d = West
		case b.y > a.y:
			d = South
		default:
			d = North
		}
		cv.Line(a.x, a.y, d, p)
		cv.Line(b.x, b.y, d.Opposite(), p)
	}
	if e.Dir == layout.Back || e.Dir == layout.Both {
		cv.Head(tp.x, tp.y, tp.d, p.Pen)
	} else {
		attach(cv, tp, p)
	}
	if e.Dir == layout.Forward || e.Dir == layout.Both {
		cv.Head(hp.x, hp.y, hp.d, p.Pen)
	} else {
		attach(cv, hp, p)
	}
}

// attach joins a port to the wall it faces. Without it the line merely
// began beside the box, and a reader walking the line from the other end
// finds it pointing at nothing.
func attach(cv *Canvas, p port, pen Pencil) {
	dx, dy := p.d.Step()
	cv.Line(p.x, p.y, p.d, pen)
	cv.Line(p.x+dx, p.y+dy, p.d.Opposite(), pen)
}

// ---------- labels ----------

// segment is one straight stretch of a route, by the cells it covers.
type segment struct {
	a, b  ipt
	horiz bool
	n     int
}

// segments cuts a route into its straight stretches, ports left out: a
// label may not stand where the line meets a box.
func segments(ps Route) []segment {
	var out []segment
	if len(ps) < 3 {
		return out
	}
	in := ps[1 : len(ps)-1]
	i := 0
	for i < len(in) {
		j := i
		horiz := i+1 < len(in) && in[i+1].y == in[i].y
		for j+1 < len(in) {
			if horiz && in[j+1].y != in[i].y {
				break
			}
			if !horiz && in[j+1].x != in[i].x {
				break
			}
			j++
		}
		out = append(out, segment{in[i], in[j], horiz, j - i + 1})
		i = j + 1
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].n > out[b].n })
	return out
}

// placeLabel puts an edge's words where the reader will give them back to
// that edge and no other. Best is in the line itself, with a shoulder each
// side — a word interrupting a stroke belongs to that stroke, and nothing
// else can claim it. Failing that it stands beside the line, which is
// where graph-easy sets one.
func placeLabel(cv *Canvas, ps Route, label string) bool {
	need := grid.Cells(label)
	if need == 0 {
		return true
	}
	segs := segments(ps)
	for _, s := range segs {
		if s.horiz && inLine(cv, s, label, need) {
			return true
		}
	}
	for _, s := range segs {
		if beside(cv, ps, s, label, need) {
			return true
		}
	}
	return nudge(cv, ps, label, need)
}

// mine says whether a spot's whole neighbourhood belongs to this edge.
//
// A reader gives a piece of writing to the line it touches most, so a
// label set between two lines is a label on whichever of them happens to
// have more cells beside it — which is how the "no" of one edge ends up on
// the edge going the other way. The cells looked at here are the same ones
// the reader looks at, and a spot that touches anybody else's line is not
// a spot.
func mine(cv *Canvas, ps Route, x, y, n int) bool {
	own := make(map[ipt]bool, len(ps))
	for _, p := range ps {
		own[p] = true
	}
	touched := false
	look := func(cx, cy int) bool {
		if cv.MaskAt(cx, cy) == 0 {
			return true
		}
		if !own[ipt{cx, cy}] {
			return false
		}
		touched = true
		return true
	}
	for i := 0; i < n; i++ {
		if !look(x+i, y-1) || !look(x+i, y+1) {
			return false
		}
	}
	for _, cx := range []int{x - 1, x - 2, x + n, x + n + 1} {
		if !look(cx, y) {
			return false
		}
	}
	return touched
}

// inLine writes the label into its own line: the run is blanked for the
// words and a shoulder, and the line carries on either side of them.
func inLine(cv *Canvas, s segment, label string, need int) bool {
	lo, hi := s.a.x, s.b.x
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo+1 < need+4 {
		return false
	}
	y := s.a.y
	mid := (lo + hi) / 2
	x0 := mid - (need+2)/2
	if x0 < lo+1 {
		x0 = lo + 1
	}
	if x0+need+1 > hi-1 {
		x0 = hi - 1 - need - 1
	}
	for x := x0; x < x0+need+2; x++ {
		m := cv.MaskAt(x, y).Arms()
		if cv.Rune(x, y) != ' ' || cv.Held(x, y) || m.Vert() || !m.Horiz() {
			return false
		}
	}
	cv.Blank(x0, y, need+2)
	cv.Text(x0+1, y, label, 0)
	cv.Hold(x0, y, need+2, 1)
	return true
}

// beside sets the label next to its line: above a run across the page, or
// two cells out from one down it, which is where a reader looks for it.
func beside(cv *Canvas, ps Route, s segment, label string, need int) bool {
	put := func(x, y int) bool {
		if !cv.Free(x, y, need) || !mine(cv, ps, x, y, need) {
			return false
		}
		cv.Text(x, y, label, 0)
		cv.Hold(x, y, need, 1)
		return true
	}
	if s.horiz {
		lo, hi := s.a.x, s.b.x
		if lo > hi {
			lo, hi = hi, lo
		}
		x0 := (lo+hi+1)/2 - need/2
		if x0+need > hi+1 && hi-lo+1 >= need {
			x0 = hi + 1 - need
		}
		if x0 < lo && hi-lo+1 >= need {
			x0 = lo
		}
		// Above the run, then under it, then off either end — a reader
		// looks in all four places, and a hoop over a box has room in
		// none of the first two.
		for _, y := range []int{s.a.y - 1, s.a.y + 1} {
			if put(x0, y) {
				return true
			}
		}
		return put(hi+2, s.a.y) || put(lo-1-need, s.a.y)
	}
	lo, hi := s.a.y, s.b.y
	if lo > hi {
		lo, hi = hi, lo
	}
	for _, y := range []int{(lo + hi) / 2, lo, hi} {
		if put(s.a.x+2, y) || put(s.a.x-1-need, y) {
			return true
		}
	}
	return false
}

// nudge is the last try: anywhere touching this edge's own line and
// nothing else's. A label that will not go anywhere clean is left off, and
// the attempt says it was short, so the drawing is made again with more
// room rather than damaged here.
func nudge(cv *Canvas, ps Route, label string, need int) bool {
	for _, p := range ps {
		for _, dy := range []int{-1, 1, -2, 2, 0} {
			for _, dx := range []int{0, 2, -1 - need, 1, -2, -need, 3} {
				x, y := p.x+dx, p.y+dy
				if cv.Free(x, y, need) && mine(cv, ps, x, y, need) {
					cv.Text(x, y, label, 0)
					cv.Hold(x, y, need, 1)
					return true
				}
			}
		}
	}
	return false
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

// outside is the frame an end has to get out of to reach the other end:
// the outermost cluster round one that is not round the other. Nothing
// where the two share every frame they are in.
func outside(a, b []int) int {
	in := map[int]bool{}
	for _, c := range b {
		in[c] = true
	}
	for _, c := range a {
		if !in[c] {
			return c
		}
	}
	return -1
}

// reachOut claims a port, and where the edge has to leave a frame, claims
// it outside that frame with a corridor of blanks behind it.
//
// The corridor is the reader's own rule turned into geometry: an end walks
// straight outward across blanks and frames until it meets a box, and it
// stops at the first thing it finds. So the run from the port to the wall
// is kept empty and the reading is the one the drawing meant.
func reachOut(cv *Canvas, t *terrain, pt *porter, b Box, frames []Box, fr, tx, ty int) (port, bool) {
	if fr < 0 || fr >= len(frames) {
		p, ok := pt.claim(cv, t, b, tx, ty)
		return p, ok
	}
	f := frames[fr]
	for _, side := range sideOrder(b, tx, ty) {
		dx, dy := side.Step()
		var end int
		switch side {
		case East:
			end = f.X + f.W - 1
		case West:
			end = f.X
		case South:
			end = f.Y + f.H - 1
		default:
			end = f.Y
		}
		for _, c := range slotsOn(b, side, tx, ty) {
			if pt.used[c] {
				continue
			}
			var run []ipt
			x, y := c.x, c.y
			blocked := false
			for {
				if t.blocked(x, y) && !t.isRing(x, y) {
					blocked = true
					break
				}
				if !t.isRing(x, y) {
					if cv.Held(x, y) || cv.MaskAt(x, y) != 0 || cv.Rune(x, y) != ' ' {
						blocked = true
						break
					}
					run = append(run, ipt{x, y})
				}
				if (dx != 0 && x == end) || (dy != 0 && y == end) {
					break
				}
				x, y = x+dx, y+dy
				if !cv.In(x, y) {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			px, py := x+dx, y+dy
			if !cv.In(px, py) || t.blocked(px, py) || cv.Held(px, py) ||
				cv.MaskAt(px, py) != 0 || cv.Rune(px, py) != ' ' {
				continue
			}
			for _, p := range run {
				cv.Hold(p.x, p.y, 1, 1)
			}
			pt.used[c] = true
			pt.used[ipt{px, py}] = true
			return port{px, py, side.Opposite()}, true
		}
	}
	return port{}, false
}
