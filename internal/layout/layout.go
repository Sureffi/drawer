// layout.go — graphviz does the placing.
//
// The one door to graphviz, the scale from its inches to the terminal's
// cells, and the `plain` layout the cells rung draws from: node centres and
// sizes, edge splines as point lists. Placing boxes so edges do not cross
// is the part that is genuinely hard and is already solved; everything
// after that answer is derived in cell space.

package layout

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/grid"
)

// Door is the one way through to graphviz: it opens a context, parses the
// source and hands the graph to fn, closing everything after it. A source
// with no graph in it parses to (nil, nil): graphviz reports nothing wrong
// because nothing was asked of it. Every caller dereferences the result, so
// the nil dies here — an empty ```dot fence is one the model opened and
// closed, not a diagram. graphviz.New registers into package-level maps and
// two at once are a fatal error; nothing here runs two, the hook being one
// process per delta. A layout measures about a millisecond.
func Door(src string, fn func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error) error {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return err
	}
	defer g.Close()
	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil {
		return err
	}
	if graph == nil {
		return errors.New("no graph in source")
	}
	defer graph.Close()
	return fn(ctx, g, graph)
}

// Orientations is the order a graph is tried in: as written, then top-down
// where it was not already, because rows scroll and columns run out.
func Orientations(src string) []cgraph.RankDir {
	if RankdirOf(src) == cgraph.TBRank {
		return []cgraph.RankDir{""}
	}
	return []cgraph.RankDir{"", cgraph.TBRank}
}

// LabelOf is a node's label as graphviz would print it: what it declares,
// else its name, which is what `\N` means.
func LabelOf(n *cgraph.Node) string {
	if l := n.Label(); l != "" && l != `\N` {
		return l
	}
	name, _ := n.Name()
	return name
}

type Node struct {
	Name  string
	X, Y  float64
	Label string
}

type Edge struct {
	Tail, Head string
	Pts        [][2]float64
	Label      string
	LX         float64
	LY         float64
}

type Plain struct {
	W, H  float64
	Nodes []Node
	Edges []Edge
	// horiz is the axis the ranks run along, read from the rankdir the
	// layout was made with. It used to be re-derived from where the nodes
	// landed, and a top-down tree wider than it was tall read as
	// left-right — then `straighten` pulled children onto their parent's
	// row and the hierarchy collapsed. The answer was upstream all along.
	Horiz bool
	// directed says whether the edges carry heads. A `graph { a -- b }`
	// used to draw arrows nobody wrote.
	Directed bool
}

// Cells per inch. graphviz thinks in inches sized for 14pt type; a
// terminal thinks in cells that are roughly twice as tall as they are
// wide. Rather than scaling its output down — which turns a five-letter
// label into a twenty-cell box — we hand it node sizes already expressed
// in these units, let it do the spacing, and scale back by exactly the
// same factor. The boxes then come out the size we asked for.
const (
	CellsPerInchX = 10.0
	RowsPerInchY  = 6.0
	NodeRows      = 3 // border, label, border
)

// DOT runs graphviz and parses its `plain` output. Coordinates are
// in inches with the origin bottom-left; the rasteriser flips them.
//
// force overrides the orientation the source asked for; empty leaves the
// author's choice alone.
func DOT(src string, force cgraph.RankDir) (*Plain, error) {
	var l *Plain
	err := Door(src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		rd := force
		if rd == "" {
			rd = RankdirOf(src)
		} else {
			graph.SetRankDir(rd)
		}
		sizeNodesInCells(graph)
		SetSeparation(graph, rd)
		var buf bytes.Buffer
		if err := g.Render(ctx, graph, "plain", &buf); err != nil {
			return err
		}
		l = parsePlain(buf.String())
		l.Horiz = rd == cgraph.LRRank || rd == cgraph.RLRank
		l.Directed = directedRe.MatchString(src)
		return nil
	})
	return l, err
}

// sizeNodesInCells fixes every node's footprint to the space its label
// actually needs, in cell units. This is the whole trick: graphviz keeps
// doing the hard part (placing boxes so edges behave) but stops guessing
// at typography we do not have.
//
// This rung draws one line, so a label's line breaks — `\n`, and the
// justified `\l` and `\r` — become spaces before graphviz sees it.
var oneLine = strings.NewReplacer(`\n`, " ", `\l`, " ", `\r`, " ")

func sizeNodesInCells(g *cgraph.Graph) {
	for n, _ := g.FirstNode(); n != nil; n, _ = g.NextNode(n) {
		label := oneLine.Replace(LabelOf(n))
		n.SetLabel(label)
		n.SetShape(cgraph.BoxShape)
		n.SetFixedSize(true)
		n.SetWidth(float64(grid.Cells(label)+2) / CellsPerInchX)
		n.SetHeight(NodeRows / RowsPerInchY)
	}
}

// RankdirOf reports the orientation a source asks for. graphviz exposes
// no getter for it, and the axis is what decides whether a gap measured
// in cells divides by columns-per-inch or rows-per-inch. Absent means TB,
// which is graphviz's own default.
var rankdirRe = regexp.MustCompile(`(?i)rankdir\s*=\s*"?(TB|LR|BT|RL)"?`)

// directedRe reads the graph's own first word. The binding exposes no
// getter for it, and a `graph {` drawn with arrowheads is a graph nobody
// wrote.
var directedRe = regexp.MustCompile(`(?i)^\s*(strict\s+)?digraph\b`)

func RankdirOf(src string) cgraph.RankDir {
	if m := rankdirRe.FindStringSubmatch(src); m != nil {
		return cgraph.RankDir(strings.ToUpper(m[1]))
	}
	return cgraph.TBRank
}

// SetSeparation picks the gaps in cells and hands them over in the inches
// graphviz wants. Node sizes were already expressed this way; leaving the
// gaps at graphviz's defaults meant spacing chosen for 14pt type on paper
// — three blank rows between ranks, which is a lot of screen for a gap.
//
// Which axis a gap lives on depends on the orientation: ranks separate
// along the flow, siblings separate across it. A row costs about twice
// what a column costs to a reader, so the two axes do not want the same
// number.
func SetSeparation(g *cgraph.Graph, rd cgraph.RankDir) {
	// top-down: ranks stack in rows, siblings spread in columns
	rank, rankPerInch := 2.0, RowsPerInchY
	node, nodePerInch := 4.0, CellsPerInchX
	if rd == cgraph.LRRank || rd == cgraph.RLRank {
		// left-right: ranks march in columns, siblings stack in rows
		rank, rankPerInch = 5.0, CellsPerInchX // room for ───▶
		node, nodePerInch = 1.0, RowsPerInchY
	}
	g.SetRankSeparator(rank / rankPerInch)
	g.SetNodeSeparator(node / nodePerInch)
}

func Atof(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

// parsePlain reads graphviz's plain format. Fields are space separated
// with quoted labels; the label is the only field that can contain a
// space, so a small hand parser beats a regexp here.
func parsePlain(s string) *Plain {
	l := &Plain{}
	for _, line := range strings.Split(s, "\n") {
		f := splitPlain(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "graph":
			if len(f) >= 4 {
				l.W, l.H = Atof(f[2]), Atof(f[3])
			}
		case "node":
			if len(f) >= 7 {
				// f[4] and f[5] are graphviz's node width and height in
				// inches. A node box here is sized from its label in cells,
				// not from what graphviz thought it would be, so they are
				// read past rather than stored.
				l.Nodes = append(l.Nodes, Node{
					Name: f[1], X: Atof(f[2]), Y: Atof(f[3]), Label: f[6],
				})
			}
		case "edge":
			if len(f) < 4 {
				continue
			}
			n, _ := strconv.Atoi(f[3])
			e := Edge{Tail: f[1], Head: f[2]}
			i := 4
			for k := 0; k < n && i+1 < len(f); k++ {
				e.Pts = append(e.Pts, [2]float64{Atof(f[i]), Atof(f[i+1])})
				i += 2
			}
			// What trails the points is `style color`, or `label lx ly
			// style color`. The field count says which, exactly. Asking
			// instead whether the text looks like a number dropped every
			// numeric label on the floor — a weight of `42` simply was not
			// drawn — and graphviz had already answered the question by
			// how many fields it wrote.
			if len(f)-i >= 5 {
				e.Label, e.LX, e.LY = f[i], Atof(f[i+1]), Atof(f[i+2])
			}
			l.Edges = append(l.Edges, e)
		}
	}
	return l
}

func splitPlain(line string) []string {
	var out []string
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '"' {
			i++
			var b strings.Builder
			for i < len(line) && line[i] != '"' {
				// Only `\"` and `\\` are this format's escapes. Eating the
				// backslash of anything else turned a label of `a\nb` into
				// `anb` — a word nobody wrote, drawn with full confidence.
				// Left alone it reads as `a\nb`: the tool plainly did not
				// handle it, which is the accepted failure.
				if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\') {
					i++
				}
				b.WriteByte(line[i])
				i++
			}
			i++
			out = append(out, b.String())
			continue
		}
		start := i
		for i < len(line) && line[i] != ' ' {
			i++
		}
		out = append(out, line[start:i])
	}
	return out
}

// Footprint is the cell box a laid-out graph occupies. Reserving and
// drawing both ask it, of the same layout, so the two can never disagree.
func Footprint(l *Plain) (w, h int) {
	if l == nil || len(l.Nodes) == 0 || l.W <= 0 || l.H <= 0 {
		return 0, 0
	}
	return int(l.W*CellsPerInchX) + 2, int(l.H*RowsPerInchY) + 1
}

// Fit chooses how to draw a graph in the space that actually exists. A
// graph too wide to fit is redrawn top-down before it is given up on:
// vertical costs rows, and rows scroll, where horizontal costs columns,
// and columns simply run out. Only when neither orientation fits does the
// source show, which is still the whole failure policy.
//
// maxRows bounds the answer; 0 is no ceiling.
//
// It returns the layout it settled on. Measuring meant laying the graph
// out, and the drawing that follows needs exactly that layout; running
// graphviz a second time to rediscover what this call already knows would
// be the plainest waste in the file.
func Fit(src string, width, maxRows int) (*Plain, int, bool) {
	for _, rd := range Orientations(src) {
		l, err := DOT(src, rd)
		if err != nil {
			continue
		}
		w, h := Footprint(l)
		if w <= 0 || h <= 0 || w > width {
			continue
		}
		if maxRows > 0 && h > maxRows {
			continue
		}
		return l, h, true
	}
	return nil, 0, false
}

// Complete says whether a source is a whole graph: graphviz reads it, and
// there is a graph in it.
func Complete(src string) bool {
	l, err := DOT(src, "")
	return err == nil && l != nil
}
