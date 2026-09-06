// drawing.go — graphviz's own drawing, as data.
//
// graphviz's json output is xdot: every operation it would have painted,
// listed per object, in points with the origin bottom-left. It is the same
// answer its SVG writer works from, and it arrives already resolved —
// colours as hex, shapes as points, type as an anchor and a measured
// width — so a renderer reading it is drawing graphviz's picture rather
// than a picture of its own.
//
// Both rungs read it: the cells rung quantises it onto the grid, the
// pixels rung paints it. So the types live here, beside the door that
// makes them, and neither rung has to reach across for the other's.
//
// Measured on go-graphviz v0.2.10, on this box:
//
//   - Colours come back as `#rrggbb` or `#rrggbbaa`, names resolved: `pink`
//     is `#ffc0cb`, `transparent` is `#fffffe00`. Alpha 00 is a pen that
//     strokes nothing and a fill that fills nothing.
//   - Every colour carries `grad`: "none", or "linear"/"radial" with `p0`,
//     `p1` and `stops`, and radial also `r0`, `r1`.
//   - An uppercase shape op is filled AND its outline stroked — `P` with
//     the fill in force and the pen in force, `p` with the pen alone. That
//     is xdot's own rule.
//   - `S` arrives as `solid`, `dashed`, `dotted`, `tapered` or
//     `setlinewidth(N)`; `bold` is a line width of 2. An `invis` object is
//     simply not in the json at all.
//   - A cluster carries a `nodes` array and a node does not, which is how
//     the two are told apart.
//   - HTML labels arrive span by span: a `t` op with the font bits, then
//     the `T` op it applies to. The space between two spans is its own
//     span and is kept.

package layout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// Op is one drawing operation. The op code says which fields carry
// anything: `c` and `C` the pen and fill colour, `S` a style, `F` the
// font, `t` the font bits of the text that follows, `p`/`P` a polygon,
// `L` a polyline, `b`/`B` a bezier run of 3n+1 points, `e`/`E` an ellipse
// as [cx, cy, rx, ry], and `T` a text run anchored at `pt` on its
// baseline.
type Op struct {
	Op     string       `json:"op"`
	Points [][2]float64 `json:"points"`
	Rect   []float64    `json:"rect"`
	Pt     []float64    `json:"pt"`
	Text   string       `json:"text"`
	Align  string       `json:"align"`
	Color  string       `json:"color"`
	// Width is a text run's width as graphviz measured it, which is the
	// space it reserved for the run and therefore where the run belongs.
	Width float64 `json:"width"`
	Style string  `json:"style"`
	Size  float64 `json:"size"`
	Face  string  `json:"face"`
	// FontChar is a bit set: 1 bold, 2 italic, 4 underline, 8 superscript,
	// 16 subscript, 32 strikethrough.
	FontChar int `json:"fontchar"`
	// The gradient a colour op carries, where it carries one.
	Grad  string    `json:"grad"`
	P0    []float64 `json:"p0"`
	P1    []float64 `json:"p1"`
	R0    float64   `json:"r0"`
	R1    float64   `json:"r1"`
	Stops []Stop    `json:"stops"`
}

// Stop is one stop of a gradient: how far along, and what colour there.
type Stop struct {
	Frac  float64 `json:"frac"`
	Color string  `json:"color"`
}

// Object is a node or a cluster: what graphviz drew for its shape, what it
// drew for its label, and — on a cluster alone — which nodes are inside.
type Object struct {
	Name  string `json:"name"`
	Draw  []Op   `json:"_draw_"`
	LDraw []Op   `json:"_ldraw_"`
	Nodes []int  `json:"nodes"` // present on a cluster, absent on a node
}

// Edge is an edge as graphviz drew it: the line, the ornament at each end,
// and the label. Tail and Head index Objects.
type Edge struct {
	Tail  int  `json:"tail"`
	Head  int  `json:"head"`
	Draw  []Op `json:"_draw_"`
	HDraw []Op `json:"_hdraw_"`
	TDraw []Op `json:"_tdraw_"`
	LDraw []Op `json:"_ldraw_"`
}

// Drawing is one laid-out graph, as graphviz would have painted it: how
// big it is, what stands in it, and the ground under it. Draw is the
// background — see BGColor before painting it — and LDraw the graph's own
// label.
type Drawing struct {
	BB      string   `json:"bb"`
	Objects []Object `json:"objects"`
	Edges   []Edge   `json:"edges"`
	Draw    []Op     `json:"_draw_"`
	LDraw   []Op     `json:"_ldraw_"`
	// BGColor is the graph's bgcolor attribute, absent where the source
	// never set one. The background op list is written either way and says
	// white when nobody asked for a background at all, so this is the only
	// field that answers whether there is a ground to paint.
	BGColor string `json:"bgcolor"`
	// Pad is the air graphviz puts round the drawing, in inches, as the
	// source declared it — empty where it declared none.
	Pad string `json:"pad"`
	// W and H are the drawing's own size in points, read off BB.
	W, H float64
}

// Render lays a parsed graph out and reads back what graphviz would have
// drawn. The size comes off `bb`, which is "x0,y0,x1,y1" with the near
// corner at the origin, so the far corner is the size.
func Render(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) (*Drawing, error) {
	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.Format("json"), &buf); err != nil {
		return nil, err
	}
	var d Drawing
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		return nil, err
	}
	if f := strings.Split(d.BB, ","); len(f) == 4 {
		d.W, d.H = Atof(f[2]), Atof(f[3])
	}
	if d.W <= 0 || d.H <= 0 {
		return nil, errors.New("layout has no bounding box")
	}
	return &d, nil
}

// Object is the node or cluster of that name, or nil.
func (d *Drawing) Object(name string) *Object {
	for i := range d.Objects {
		if d.Objects[i].Name == name {
			return &d.Objects[i]
		}
	}
	return nil
}

// Between is the edge from one named node to another, or nil. The json
// names an edge's ends by index, so this is the reading that turns two
// names into the thing drawn between them.
func (d *Drawing) Between(tail, head string) *Edge {
	name := func(i int) string {
		if i < 0 || i >= len(d.Objects) {
			return ""
		}
		return d.Objects[i].Name
	}
	for i := range d.Edges {
		if name(d.Edges[i].Tail) == tail && name(d.Edges[i].Head) == head {
			return &d.Edges[i]
		}
	}
	return nil
}

// TextY is the baseline of the first text a drawing list sets, y growing up
// the page. Zero where the list sets no type.
func TextY(list []Op) float64 {
	for _, op := range list {
		if op.Op == "T" && len(op.Pt) >= 2 {
			return op.Pt[1]
		}
	}
	return 0
}
