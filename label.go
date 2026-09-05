// label.go — an edge label sits on its line, and the line stops for it.
//
// graphviz places an edge label beside the midpoint of its edge, and the
// picture reads as a caption near a line. The glyph rungs put the label on
// the line and cut the stroke around it; so does this rung, by rewriting the
// graph before layout: a labelled edge becomes two edges through a plain
// node that carries the label. dot does the same inside itself for a
// labelled edge — the label is a virtual node on the edge's rank chain — so
// the layout barely moves; the difference is that the node is ours, stays on
// the line, and stops the line a glyph short of the text on either side.
//
// What follows the edge to its halves: every attribute the model wrote,
// copied to both, except that the label goes to the node, the tail's
// ornaments stay on the half that touches the tail and the head's on the
// other, and the arrow direction is split so a `dir=both` edge keeps both
// heads. Head and tail labels are not touched: they belong to an end, not
// to the line.
//
// An edge dot will reverse to rank the graph is chained the other way
// round, head to label to tail, with the arrows turned to match. A forward
// chain through such an edge would put the label a rank past the far end,
// with the line making a detour to visit it — measured as "no" hanging
// under a decision it pointed up from. Chained backwards it is a forward
// path beside the one it answers, and the label sits between its ends.
// Which edges those are is dot's own question, answered dot's own way: a
// depth-first walk in input order, and an edge into a node still on the
// stack is a back edge. "Closes a cycle" is not the rule — in a two-cycle
// that is both edges, and flipping both scrambles the picture.

package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/theme"
)

// labelNodePrefix names the nodes this file makes. A name the model would
// not write, so the two never meet.
const labelNodePrefix = "__drawer_label_"

var digraphRe = regexp.MustCompile(`(?i)\bdigraph\b`)

// isDigraph reads the source's own keyword: the one place the answer is
// written, and cgraph does not hand it back through this binding.
func isDigraph(src string) bool {
	head := src
	if i := strings.IndexByte(src, '{'); i >= 0 {
		head = src[:i]
	}
	return digraphRe.MatchString(head)
}

// inlineEdgeLabels puts every edge label on its edge. fontPt is the type
// size the labels will be set at, which fixes the air around them.
func inlineEdgeLabels(graph *cgraph.Graph, th *theme.Theme, fontPt float64, directed bool) {
	// Collect first: rewriting the out-lists while walking them is undefined.
	var todo, plain []*cgraph.Edge
	for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
		for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
			if t, h := ends(e); e.GetStr("label") != "" && t != h {
				todo = append(todo, e)
			} else {
				plain = append(plain, e)
			}
		}
	}
	if len(todo) == 0 {
		return
	}
	// dot's own spacing for a labelled graph, made here because our label
	// nodes are real nodes on real ranks: it halves whatever ranksep is in
	// force and doubles every edge's minlen, so a labelled edge spans two
	// ranks with its label on the middle one and a plain edge still spans
	// a full rank gap. Without the halving a labelled chain stands a third
	// taller than dot would draw it — measured, 18 rows against 24.5 on
	// four nodes; without the doubling a plain edge between neighbours is a
	// tick, half the line dot gives it. The model's ranksep is the one
	// halved, else the theme's, else dot's half inch; a labelled edge's
	// halves keep the model's minlen, which is the label at its midpoint.
	ranksep := graph.GetStr("ranksep")
	if ranksep == "" {
		ranksep = th.Graph["ranksep"]
	}
	if ranksep == "" {
		ranksep = "0.5"
	}
	graph.SafeSet("ranksep", halved(ranksep), "")
	for _, e := range plain {
		minlen := 1
		if v, err := strconv.Atoi(e.GetStr("minlen")); err == nil {
			minlen = v
		}
		e.SafeSet("minlen", strconv.Itoa(2*minlen), "")
	}
	back := backEdges(graph)
	// Every edge attribute the source declared, by name, so the copy is the
	// model's whole edge and not a list somebody thought of.
	var attrs []string
	for sym, _ := graph.NextAttr(theme.KindEdge, nil); sym != nil; sym, _ = graph.NextAttr(theme.KindEdge, sym) {
		attrs = append(attrs, sym.Name())
	}
	if fontPt <= 0 {
		fontPt = 14
	}
	// One glyph of air on either side, in inches; almost none above and
	// below, so the node is the text's height and the line meets it level.
	margin := strconv.FormatFloat(fontPt*pxAdvance/72, 'f', 3, 64) + ",0.02"

	for i, e := range todo {
		tail, _ := e.Tail()
		head, _ := e.Head()
		if tail == nil || head == nil {
			continue
		}
		home := homeOf(graph, tail, head)
		ln, err := home.CreateNodeByName(labelNodePrefix + strconv.Itoa(i))
		if err != nil || ln == nil {
			continue
		}
		ln.SafeSet("label", e.GetStr("label"), "")
		ln.SafeSet("shape", "plaintext", "")
		ln.SafeSet("width", "0", "")
		ln.SafeSet("height", "0", "")
		ln.SafeSet("margin", margin, "")
		// Filled with nothing: the theme's fill rule reads a filled node as
		// the model's and leaves it alone, which is what a label wants.
		ln.SafeSet("style", "filled", "")
		ln.SafeSet("fillcolor", "transparent", "")
		fc := e.GetStr("fontcolor")
		if fc == "" {
			fc = th.Edge["fontcolor"]
		}
		if fc != "" {
			ln.SafeSet("fontcolor", fc, "")
		}
		if fs := e.GetStr("fontsize"); fs != "" {
			ln.SafeSet("fontsize", fs, "")
		}
		// tailHalf touches the edge's tail, headHalf its head. Chained
		// forward the tail is tailHalf's tail; chained back it is its head,
		// and every end-attribute is renamed to the end it now sits at.
		flipped := back[edgeKey(e)]
		var tailHalf, headHalf *cgraph.Edge
		if flipped {
			headHalf, _ = home.CreateEdgeByName("", head, ln)
			tailHalf, _ = home.CreateEdgeByName("", ln, tail)
		} else {
			tailHalf, _ = home.CreateEdgeByName("", tail, ln)
			headHalf, _ = home.CreateEdgeByName("", ln, head)
		}
		if tailHalf == nil || headHalf == nil {
			continue
		}
		for _, a := range attrs {
			v := e.GetStr(a)
			if v == "" || a == "label" || a == "dir" {
				continue
			}
			switch {
			case strings.Contains(a, "tail"):
				tailHalf.SafeSet(otherEnd(a, flipped), v, "")
			case strings.Contains(a, "head"):
				headHalf.SafeSet(otherEnd(a, flipped), v, "")
			default:
				tailHalf.SafeSet(a, v, "")
				headHalf.SafeSet(a, v, "")
			}
		}
		if directed {
			dir := e.GetStr("dir")
			if dir == "" {
				dir = "forward"
			}
			atHead, atTail := "none", "none"
			if dir == "forward" || dir == "both" {
				atHead = "forward"
				if flipped {
					atHead = "back"
				}
			}
			if dir == "back" || dir == "both" {
				atTail = "back"
				if flipped {
					atTail = "forward"
				}
			}
			headHalf.SafeSet("dir", atHead, "")
			tailHalf.SafeSet("dir", atTail, "")
		}
		graph.DeleteEdge(e)
	}
}

// halved is a ranksep at half its length, the "equally" it may carry kept:
// "1" is "0.5", "0.5 equally" is "0.25 equally". Anything that does not
// start with a number is returned as it came.
func halved(ranksep string) string {
	num, rest, _ := strings.Cut(ranksep, " ")
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return ranksep
	}
	out := strconv.FormatFloat(v/2, 'f', -1, 64)
	if rest != "" {
		out += " " + rest
	}
	return out
}

// ends names an edge's tail and head.
func ends(e *cgraph.Edge) (string, string) {
	t, _ := e.Tail()
	h, _ := e.Head()
	tn, _ := t.Name()
	hn, _ := h.Name()
	return tn, hn
}

// otherEnd renames an end-attribute to the opposite end when the half it
// rides on runs the other way: taillabel becomes headlabel, ltail lhead.
func otherEnd(a string, flipped bool) string {
	if !flipped {
		return a
	}
	if strings.Contains(a, "tail") {
		return strings.Replace(a, "tail", "head", 1)
	}
	return strings.Replace(a, "head", "tail", 1)
}

// backEdges is dot's answer to which edges it will reverse to rank the
// graph, found dot's way: a depth-first walk over the nodes in input order,
// out-edges in input order, and an edge into a node still on the stack is
// a back edge. Keyed by edgeKey, since the binding hands out a fresh
// wrapper for every visit.
func backEdges(graph *cgraph.Graph) map[string]bool {
	back := map[string]bool{}
	const onStack, done = 1, 2
	seen := map[string]int{}
	var visit func(n *cgraph.Node)
	visit = func(n *cgraph.Node) {
		name, _ := n.Name()
		seen[name] = onStack
		for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
			h, _ := e.Head()
			hn, _ := h.Name()
			switch seen[hn] {
			case onStack:
				back[edgeKey(e)] = true
			case 0:
				visit(h)
			}
		}
		seen[name] = done
	}
	for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
		if name, _ := n.Name(); seen[name] == 0 {
			visit(n)
		}
	}
	return back
}

// edgeKey names an edge by its ends and its key, across wrappers.
func edgeKey(e *cgraph.Edge) string {
	t, h := ends(e)
	name, _ := e.Name()
	return t + "\x00" + h + "\x00" + name
}

// homeOf is the innermost subgraph that holds both ends, else the graph
// itself: a labelled edge inside a cluster keeps its label in the cluster.
func homeOf(g *cgraph.Graph, a, b *cgraph.Node) *cgraph.Graph {
	for sg, _ := g.FirstSubGraph(); sg != nil; sg, _ = sg.NextSubGraph() {
		if na, _ := sg.SubNode(a); na != nil {
			if nb, _ := sg.SubNode(b); nb != nil {
				return homeOf(sg, a, b)
			}
		}
	}
	return g
}
