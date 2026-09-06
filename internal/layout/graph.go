// graph.go — the graph a source declares, read through the one door.
//
// The other file here asks graphviz where the boxes go. This one asks it
// only what the boxes are: names, labels, record fields, clusters, and
// the edges with the direction and the ornament each carries. A layout
// that does its own placing needs the graph and not a placement, and
// there is exactly one way into graphviz, so it is a function here
// rather than a second door somewhere else.
//
// Everything a drawing can hold is in these types and nothing else is:
// a box drawing has no room for a spline, a font or a fill, so the
// reading stops where the drawing does.

package layout

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// Dir is what an edge's ends carry: a head at the head end, at the tail
// end, at both, or at neither.
type Dir uint8

const (
	Forward Dir = iota
	Back
	Both
	None
)

// GNode is one node as its source declares it.
type GNode struct {
	Name  string
	Label []string // the label's lines; a record's are its fields
	// Record says the label's parts are a record's fields, drawn with a
	// divider between them rather than stacked as lines.
	Record   bool
	Round    bool // ellipse, circle and the rest of the round family
	Style    string
	Pen      string // color
	FontPen  string // fontcolor
	PenWidth float64
	Cluster  int // the innermost cluster holding it, or -1
}

// GEdge is one edge, by the indices of its ends.
type GEdge struct {
	Tail, Head int
	Label      string
	Dir        Dir
	Style      string
	Pen        string
	FontPen    string
	PenWidth   float64
}

// GCluster is one `cluster*` subgraph: its label, the clusters it sits
// in, and every node under it, nested ones included — which is what the
// source says a cluster holds.
type GCluster struct {
	Name    string
	Label   string
	Parent  int
	Members []int
	Style   string
	Pen     string
	FontPen string
}

// Graph is a whole source, as declared.
type Graph struct {
	Name     string
	Nodes    []GNode
	Edges    []GEdge
	Clusters []GCluster
	Directed bool
	// Horiz is true where the ranks march across the page (LR, RL), and
	// Reverse where the flow runs against the reading direction (RL, BT).
	Horiz   bool
	Reverse bool
}

// digraphRe reads the graph's own first word, on whatever line it stands.
// The binding exposes no getter for it, half the graphviz samples open
// with a comment, and a `graph {` drawn with arrowheads is a graph nobody
// wrote.
var digraphRe = regexp.MustCompile(`(?im)^[^\S\n]*(strict[^\S\n]+)?digraph\b`)

var blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
var lineCommentRe = regexp.MustCompile(`(?m)(//|#).*$`)

// Directed reports whether a source's own first word is `digraph`, read
// past the three ways DOT has of saying nothing.
func Directed(src string) bool {
	return digraphRe.MatchString(lineCommentRe.ReplaceAllString(blockCommentRe.ReplaceAllString(src, " "), ""))
}

// Read is the graph a source declares. It runs the same door every other
// layout runs, and asks the parsed graph rather than a rendering, so it
// costs a parse and no layout at all.
func Read(ctx context.Context, src string) (*Graph, error) {
	out := &Graph{Directed: Directed(src)}
	err := Door(ctx, src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		out.Name, _ = graph.Name()
		rd := RankdirOf(src)
		out.Horiz = rd == cgraph.LRRank || rd == cgraph.RLRank
		out.Reverse = rd == cgraph.RLRank || rd == cgraph.BTRank
		index := map[string]int{}
		for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
			name, _ := n.Name()
			index[name] = len(out.Nodes)
			out.Nodes = append(out.Nodes, readNode(n, out.Name))
		}
		// The edge walk is the source's own order: every node, then its
		// out-edges. A multi-edge is two edges here because it is two
		// edges there.
		for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
			for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
				t, _ := e.Tail()
				h, _ := e.Head()
				if t == nil || h == nil {
					continue
				}
				tn, _ := t.Name()
				hn, _ := h.Name()
				ti, tok := index[tn]
				hi, hok := index[hn]
				if !tok || !hok {
					continue
				}
				out.Edges = append(out.Edges, readEdge(e, ti, hi, tn, hn, out.Name, out.Directed))
			}
		}
		readClusters(graph, out, index, -1)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// roundShapes is the family a box drawing answers with rounded corners.
// graphviz's own default shape is `ellipse`, so a source that names no
// shape at all draws round.
var roundShapes = map[string]bool{
	"": true, "ellipse": true, "oval": true, "circle": true, "doublecircle": true,
	"egg": true, "point": true, "Mcircle": true, "Mdiamond": true, "diamond": true,
	"Msquare": false,
}

func readNode(n *cgraph.Node, graphName string) GNode {
	name, _ := n.Name()
	shape := strings.TrimSpace(n.GetStr("shape"))
	style := strings.TrimSpace(n.GetStr("style"))
	out := GNode{
		Name: name,
		// The shape says which family the box is in, and `style=rounded`
		// says the same thing about a box that named none — which is how a
		// model actually writes a diagram: `node [shape=box,
		// style=rounded]` in six of the corpus's own model fixtures, every
		// one of them drawn square while the source said round.
		Round:    roundShapes[shape] || (strings.Contains(style, "rounded") && !strings.Contains(shape, "record")),
		Style:    style,
		Pen:      strings.TrimSpace(n.GetStr("color")),
		FontPen:  strings.TrimSpace(n.GetStr("fontcolor")),
		PenWidth: atof(n.GetStr("penwidth")),
		Cluster:  -1,
	}
	label := n.Label()
	if label == "" || label == `\N` {
		label = name
	}
	label = strings.NewReplacer(`\N`, name, `\G`, graphName).Replace(label)
	if s, ok := htmlWords(label); ok {
		out.Label = []string{s}
		return out
	}
	if strings.Contains(shape, "record") {
		out.Record = true
		out.Label = recordFields(label)
		return out
	}
	out.Label = labelLines(label)
	return out
}

func readEdge(e *cgraph.Edge, tail, head int, tn, hn, graphName string, directed bool) GEdge {
	out := GEdge{
		Tail: tail, Head: head, Dir: None,
		Style:    strings.TrimSpace(e.GetStr("style")),
		Pen:      strings.TrimSpace(e.GetStr("color")),
		FontPen:  strings.TrimSpace(e.GetStr("fontcolor")),
		PenWidth: atof(e.GetStr("penwidth")),
	}
	if directed {
		out.Dir = Forward
	}
	switch strings.TrimSpace(e.GetStr("dir")) {
	case "forward":
		out.Dir = Forward
	case "back":
		out.Dir = Back
	case "both":
		out.Dir = Both
	case "none":
		out.Dir = None
	}
	// `arrowhead=none` takes the head off the head end and nothing else,
	// which on a plain forward edge leaves a line with no head at all.
	if strings.TrimSpace(e.GetStr("arrowhead")) == "none" && out.Dir == Forward {
		out.Dir = None
	}
	lbl := e.Label()
	lbl = strings.NewReplacer(`\T`, tn, `\H`, hn, `\E`, tn+"->"+hn, `\G`, graphName).Replace(lbl)
	if s, ok := htmlWords(lbl); ok {
		out.Label = strings.Join(strings.Fields(s), " ")
		return out
	}
	out.Label = flatLabel(lbl)
	return out
}

// flatLabel is a label that has to come out on one row — an edge's words, a
// cluster's name. It reads by the same law as a node's: a break is a space
// here because there is nowhere to break to, and every other escape prints
// as the character it escapes. Folding only the breaks left `Group \#0` on
// the page with the backslash still in it.
func flatLabel(label string) string {
	return strings.Join(strings.Fields(strings.Join(labelLines(label), " ")), " ")
}

func readClusters(g *cgraph.Graph, out *Graph, index map[string]int, parent int) {
	for s, _ := g.FirstSubGraph(); s != nil; s, _ = s.NextSubGraph() {
		name, _ := s.Name()
		me := parent
		if strings.HasPrefix(name, "cluster") {
			c := GCluster{
				Name: name, Label: s.Label(), Parent: parent,
				Style:   strings.TrimSpace(s.GetStr("style")),
				Pen:     strings.TrimSpace(s.GetStr("color")),
				FontPen: strings.TrimSpace(s.GetStr("fontcolor")),
			}
			if w, ok := htmlWords(c.Label); ok {
				c.Label = strings.Join(strings.Fields(w), " ")
			} else {
				c.Label = flatLabel(c.Label)
			}
			for n, _ := s.FirstNode(); n != nil; n, _ = s.NextNode(n) {
				nm, _ := n.Name()
				if i, ok := index[nm]; ok {
					c.Members = append(c.Members, i)
				}
			}
			if len(c.Members) > 0 {
				me = len(out.Clusters)
				out.Clusters = append(out.Clusters, c)
				// A node's cluster is the innermost one holding it, and
				// the innermost is the one found last: the walk goes down.
				for _, i := range c.Members {
					out.Nodes[i].Cluster = me
				}
			}
		}
		readClusters(s, out, index, me)
	}
}

// tagRe is the markup of an HTML label. A drawing has no table in it — it
// has the words the table held — so the label is those words.
var tagRe = regexp.MustCompile(`(?s)<[^<>]*>`)

var entities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&nbsp;", " ")

func htmlWords(label string) (string, bool) {
	t := strings.TrimSpace(label)
	if !strings.HasPrefix(t, "<") || !strings.HasSuffix(t, ">") || !strings.Contains(t, "</") {
		return "", false
	}
	return strings.Join(strings.Fields(entities.Replace(tagRe.ReplaceAllString(t, " "))), " "), true
}

// portRe is a record field's port name. `<f0> text` is drawn as `text`:
// the port is a place to attach to, never anything to read.
var portRe = regexp.MustCompile(`<[^<>]*>`)

// recordFields cuts a record's label into the fields a divider stands
// between. graphviz nests them with braces to turn the fields across the
// rank instead of along it; a box drawing has one strip either way, and
// the words come out in the order the source wrote them, which is all a
// reader can get back.
//
// A field keeps its own line breaks, as `\n` inside the string: the box law
// is that a multi-line label grows its box, and a record is a label. Folding
// them to spaces drew `+ speak() + fetch()` on one row where the source
// stacked two methods, and the run-on width then flipped a whole schema
// sideways to make it fit.
func recordFields(label string) []string {
	label = portRe.ReplaceAllString(label, " ")
	var out []string
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		switch c := label[i]; c {
		case '\\':
			if i+1 < len(label) {
				i++
				switch label[i] {
				case 'n', 'l', 'r':
					b.WriteByte('\n')
				default:
					b.WriteByte(label[i])
				}
			}
		case '|':
			out = append(out, recordField(b.String()))
			b.Reset()
		case '{', '}':
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	out = append(out, recordField(b.String()))
	return out
}

// recordField tidies one field: its own lines squeezed, and the empty ones
// its breaks left at either end taken off. A field of nothing but a break
// is still a field — the divider is there in the source — so one empty line
// is kept where that is all there was.
func recordField(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	for len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 1 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// labelLines cuts a label on the breaks graphviz spells with a backslash.
// Everything else a backslash escapes is the character itself, which is
// what the drawing prints.
func labelLines(label string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		if label[i] == '\\' && i+1 < len(label) {
			i++
			switch label[i] {
			case 'n', 'l', 'r':
				out = append(out, b.String())
				b.Reset()
			default:
				b.WriteByte(label[i])
			}
			continue
		}
		b.WriteByte(label[i])
	}
	out = append(out, b.String())
	// A label ending in a break has a trailing empty line nobody wrote.
	for len(out) > 1 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	for i := range out {
		out[i] = strings.TrimRight(out[i], " \t")
	}
	if len(out) == 1 {
		out[0] = strings.TrimSpace(out[0])
	}
	return out
}

func atof(s string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return v }
