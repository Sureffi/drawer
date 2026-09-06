// chains.go — the placing: the graph's own longest paths, taken one after
// another, longest first.
//
// A chain runs dead straight along the flow — that is the whole of why it
// is worth finding chains at all — and each one takes the lane nearest the
// neighbours it already has. A node's rank is one past the last of its
// placed parents, so an edge points forward unless the graph itself points
// back. What comes out is a grid of slots, not of cells: sheet.go gives
// the slots their sizes and draws on them, and scout.go finds the way
// between them.
//
// A cluster is placed on a grid of its own and stands in its parent's as
// one rectangle, which is the whole of what makes a frame possible: no
// node that is not a member can land inside one.

package cells

import (
	"sort"

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
