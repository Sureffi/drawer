// paint.go — graphviz's own drawing, in pixels, painted here.
//
// The wasm hands back xdot: every polygon, bezier, ellipse and text anchor
// graphviz would have painted, in points with the origin bottom-left and
// the colours already resolved to hex. This file walks that list with
// fogleman/gg — a rasteriser in Go, in this process — and the picture is
// graphviz's picture rather than a picture of it. Nothing on the box is
// asked for, and no other process is started.
//
// The rules that are not obvious, all of them measured on go-graphviz
// v0.2.10:
//
//   - An uppercase op is filled AND stroked: the fill in force paints the
//     inside, the pen in force paints the outline. That is xdot's rule,
//     and it is the one go-graphviz's own PNG renderer gets wrong.
//   - A colour with alpha 00 is how graphviz says "no pen" and "no fill".
//   - One drawing list is one object, and it names every colour and style
//     it means, so the pen resets between lists rather than inheriting the
//     neighbour's.
//   - The zoom is arithmetic on the coordinates, not a matrix on the
//     context: gg positions type through the matrix but does not scale the
//     glyphs by it, so a scaled matrix draws a big picture in small type.
//     Points are multiplied on the way in and faces are asked for in
//     pixels.

package pixel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/flopp/go-findfont"
	"github.com/fogleman/gg"
	"github.com/sureffi/drawer/internal/layout"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

const (
	// A tiny graph in a tall region would otherwise be blown up without
	// limit; past this the picture is not better, only heavier.
	maxZoom = 8.0
	// maxPixels is the ceiling on the image itself. The rows are already
	// bounded by grid.MaxRows and the columns by the 297 marks a
	// placeholder cell can be numbered with, so the one term nobody
	// bounded is the cell, which -cell takes on trust and a terminal
	// answers for itself. The largest picture those bounds allow on a 4K
	// screen is 297 columns of a 13px cell by 120 rows of a 30px one —
	// 3861 x 3600, fourteen million pixels — so this is twice the biggest
	// real one, and still only 128MB of RGBA. Past it there is no picture
	// and the glyphs draw.
	maxPixels = 32 << 20
	// maxFontBytes is the ceiling on a font file a theme names, which the
	// painter reads whole. Measured, the largest font on this box is
	// NotoSerifCJK-Bold.ttc at 27MB, so this is twice the biggest real one.
	// Past it there is no face and the picture is set in Go Mono.
	maxFontBytes = 64 << 20
	// The air graphviz's SVG writer puts round a drawing when the source
	// named no pad of its own. Measured: a 367.44 x 36pt bounding box is
	// written onto a 375.44 x 44pt canvas.
	svgPad = 4.0
)

// box is a rectangle in graphviz's points, origin bottom-left.
type box struct{ x0, y0, x1, y1 float64 }

func (b box) w() float64 { return b.x1 - b.x0 }
func (b box) h() float64 { return b.y1 - b.y0 }

// canvas is the ground the picture stands on, in points: graphviz's own,
// which is the rectangle its background op paints. Measured, that
// rectangle is the bounding box grown by the graph's `pad` — a
// `pad="0.15,0.3"` picture's is the box out 10.8pt across and 21.6pt down
// — and where the source names no pad it is the bare bounding box, which
// its SVG writer then pads by 4pt itself. So this does too: a stroke
// centred on the boundary would otherwise lose half its width off the
// edge.
//
// It is a whole number of points, which is how that same writer wrote the
// canvas: the cut is arithmetic on this number, and a terminal column is
// not bought with a hundredth of a point. Measured on corpus/unicode.dot,
// whose canvas is 105.111pt across — 14.015 columns of a 10px cell — the
// fraction alone cut a fifteenth column of empty air, and edges.dot and
// shapes.dot each grew a row the same way. The far corner is the one that
// moves, so every point of the drawing lands where it landed.
func canvas(d *layout.Drawing) box {
	b := box{0, 0, d.W, d.H}
	for _, op := range d.Draw {
		if op.Op != "P" || len(op.Points) < 3 {
			continue
		}
		b = box{op.Points[0][0], op.Points[0][1], op.Points[0][0], op.Points[0][1]}
		for _, pt := range op.Points {
			b.x0, b.y0 = math.Min(b.x0, pt[0]), math.Min(b.y0, pt[1])
			b.x1, b.y1 = math.Max(b.x1, pt[0]), math.Max(b.y1, pt[1])
		}
		break
	}
	if d.Pad == "" {
		b = box{b.x0 - svgPad, b.y0 - svgPad, b.x1 + svgPad, b.y1 + svgPad}
	}
	b.x1, b.y0 = b.x0+math.Round(b.w()), b.y1-math.Round(b.h())
	return b
}

// paintPNG is the picture at a zoom, encoded as the terminal takes it.
func paintPNG(ctx context.Context, d *layout.Drawing, face string, zoom float64) ([]byte, error) {
	im, err := paint(ctx, d, face, zoom)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paint draws a laid-out picture at a zoom. face is the theme's, which is
// a face only where it names a font file; see the font table below.
//
// The context is read once, here, the way layout.Door reads it: painting
// is arithmetic in this process and measured in milliseconds, so there is
// nothing to interrupt — what a spent deadline buys is that the picture
// nobody is waiting for any more is not painted at all.
func paint(ctx context.Context, d *layout.Drawing, face string, zoom float64) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := canvas(d)
	s := zoom * pxPerPt
	// The size is judged before it is a number of pixels: a float too big
	// for an int converts to whatever the machine feels like, and this is
	// the one place a caller's arithmetic reaches an allocation.
	wf, hf := b.w()*s, b.h()*s
	if !(wf >= 1 && hf >= 1) {
		return nil, errors.New("the picture has no size")
	}
	if wf*hf > maxPixels {
		return nil, fmt.Errorf("%.0fx%.0f px; the ceiling is %d pixels", wf, hf, maxPixels)
	}
	// Rounded, not ceilinged: the cut chose this zoom so that the width
	// lands on a whole number of columns, and 120 columns of arithmetic
	// that comes out 120.00000000000001 must not be a 121-pixel picture the
	// terminal then scales.
	w, h := int(math.Round(wf)), int(math.Round(hf))
	p := &painter{dc: gg.NewContext(w, h), s: s, box: b, file: fontFile(face)}
	// A stroke ends flat, which is cairo's own cap and what graphviz's SVG
	// asked for by naming none. The join is the one thing gg cannot follow:
	// its rasteriser mitres nothing, and round is the nearest it has. On a
	// spline or a polyline that is a difference nobody can see, because a
	// flattened curve turns by a fraction of a degree at a time; on a
	// polygon's corners it shows at any pen wider than a hair, so a
	// polygon's outline is laid by hand instead — see strokeClosed.
	p.dc.SetLineCap(gg.LineCapButt)
	p.dc.SetLineJoin(gg.LineJoinRound)
	// The graph's own background is a trap: graphviz writes it whether or
	// not anybody asked for one, and it says white when nobody did.
	// Painting it unasked is an opaque slab under every picture — over a
	// translucent terminal it is the first thing a reader sees and the one
	// thing that says "pasted in". bgcolor is the attribute itself, absent
	// where the source set none.
	if bg := strings.TrimSpace(d.BGColor); bg != "" && bg != "transparent" && bg != "none" {
		p.ground(d.Draw)
	}
	p.objects(d)
	p.ops(d.LDraw)
	return p.dc.Image().(*image.RGBA), nil
}

// ground paints the graph's background over the whole canvas.
//
// The shape is the canvas and not the polygon in the list, because that
// polygon is the bare bounding box wherever the source named no pad — where
// graphviz's SVG writer wrote a background over the padded canvas instead.
// Measured, a `bgcolor=white` picture under a theme that names no pad came
// out with a 4pt transparent frame the old picture had filled.
//
// The colour is the list's, though: the attribute arrives as the model
// wrote it — `white`, `#1a1b26`, a gradient — and only the drawing list
// carries what graphviz resolved that to. A pen of no colour, so the ground
// is filled and not also outlined.
func (p *painter) ground(list []layout.Op) {
	for _, op := range list {
		if op.Op != "C" {
			continue
		}
		st := pen{fill: hexColour(op.Color), grad: p.gradient(op)}
		p.dc.ClearPath()
		p.dc.DrawRectangle(0, 0, float64(p.dc.Width()), float64(p.dc.Height()))
		p.ink(&st, true, nil)
		return
	}
}

// painter is one picture being painted: where the points go, and what the
// type is set in.
type painter struct {
	dc  *gg.Context
	s   float64 // points to pixels
	box box     // the canvas, in points
	// file is the font file the theme named, empty for Go Mono.
	file string
}

// x and y put a point of graphviz's on the image: its origin is the
// canvas's bottom-left corner and its y goes up the page.
func (p *painter) x(v float64) float64 { return (v - p.box.x0) * p.s }
func (p *painter) y(v float64) float64 { return (p.box.y1 - v) * p.s }

// objects paints the drawing in graphviz's own order: every cluster, then
// the nodes and edges the way its emitter walks them — a node, then for
// each edge leaving it the node at the far end and the edge itself.
// Measured on demo/graph.dot, whose SVG runs client, lb, client->lb, api1,
// lb->api1: painting every node and then every edge would lay an arrowhead
// over the border it stops at.
func (p *painter) objects(d *layout.Drawing) {
	for i := range d.Objects {
		if d.Objects[i].Nodes != nil {
			p.ops(d.Objects[i].Draw)
			p.ops(d.Objects[i].LDraw)
		}
	}
	painted := make([]bool, len(d.Objects))
	node := func(i int) {
		if i < 0 || i >= len(d.Objects) || painted[i] || d.Objects[i].Nodes != nil {
			return
		}
		painted[i] = true
		p.ops(d.Objects[i].Draw)
		p.ops(d.Objects[i].LDraw)
	}
	for i := range d.Objects {
		node(i)
		for j := range d.Edges {
			e := &d.Edges[j]
			if e.Tail != i {
				continue
			}
			node(e.Head)
			p.ops(e.Draw)
			p.ops(e.HDraw)
			p.ops(e.TDraw)
			p.ops(e.LDraw)
		}
	}
}

// pen is the state one drawing list carries.
type pen struct {
	colour, fill color.NRGBA
	grad         gg.Gradient
	width        float64   // in points
	dash         []float64 // in points
	size         float64   // type, in points
	face         string
	fontchar     int
}

// ops runs one drawing list. It starts from graphviz's own defaults — a
// black hairline and 14pt type — because a list names every colour and
// style it means and inherits nothing from the object beside it.
func (p *painter) ops(list []layout.Op) {
	black := color.NRGBA{A: 0xff}
	st := pen{colour: black, fill: black, width: 1, size: 14}
	for _, op := range list {
		switch op.Op {
		case "c":
			st.colour = hexColour(op.Color)
		case "C":
			st.fill, st.grad = hexColour(op.Color), p.gradient(op)
		case "S":
			if !st.style(op.Style) {
				return
			}
		case "F":
			st.size, st.face = op.Size, op.Face
		case "t":
			st.fontchar = op.FontChar
		case "p", "P":
			p.polyline(op.Points)
			p.dc.ClosePath()
			p.ink(&st, op.Op == "P", op.Points)
		case "L":
			p.polyline(op.Points)
			p.ink(&st, false, nil)
		case "b", "B":
			p.bezier(op.Points)
			p.ink(&st, op.Op == "B", nil)
		case "e", "E":
			p.ellipse(op.Rect)
			p.ink(&st, op.Op == "E", nil)
		case "T":
			p.text(op, &st)
		}
	}
}

// style is one S op. graphviz writes a line width as setlinewidth(N) and
// `bold` as setlinewidth(2); `tapered` is drawn as a filled shape and
// needs nothing here. False ends the list: an invisible object draws
// nothing at all.
func (st *pen) style(s string) bool {
	switch {
	case s == "invis":
		return false
	case s == "solid":
		st.dash = nil
	// The dashes graphviz's SVG writer asks for, in points, so a dashed
	// edge is dashed the way it was before.
	case s == "dashed":
		st.dash = []float64{5, 2}
	case s == "dotted":
		st.dash = []float64{1, 5}
	case strings.HasPrefix(s, "setlinewidth("):
		if w := layout.Atof(strings.TrimSuffix(strings.TrimPrefix(s, "setlinewidth("), ")")); w > 0 {
			st.width = w
		}
	}
	return true
}

// ink lays the path that was just built: a filled shape takes the fill and
// then its outline, an outline takes the pen alone. closed is the polygon's
// own points where the path is a closed polygon and nil otherwise — that
// outline is laid by hand, because gg's stroker has no corner sharp enough
// for an arrowhead. A dashed polygon goes through the stroker anyway: the
// dashes are gg's to lay, and a cluster's dashed border has no sharp corner
// to lose.
func (p *painter) ink(st *pen, fill bool, closed [][2]float64) {
	if fill && (st.fill.A > 0 || st.grad != nil) {
		if st.grad != nil {
			p.dc.SetFillStyle(st.grad)
		} else {
			p.dc.SetFillStyle(gg.NewSolidPattern(st.fill))
		}
		p.dc.FillPreserve()
	}
	if st.colour.A == 0 {
		p.dc.ClearPath()
		return
	}
	p.dc.SetColor(st.colour)
	if closed != nil && st.dash == nil {
		p.strokeClosed(closed, st.width*p.s)
		return
	}
	p.dc.SetLineWidth(st.width * p.s)
	p.dc.SetDash(scaled(st.dash, p.s)...)
	p.dc.Stroke()
}

// strokeClosed lays a closed polygon's outline: mitred at every corner, and
// the seam is a corner like the rest of them.
//
// gg has neither. Its rasteriser strokes a closed path as an open polyline
// — the two ends meet at the first vertex and each gets a flat cap — and it
// rounds every turn, and both show. Measured on corpus/edges.dot against
// the picture rsvg drew from the same graph: the seam grew a square spur on
// every head wider than a hairline, and the tip of the `penwidth=4` head
// stopped four pixels short of the node it points at, because a round join
// reaches half a pen width past the vertex where a miter reaches
// (w/2)/sin(θ/2) — 4.5pt at an arrowhead's nineteen degrees.
//
// So the outline is filled rather than stroked: a quad along every segment
// and a wedge at every corner, each wound the same way, which under the
// nonzero rule gg fills by is their union. Every piece is convex whatever
// the polygon is, so a shape that folds into itself costs nothing to think
// about. The miter limit is cairo's ten — past that the corner is so sharp
// that its point would fly off on its own, and cairo bevels it too.
func (p *painter) strokeClosed(pts [][2]float64, w float64) {
	q := make([][2]float64, 0, len(pts))
	for _, pt := range pts {
		v := [2]float64{p.x(pt[0]), p.y(pt[1])}
		if n := len(q); n > 0 && same(q[n-1], v) {
			continue
		}
		q = append(q, v)
	}
	for len(q) > 1 && same(q[0], q[len(q)-1]) {
		q = q[:len(q)-1]
	}
	if len(q) < 2 || w <= 0 {
		return
	}
	hw := w / 2
	p.dc.ClearPath()
	for i, a := range q {
		b := q[(i+1)%len(q)]
		dx, dy, ok := unit(b[0]-a[0], b[1]-a[1])
		if !ok {
			continue
		}
		nx, ny := -dy*hw, dx*hw
		p.piece([][2]float64{
			{a[0] + nx, a[1] + ny}, {b[0] + nx, b[1] + ny},
			{b[0] - nx, b[1] - ny}, {a[0] - nx, a[1] - ny},
		})
	}
	for i := range q {
		p.corner(q[(i+len(q)-1)%len(q)], q[i], q[(i+1)%len(q)], hw)
	}
	p.dc.Fill()
}

// corner is the join at v, between the segment arriving from a and the one
// leaving for b: the bevel that closes the gap the two offsets leave on the
// outside of the turn, and the miter that fills its point.
func (p *painter) corner(a, v, b [2]float64, hw float64) {
	d0x, d0y, ok0 := unit(v[0]-a[0], v[1]-a[1])
	d1x, d1y, ok1 := unit(b[0]-v[0], b[1]-v[1])
	if !ok0 || !ok1 {
		return
	}
	// The gap opens on the outside of the turn, and which side that is is
	// the sign of the turn itself. Straight through, or doubled back on
	// itself, there is no gap and no join.
	cross := d0x*d1y - d0y*d1x
	if math.Abs(cross) < 1e-9 {
		return
	}
	s := hw
	if cross > 0 {
		s = -hw
	}
	p0 := [2]float64{v[0] - d0y*s, v[1] + d0x*s}
	p1 := [2]float64{v[0] - d1y*s, v[1] + d1x*s}
	p.piece([][2]float64{v, p0, p1})
	t := ((p1[0]-p0[0])*d1y - (p1[1]-p0[1])*d1x) / cross
	m := [2]float64{p0[0] + t*d0x, p0[1] + t*d0y}
	if math.Hypot(m[0]-v[0], m[1]-v[1]) > 10*hw {
		return
	}
	p.piece([][2]float64{p0, m, p1})
}

// piece adds one convex patch of an outline, wound the way its neighbours
// are: the nonzero rule counts the turns a path makes round a point, so two
// patches wound against each other would cancel where they overlap instead
// of joining there.
func (p *painter) piece(pts [][2]float64) {
	area := 0.0
	for i, a := range pts {
		b := pts[(i+1)%len(pts)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	if area < 0 {
		slices.Reverse(pts)
	}
	p.dc.MoveTo(pts[0][0], pts[0][1])
	for _, pt := range pts[1:] {
		p.dc.LineTo(pt[0], pt[1])
	}
	p.dc.ClosePath()
}

// unit is a direction, false where there is no direction to have.
func unit(x, y float64) (float64, float64, bool) {
	l := math.Hypot(x, y)
	if l < 1e-9 {
		return 0, 0, false
	}
	return x / l, y / l, true
}

// same is two points a rasteriser could not tell apart.
func same(a, b [2]float64) bool { return math.Hypot(a[0]-b[0], a[1]-b[1]) < 1e-9 }

// polyline builds a run of points, and polygons close it afterwards.
func (p *painter) polyline(pts [][2]float64) {
	p.dc.ClearPath()
	for i, pt := range pts {
		if i == 0 {
			p.dc.MoveTo(p.x(pt[0]), p.y(pt[1]))
		} else {
			p.dc.LineTo(p.x(pt[0]), p.y(pt[1]))
		}
	}
}

// bezier builds graphviz's cubic run: 3n+1 points, n curves.
func (p *painter) bezier(pts [][2]float64) {
	if len(pts) < 4 {
		p.polyline(pts)
		return
	}
	p.dc.ClearPath()
	p.dc.MoveTo(p.x(pts[0][0]), p.y(pts[0][1]))
	for i := 0; i+3 < len(pts); i += 3 {
		p.dc.CubicTo(
			p.x(pts[i+1][0]), p.y(pts[i+1][1]),
			p.x(pts[i+2][0]), p.y(pts[i+2][1]),
			p.x(pts[i+3][0]), p.y(pts[i+3][1]))
	}
}

// ellipse builds graphviz's [cx, cy, rx, ry].
func (p *painter) ellipse(r []float64) {
	p.dc.ClearPath()
	if len(r) < 4 {
		return
	}
	p.dc.DrawEllipse(p.x(r[0]), p.y(r[1]), r[2]*p.s, r[3]*p.s)
}

// text sets one run: the baseline at pt, the run laid across it by align.
//
// What it is centred on is the width the run will actually take, and not
// the width graphviz reserved for it. With no font system the wasm
// measures a label byte by byte — measured, "päivää" is nine characters
// wide to it and six to anybody setting it — so a run centred on the
// reservation sits a glyph and a half left of its own box. For the ASCII
// the layout was measured in the two numbers are the same one: Courier's
// advance is 0.600em and Go Mono's 0.602. A run graphviz anchored on the
// left is not measured at all; its x is already where it goes.
func (p *painter) text(op layout.Op, st *pen) {
	if op.Text == "" || len(op.Pt) < 2 || st.colour.A == 0 || st.size <= 0 {
		return
	}
	f := p.font(st)
	if f == nil {
		return
	}
	p.dc.SetFontFace(f)
	run := notdef(f, op.Text)
	w, _ := p.dc.MeasureString(run)
	px, py := p.x(op.Pt[0]), p.y(op.Pt[1])
	switch op.Align {
	case "c":
		px -= w / 2
	case "r":
		px -= w
	}
	p.dc.SetColor(st.colour)
	p.dc.DrawString(run, px, py)
	// Underline is the one font bit with a mark of its own to draw; the
	// rest — superscript, subscript, strikethrough — are set as ordinary
	// type and lose only their decoration.
	if st.fontchar&4 != 0 {
		size := st.size * p.s
		p.dc.DrawRectangle(px, py+size*0.12, w, math.Max(size/14, 1))
		p.dc.Fill()
	}
}

// notdef is a run with every rune the face has no glyph for standing in the
// one glyph that says so.
//
// Left alone the two halves of setting a run disagree: the measurement pays
// the advance of a rune it cannot draw and the drawing skips both the glyph
// and its space, so "AB日CD" and "ABCD" paint as the same four letters and
// the survivors sit half a glyph off their own centre. A reader cannot see
// that a character was dropped, which is the one thing worse than seeing
// that it was.
//
// Measured on Go Mono: `日`, `本` and `☃` have no glyph in it and U+FFFD
// has, so the box is there to draw. A face from a theme's own file may have
// neither, and then the run is set as it came and the picture still draws.
func notdef(f font.Face, s string) string {
	missing := func(r rune) bool {
		_, ok := f.GlyphAdvance(r)
		return !ok
	}
	if !strings.ContainsFunc(s, missing) || missing('\uFFFD') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if missing(r) {
			r = '\uFFFD'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// gradient is the fill a C op carries when it is not flat. graphviz gives
// the ends in the drawing's own points and the stops along them; gg paints
// in pixels, so the ends convert on the way in.
func (p *painter) gradient(op layout.Op) gg.Gradient {
	if len(op.P0) < 2 || len(op.P1) < 2 {
		return nil
	}
	var g gg.Gradient
	switch op.Grad {
	case "linear":
		g = gg.NewLinearGradient(p.x(op.P0[0]), p.y(op.P0[1]), p.x(op.P1[0]), p.y(op.P1[1]))
	case "radial":
		g = gg.NewRadialGradient(
			p.x(op.P0[0]), p.y(op.P0[1]), op.R0*p.s,
			p.x(op.P1[0]), p.y(op.P1[1]), op.R1*p.s)
	default:
		return nil
	}
	for _, s := range op.Stops {
		g.AddColorStop(s.Frac, hexColour(s.Color))
	}
	return g
}

// hexColour reads a colour as graphviz resolved it. Measured: every colour
// in the json arrives as #rrggbb or #rrggbbaa, names and all — `pink` is
// `#ffc0cb`, `transparent` is `#fffffe00`. Anything else is graphviz's own
// default, black, rather than a hole in the picture.
//
// Not premultiplied: an image.RGBA carries its colours multiplied by their
// own alpha, and a theme's `#7aa2f71f` handed over that way is a fill
// eight times too bright.
func hexColour(s string) color.NRGBA {
	black := color.NRGBA{A: 0xff}
	if !strings.HasPrefix(s, "#") || (len(s) != 7 && len(s) != 9) {
		return black
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return black
	}
	if len(s) == 7 {
		v = v<<8 | 0xff
	}
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// scaled is a dash pattern in pixels.
func scaled(dash []float64, s float64) []float64 {
	out := make([]float64, len(dash))
	for i, v := range dash {
		out[i] = v * s
	}
	return out
}

// ---------- the type, carried in the binary ----------

// Go Mono, in its four faces. graphviz measured the layout in Courier —
// the wasm has no font system, and Courier is one of the three faces it
// knows the metrics of exactly — and Go Mono's advance is 0.602em against
// Courier's 0.600, so a label measured in the one and set in the other
// still stands in its box. Nothing is asked of the box: no fontconfig, no
// file, no face the terminal happens to have.
//
// A theme's fontname survives as a narrower rule. It may name a font file
// — an absolute path, or a file name findfont locates in the system font
// directories — and that one face is then set for every span of the
// picture. Anything else, "monospace" and every family name included, and
// any file that will not parse, is Go Mono, silently: the picture always
// draws. Bold and italic are honoured only in Go Mono, which has the four
// faces; a file names one face and gets one.
var goMono = [...][]byte{
	gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF,
}

// fontFile is the file a theme's fontname names, or empty for Go Mono. A
// name with no font file's extension is not looked for at all: the face a
// theme leaves undeclared is "monospace", and walking every font directory
// on the box to not find it is a walk on every picture.
func fontFile(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc":
	default:
		return ""
	}
	if filepath.IsAbs(name) {
		if !isFont(name) {
			return ""
		}
		return name
	}
	path, err := findfont.Find(name)
	if err != nil || !isFont(path) {
		return ""
	}
	return path
}

// isFont says whether a path is a file a face could be read from: a regular
// file, within the ceiling. That a file exists is not enough to read it —
// os.Stat is as happy with a fifo, a character device and a three-gigabyte
// file as with a font. Measured on this box, a theme naming /dev/zero never
// returned at all and was past ten gigabytes of memory at fifteen seconds,
// where the same picture had drawn in 24ms; a three-gigabyte file peaked at
// 2.8GB before it was found not to be a font. The painter carries no
// deadline of its own, so a read that does not end is a hook that never
// answers.
func isFont(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() <= maxFontBytes
}

// readFont is a font file in memory, nil where there is nothing safe to
// read. The guard is repeated here, where the memory is actually taken,
// because this is the only place that matters if a caller ever arrives with
// a path fontFile did not resolve.
func readFont(path string) []byte {
	if !isFont(path) {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

// font is the face a span is set in: the theme's file where it named one
// that parses, else Go Mono in the weight and slope the span asks for. The
// span asks in two ways, and both are graphviz's — an HTML `<b>` arrives
// as a font bit on the run, and a `fontname="Courier-Bold"` arrives as the
// face's own name.
func (p *painter) font(st *pen) font.Face {
	bold := st.fontchar&1 != 0 || strings.Contains(st.face, "Bold")
	italic := st.fontchar&2 != 0 ||
		strings.Contains(st.face, "Italic") || strings.Contains(st.face, "Oblique")
	px := st.size * p.s
	if f := face(faceKey{fontKey{p.file, bold, italic}, px}); f != nil {
		return f
	}
	return face(faceKey{fontKey{"", bold, italic}, px})
}

type fontKey struct {
	file         string
	bold, italic bool
}

type faceKey struct {
	fontKey
	px float64
}

var (
	facesMu sync.Mutex
	fonts   = map[fontKey]*sfnt.Font{}
	faces   = map[faceKey]font.Face{}
)

// face is one face at one size, held for the life of the process: a
// picture sets the same face at the same size a hundred times, and parsing
// the font is the expensive part. Nil where the file will not read or
// parse, which is the caller's cue to fall to Go Mono.
//
// The size is in pixels and the face is asked for at 72 dpi, which is the
// resolution at which a point is a pixel: the zoom is already in the
// number.
func face(k faceKey) font.Face {
	facesMu.Lock()
	defer facesMu.Unlock()
	if f, ok := faces[k]; ok {
		return f
	}
	faces[k] = nil // a file that will not parse is read once, not once a run
	sf, ok := fonts[k.fontKey]
	if !ok {
		ttf := goMono[0]
		switch {
		case k.bold && k.italic:
			ttf = goMono[3]
		case k.bold:
			ttf = goMono[1]
		case k.italic:
			ttf = goMono[2]
		}
		if k.file != "" {
			b := readFont(k.file)
			if b == nil {
				fonts[k.fontKey] = nil
				return nil
			}
			ttf = b
		}
		sf, _ = opentype.Parse(ttf)
		fonts[k.fontKey] = sf
	}
	if sf == nil {
		return nil
	}
	f, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: k.px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	faces[k] = f
	return f
}
