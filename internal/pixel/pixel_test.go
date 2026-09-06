// pixel_test.go — laws for the pixels rung: the placeholders, the cut, the
// theme on the picture, the paint on the image, and the labels on their
// lines.

package pixel

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The hook wire quantises a truecolor foreground, so a picture's id rides
// in a 256-colour index and a third diacritic. The low byte is never zero
// — zero is "no image" — and every cell names its row and column itself,
// because CC re-wraps what a hook returns and a cell that lost its
// neighbour must not lose its place.
func TestPlaceholderRowsNameTheirImageOnEveryCell(t *testing.T) {
	id := ImageID("digraph { a -> b }", 12, 3)
	if id&0xff == 0 {
		t.Fatal("image id has a zero low byte")
	}
	rows := PlaceholderRows(id, 12, 3)
	if len(rows) != 3 {
		t.Fatalf("%d rows for a 3-row block", len(rows))
	}
	for r, row := range rows {
		if !strings.HasPrefix(row, "\x1b[38;5;") {
			t.Fatalf("row %d does not open with a 256-colour foreground: %q", r, row)
		}
		plain := grid.StripSGR(row)
		cells := strings.Count(plain, string(PlaceholderRune))
		if cells != 12 {
			t.Fatalf("row %d has %d placeholder cells, want 12", r, cells)
		}
		want := string(PlaceholderRune) + string(rowColumnDiacritics[r]) + string(rowColumnDiacritics[0])
		if !strings.HasPrefix(plain, want) {
			t.Fatalf("row %d does not start with row-then-column marks", r)
		}
		if grid.Cells(plain) != 12 {
			t.Fatalf("row %d measures %d cells; the marks took columns", r, grid.Cells(plain))
		}
	}
}

// ---------- reading a drawing ----------

// inked reports whether a drawing list carries an op of that code in that
// colour: `c` is the pen, `C` the fill, and graphviz resolves both to hex
// before this ever sees them.
func inked(list []layout.Op, code, colour string) bool {
	for _, op := range list {
		if op.Op == code && op.Color == colour {
			return true
		}
	}
	return false
}

// shaped reports whether a list draws one of those ops at all.
func shaped(list []layout.Op, codes ...string) bool {
	for _, op := range list {
		if slices.Contains(codes, op.Op) {
			return true
		}
	}
	return false
}

// set reports whether a list sets that text, as one run of it.
func set(list []layout.Op, text string) bool {
	for _, op := range list {
		if op.Op == "T" && op.Text == text {
			return true
		}
	}
	return false
}

// heads counts the arrowheads on an edge: a filled shape at either end.
func heads(e *layout.Edge) int {
	n := 0
	for _, list := range [][]layout.Op{e.HDraw, e.TDraw} {
		for _, op := range list {
			if op.Op == "P" || op.Op == "E" || op.Op == "B" {
				n++
			}
		}
	}
	return n
}

// ---------- the pixel theme ----------

// Type is measured in Courier, which the wasm's tables know exactly, and
// set in Go Mono, which this binary carries: a label measured in one face
// and set in another of a different advance runs out of its box, and those
// two advances are 0.600em and 0.602em. At a known cell width the size is
// the one that puts a glyph in a cell.
func TestPixelTypeIsMeasuredInCourierAndSetInGoMono(t *testing.T) {
	th := mustTheme(t, theme.ClaudeDOT())
	d, err := RenderThemed(t.Context(), th, "digraph { a -> b }", FontPt(10), "")
	if err != nil {
		t.Fatal(err)
	}
	a := d.Object("a")
	if a == nil {
		t.Fatal("the drawing has no node a")
	}
	var font *layout.Op
	for i, op := range a.LDraw {
		if op.Op == "F" {
			font = &a.LDraw[i]
			break
		}
	}
	if font == nil {
		t.Fatal("the label sets no font at all")
	}
	if font.Face != pxLayoutFont {
		t.Errorf("the layout was measured in %q, want %s", font.Face, pxLayoutFont)
	}
	if font.Size != 12.5 {
		t.Errorf("a 10px cell wants 12.5pt type; got %v", font.Size)
	}
	if f := fontFile(th.Face()); f != "" {
		t.Errorf("the face a theme leaves undeclared was looked for on the box, and found %q", f)
	}
	if face(faceKey{fontKey{"", false, false}, font.Size}) == nil {
		t.Error("Go Mono is not in this binary")
	}
}

// What the model painted stays painted, and graphviz's own defaults apply
// around its paint: a pink node keeps black text, as `dot` would give it. A
// shape it asked for is drawn, records included. Where it left an attribute
// unset, the theme fills in.
func TestPixelThemeKeepsTheModelsPaint(t *testing.T) {
	src := `digraph {
		a [fillcolor=pink, style=filled, color=red]
		b [shape=ellipse]
		c [shape=record, label="{head|body|tail}"]
		d
		a -> b -> c -> d
	}`
	th := mustTheme(t, theme.ClaudeDOT())
	dr, err := RenderThemed(t.Context(), th, src, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a, b, c, d := dr.Object("a"), dr.Object("b"), dr.Object("c"), dr.Object("d")
	if a == nil || b == nil || c == nil || d == nil {
		t.Fatal("the drawing is missing a node")
	}
	if !inked(a.Draw, "C", "#ffc0cb") || !inked(a.Draw, "c", "#ff0000") {
		t.Errorf("the model's paint was overwritten:\n%+v", a.Draw)
	}
	if inked(a.LDraw, "c", th.Node["fontcolor"]) {
		t.Errorf("theme text on the model's fill:\n%+v", a.LDraw)
	}
	if !shaped(b.Draw, "e", "E") {
		t.Errorf("the model asked for an ellipse and got %+v", b.Draw)
	}
	if set(c.LDraw, "{head|body|tail}") || !set(c.LDraw, "body") {
		t.Errorf("the record printed its markup:\n%+v", c.LDraw)
	}
	if !inked(d.Draw, "C", th.Node["fillcolor"]) || !inked(d.LDraw, "c", th.Node["fontcolor"]) ||
		!inked(d.Draw, "c", th.Node["color"]) {
		t.Errorf("an unpainted node did not get the theme:\n%+v\n%+v", d.Draw, d.LDraw)
	}
}

// Every cluster is themed, not only the first. A subgraph answers for
// attributes it never set with the root's value, so the clusters have to be
// read before the root is themed; the second cluster is where that showed.
func TestPixelThemeReachesEveryCluster(t *testing.T) {
	src := `digraph {
		subgraph cluster_a { label="a side"; x }
		subgraph cluster_b { label="b side"; y }
		x -> y
	}`
	th := mustTheme(t, theme.ClaudeDOT())
	d, err := RenderThemed(t.Context(), th, src, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cluster_a", "cluster_b"} {
		g := d.Object(name)
		if g == nil {
			t.Fatalf("the drawing has no %s", name)
		}
		if !inked(g.Draw, "c", th.Graph["color"]) {
			t.Errorf("%s outline is not themed:\n%+v", name, g.Draw)
		}
		if !inked(g.LDraw, "c", th.Graph["fontcolor"]) {
			t.Errorf("%s label is not themed:\n%+v", name, g.LDraw)
		}
		if !hasFace(g.LDraw, pxLayoutFont) {
			t.Errorf("%s label was not measured in %s:\n%+v", name, pxLayoutFont, g.LDraw)
		}
	}
}

// hasFace reports whether a list sets its type in that face.
func hasFace(list []layout.Op, face string) bool {
	for _, op := range list {
		if op.Op == "F" && op.Face == face {
			return true
		}
	}
	return false
}

// The picture stands on the terminal's own ground: no background unless the
// model asked for one. graphviz writes a background op either way — white,
// when nobody asked — so the answer is in the pixels, at the corner where
// nothing else is ever drawn.
//
// Under any theme, which is why the theme that names no pad is here too:
// there the canvas is 4pt wider than the polygon graphviz wrote the
// background as, and the corner is the pixel that says so.
func TestPixelBackgroundIsTheTerminalsUnlessSet(t *testing.T) {
	corner := func(th *theme.Theme, src string) color.NRGBA {
		t.Helper()
		d, err := RenderThemed(t.Context(), th, src, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		im, err := paint(t.Context(), d, th.Face(), 1)
		if err != nil {
			t.Fatal(err)
		}
		b := im.Bounds()
		return color.NRGBAModel.Convert(im.At(b.Min.X, b.Min.Y)).(color.NRGBA)
	}
	for _, th := range []*theme.Theme{mustTheme(t, theme.ClaudeDOT()), mustTheme(t, "")} {
		if c := corner(th, "digraph { a -> b }"); c.A != 0 {
			t.Errorf("a graph that asked for no background stands on %v; want the terminal's own ground", c)
		}
		if c := (corner(th, "digraph { bgcolor=white; a -> b }")); c != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
			t.Errorf("the model's white background came out %v", c)
		}
	}
}

// ---------- the paint ----------

// A filled shape is filled with the fill in force AND its outline stroked
// with the pen in force: that is what an uppercase op means in xdot, and it
// is the rule go-graphviz's own renderer drops. Read off the image, because
// the drawing says both and only the paint can say whether both happened.
func TestPixelFilledShapeIsFilledAndStroked(t *testing.T) {
	th := mustTheme(t, "")
	src := `digraph { a [shape=box, style=filled, fillcolor="#00ff00", color="#ff0000", penwidth=4, label=""] }`
	d, err := RenderThemed(t.Context(), th, src, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	im, err := paint(t.Context(), d, th.Face(), 1)
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.NRGBA {
		return color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
	}
	// Inside the box and clear of its label, which graphviz sets from the
	// node's own name where the source leaves it empty.
	b := im.Bounds()
	inside := at(b.Dx()/2, b.Dy()/4)
	if inside != (color.NRGBA{G: 0xff, A: 0xff}) {
		t.Errorf("the inside of a filled box is %v, want the fill", inside)
	}
	// The box is the whole drawing, so its outline runs across the picture
	// one pad down from the top, and a four-point pen is wide enough to
	// land a sample on.
	top := svgPad * pxPerPt
	edge := at(b.Dx()/2, int(top))
	if edge != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Errorf("the outline of a filled box is %v, want the pen", edge)
	}
}

// A closed polygon's outline is mitred at its corners and closed at its
// seam. gg's stroker does neither: it strokes a closed path as an open
// polyline, so the seam gets two flat caps and grows a square spur, and it
// rounds every corner, so a point stops short of what it was pointing at. A
// miter reaches (w/2)/sin(θ/2) past a corner where a round join reaches w/2,
// and an arrowhead's corner is sharp enough for that to be the difference
// between touching a node and missing it.
func TestPixelAPolygonsOutlineIsMitredAndClosed(t *testing.T) {
	// An arrowhead's own shape, pointing down the page: 10pt of half-width
	// over 30pt of length is a half-angle of 18.4 degrees, so a 6pt pen
	// mitres 9.5pt past the point where a round join would reach 3.
	d := &layout.Drawing{W: 100, H: 100, Pad: "0", Objects: []layout.Object{{
		Name: "head",
		Draw: []layout.Op{
			{Op: "c", Color: "#000000"},
			{Op: "C", Color: "#000000"},
			{Op: "S", Style: "setlinewidth(6)"},
			{Op: "P", Points: [][2]float64{{40, 50}, {60, 50}, {50, 20}}},
		},
	}}}
	im, err := paint(t.Context(), d, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	inked := func(x, y float64) bool {
		_, _, _, a := im.At(int(x*pxPerPt), int((100-y)*pxPerPt)).RGBA()
		return a > 0x7fff
	}
	if !inked(50, 13) {
		t.Error("the point stops short of its miter, 7pt past the corner")
	}
	if inked(50, 9) {
		t.Error("the point runs past its miter, 11pt past the corner")
	}
	// A spur grows at the first vertex alone, so the tell is that the
	// picture stops being symmetric about the head's own axis.
	b := im.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		lo, hi := -1, -1
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := im.At(x, y).RGBA(); a > 0x7fff {
				if lo < 0 {
					lo = x
				}
				hi = x
			}
		}
		if lo < 0 {
			continue
		}
		if axis := 50 * pxPerPt; math.Abs(float64(lo+hi)/2-axis) > 1 {
			t.Fatalf("row %d runs from %d to %d, off the head's axis at %.1f: the seam grew a spur", y, lo, hi, axis)
		}
	}
}

// A rune the carried face has no glyph for is set as U+FFFD and not
// dropped. gg draws nothing at all for a missing rune — neither the glyph
// nor its advance — while the measurement that centres the run pays for it,
// so a dropped rune costs the reader the character and slides what is left
// off its own centre. A label that lost a character has to say so.
func TestPixelAMissingGlyphIsSetAsOne(t *testing.T) {
	f := face(faceKey{fontKey{"", false, false}, 12})
	if f == nil {
		t.Fatal("Go Mono is not in this binary")
	}
	if got := notdef(f, "AB日CD"); got != "AB\ufffdCD" {
		t.Errorf("a run with an ideograph in it came out %q", got)
	}
	if got := notdef(f, "päivää"); got != "päivää" {
		t.Errorf("a run the face can set was rewritten to %q", got)
	}
}

// The picture stands on graphviz's own canvas: the bounding box, plus the
// air the graph asked for with `pad`, and where it asked for none the 4pt
// its SVG writer would have added — a stroke on the boundary would
// otherwise lose half its width off the edge — and the whole thing rounded
// to whole points, as that writer rounded it, because the cut is arithmetic
// on this number and a hundredth of a point must not buy a column.
func TestPixelCanvasIsTheBoxAndItsPad(t *testing.T) {
	th := mustTheme(t, "")
	d, err := RenderThemed(t.Context(), th, "digraph { pad=0.15; a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if c := canvas(d); c.w() != math.Round(d.W+2*10.8) || c.h() != math.Round(d.H+2*10.8) {
		t.Errorf("a pad of 0.15in put %vx%v round a %vx%v box, want 10.8pt on each side",
			c.w()-d.W, c.h()-d.H, d.W, d.H)
	}
	d, err = RenderThemed(t.Context(), th, "digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if c := canvas(d); c.w() != math.Round(d.W+2*svgPad) || c.h() != math.Round(d.H+2*svgPad) {
		t.Errorf("a graph that named no pad got %vx%v of air, want %vpt on each side",
			c.w()-d.W, c.h()-d.H, svgPad)
	}
	// The far corner is the one the rounding moves: a point of the drawing
	// lands where it landed before the canvas was rounded at all.
	if c := canvas(d); c.x0 != -svgPad || c.y1 != d.H+svgPad {
		t.Errorf("the canvas's near corner moved to %v,%v", c.x0, c.y1)
	}
}

// A cell size nobody can honour is no picture, not a gigabyte of one: the
// rows and columns of a block are bounded, the cell in pixels is whatever
// -cell was handed or the terminal answered, and the product is what gets
// allocated.
func TestPixelPaintRefusesAnImageNobodyCanHold(t *testing.T) {
	th := mustTheme(t, theme.ClaudeDOT())
	d, err := RenderThemed(t.Context(), th, "digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paint(t.Context(), d, th.Face(), maxZoom); err != nil {
		t.Errorf("a picture at the zoom ceiling would not paint: %v", err)
	}
	if _, err := paint(t.Context(), d, th.Face(), 4000); err == nil {
		t.Error("a picture past the pixel ceiling was painted anyway")
	}
}

// A theme's fontname may name a font file — an absolute path, or a file
// name findfont locates — and then the picture is set in that one face.
// Anything else is Go Mono, silently, because the picture always draws: a
// family name is not a file, and a file that will not parse is not a face.
func TestPixelFontnameIsAFileOrGoMono(t *testing.T) {
	dir := t.TempDir()
	carried := filepath.Join(dir, "carried.ttf")
	if err := os.WriteFile(carried, gomono.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fontFile(carried); got != carried {
		t.Errorf("a font file the theme named resolved to %q, want the file", got)
	}
	if face(faceKey{fontKey{carried, false, false}, 12}) == nil {
		t.Error("a font file that parses gave no face")
	}
	bad := filepath.Join(dir, "bad.ttf")
	if err := os.WriteFile(bad, []byte("this is not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A font file under any other name is not one either. The rule is the
	// extension, and it is what keeps a family name off the box: those
	// three below come back empty here because nothing on this box is
	// called that, and a box with a monospace.ttf on it would answer
	// differently the moment the rule went.
	odd := filepath.Join(dir, "carried.font")
	if err := os.WriteFile(odd, gomono.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"monospace", "JetBrains Mono", "Courier", odd, filepath.Join(dir, "gone.ttf")} {
		if got := fontFile(name); got != "" {
			t.Errorf("%q was taken for a font file and resolved to %q", name, got)
		}
	}
	if f := face(faceKey{fontKey{bad, false, false}, 12}); f != nil {
		t.Error("a file that is not a font parsed as one")
	}
	// and the picture still draws: the painter falls to the face it carries
	p := &painter{s: 1, file: bad}
	if p.font(&pen{size: 12}) == nil {
		t.Error("a font file that would not parse left the picture with no face at all")
	}
}

// A fontname has to name a regular file small enough to read into memory,
// and the picture is set in Go Mono where it does not. That a path exists
// is not enough: os.Stat is as happy with a fifo, a character device and a
// three-gigabyte file as with a font, and the painter reads what it is
// handed, whole, with no deadline over it — measured, a theme naming
// /dev/zero never returned and was past ten gigabytes at fifteen seconds.
func TestPixelAFontnameIsAFileThatCanBeRead(t *testing.T) {
	dir := t.TempDir()
	// Sparse, so the box is asked for a name and a size and not for the
	// disk to go with them.
	huge := filepath.Join(dir, "huge.ttf")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFontBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	notAFile := filepath.Join(dir, "elsewhere.ttf")
	if err := os.Mkdir(notAFile, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{huge, notAFile} {
		if got := fontFile(name); got != "" {
			t.Errorf("%q was taken for a font file and resolved to %q", filepath.Base(name), got)
		}
		if readFont(name) != nil {
			t.Errorf("%q was read for a face", filepath.Base(name))
		}
	}
	// and the picture still draws, in the type this binary carries
	p := &painter{s: 1, file: huge}
	if p.font(&pen{size: 12}) == nil {
		t.Error("a fontname nobody can read left the picture with no face at all")
	}
}

// The picture is set in the monospace this binary carries, and a law that
// only asks whether some face exists would not know: swap Go Regular in and
// every label is set proportional in boxes graphviz measured in Courier,
// which is one of the four things go-graphviz's own renderer does wrong.
// Courier's advance is 0.600em and Go Mono's 0.602, so a run measured in
// the one and set in the other still stands in its box.
func TestPixelThePictureIsSetInGoMono(t *testing.T) {
	f := face(faceKey{fontKey{"", false, false}, 24})
	if f == nil {
		t.Fatal("Go Mono is not in this binary")
	}
	sf, err := opentype.Parse(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	mono, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: 24, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	var first fixed.Int26_6
	for i, r := range []rune{'i', 'M', 'W', '.', 'm'} {
		got, ok := f.GlyphAdvance(r)
		if !ok {
			t.Fatalf("the carried face has no %q", r)
		}
		if want, _ := mono.GlyphAdvance(r); got != want {
			t.Errorf("%q is set %v wide, and Go Mono sets it %v: the carried face is not Go Mono", r, got, want)
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Errorf("%q is %v wide against %v: the carried face is not a monospace", r, got, first)
		}
	}
}

// A span asks for its weight and slope in two ways, and both are
// graphviz's: an HTML `<b>` arrives as a font bit on the run, and a
// `fontname="Courier-Bold"` as the face's own name. Go Mono has the four
// faces to answer with.
func TestPixelABoldSpanIsSetInTheBoldFace(t *testing.T) {
	p := &painter{s: 1}
	plain := p.font(&pen{size: 12})
	bold := p.font(&pen{size: 12, fontchar: 1})
	italic := p.font(&pen{size: 12, fontchar: 2})
	named := p.font(&pen{size: 12, face: "Courier-Bold"})
	if plain == nil || bold == nil || italic == nil || named == nil {
		t.Fatal("a span was left with no face at all")
	}
	if bold == plain {
		t.Error("a bold span is set in the same face as the plain run")
	}
	if italic == plain || italic == bold {
		t.Error("an italic span is set in the plain or the bold face")
	}
	if named != bold {
		t.Error(`a fontname of "Courier-Bold" is not the bold face`)
	}
}

// ---------- edge labels on their lines ----------

// rewritten parses a source and puts its edge labels on their edges, for a
// look at the graph itself. The caller closes both.
func rewritten(t *testing.T, src string) (*graphviz.Graphviz, *cgraph.Graph) {
	t.Helper()
	th := mustTheme(t, theme.ClaudeDOT())
	g, err := graphviz.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil || graph == nil {
		t.Fatal("no graph:", err)
	}
	inlineEdgeLabels(graph, th, 0, true)
	return g, graph
}

// edgeAttr is one attribute of the edge between two named nodes, or "no
// such edge".
func edgeAttr(graph *cgraph.Graph, tail, head, attr string) string {
	for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
		for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
			if t, h := ends(e); t == tail && h == head {
				return e.GetStr(attr)
			}
		}
	}
	return "no such edge"
}

// A labelled graph is spaced as dot spaces one: the ranksep in force is
// halved — the model's, else dot's half inch — and every plain edge's
// minlen is doubled, so it spans a full rank gap and not the half a label
// node's rank left it. The halves of a labelled edge keep the model's
// minlen, the label at their midpoint. A graph with no labels is not
// touched.
func TestPixelLabelsSpaceAsDotDoes(t *testing.T) {
	g, graph := rewritten(t, "digraph { ranksep=\"1 equally\"; a -> b [label=x, minlen=3]; b -> c; c -> d [minlen=3] }")
	defer g.Close()
	defer graph.Close()
	if rs := graph.GetStr("ranksep"); rs != "0.5 equally" {
		t.Errorf("ranksep is %q, want the model's halved", rs)
	}
	for _, c := range []struct{ tail, head, want string }{
		{"b", "c", "2"}, {"c", "d", "6"},
		{"a", labelNodePrefix + "0", "3"}, {labelNodePrefix + "0", "b", "3"},
	} {
		if got := edgeAttr(graph, c.tail, c.head, "minlen"); got != c.want {
			t.Errorf("%s -> %s: minlen %q, want %q", c.tail, c.head, got, c.want)
		}
	}
	g2, bare := rewritten(t, "digraph { a -> b [label=x]; b -> c }")
	defer g2.Close()
	defer bare.Close()
	if rs := bare.GetStr("ranksep"); rs != "0.25" {
		t.Errorf("ranksep is %q with none declared, want dot's half inch halved", rs)
	}
	g3, plain := rewritten(t, "digraph { ranksep=1; a -> b; b -> c }")
	defer g3.Close()
	defer plain.Close()
	if rs, ml := plain.GetStr("ranksep"), edgeAttr(plain, "a", "b", "minlen"); rs != "1" || ml != "" {
		t.Errorf("an unlabelled graph was respaced: ranksep %q, minlen %q", rs, ml)
	}
}

// A labelled edge is drawn as two edges through a node carrying the label,
// each half in the edge's own paint, and a `dir=both` edge keeps a head at
// each end: the back arrow on the first half, the forward on the second.
func TestPixelEdgeLabelSitsOnItsLine(t *testing.T) {
	th := mustTheme(t, theme.ClaudeDOT())
	d, err := RenderThemed(t.Context(), th, `digraph { a -> b [label="x", color=red, dir=both] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if e := d.Between("a", "b"); e != nil {
		t.Errorf("the labelled edge is still drawn whole:\n%+v", e.Draw)
	}
	halves := map[string]*layout.Edge{
		"first":  d.Between("a", labelNodePrefix+"0"),
		"second": d.Between(labelNodePrefix+"0", "b"),
	}
	for name, half := range halves {
		if half == nil {
			t.Fatalf("no %s half of the labelled edge", name)
		}
		if !inked(half.Draw, "c", "#ff0000") {
			t.Errorf("the %s half lost the edge's colour:\n%+v", name, half.Draw)
		}
		if n := heads(half); n != 1 {
			t.Errorf("the %s half has %d heads, want one", name, n)
		}
	}
	label := d.Object(labelNodePrefix + "0")
	if label == nil {
		t.Fatal("the label is not a node on the line")
	}
	if !set(label.LDraw, "x") || !inked(label.LDraw, "c", th.Edge["fontcolor"]) {
		t.Errorf("the label is not on the line in the edge label colour:\n%+v", label.LDraw)
	}
	if inked(label.Draw, "C", th.Node["fillcolor"]) {
		t.Errorf("the label node was filled like a node:\n%+v", label.Draw)
	}
}

// An undirected labelled edge grows no heads.
func TestPixelUndirectedLabelGrowsNoHeads(t *testing.T) {
	d, err := RenderThemed(t.Context(), mustTheme(t, theme.ClaudeDOT()), `graph { a -- b [label="x"] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ends := range [][2]string{{"a", labelNodePrefix + "0"}, {labelNodePrefix + "0", "b"}} {
		half := d.Between(ends[0], ends[1])
		if half == nil {
			t.Errorf("no half between %s and %s", ends[0], ends[1])
			continue
		}
		if n := heads(half); n != 0 {
			t.Errorf("an undirected half grew %d heads", n)
		}
	}
}

// A labelled edge inside a cluster keeps its label in the cluster, or dot
// would route the edge out of the cluster and back to visit it. A cluster
// says which nodes are its own, and the label node has to be one of them.
func TestPixelEdgeLabelStaysInItsCluster(t *testing.T) {
	d, err := RenderThemed(t.Context(), mustTheme(t, theme.ClaudeDOT()), `digraph { subgraph cluster_c { a -> b [label="x"] } c -> a }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	cluster := d.Object("cluster_c")
	if cluster == nil {
		t.Fatal("the drawing has no cluster")
	}
	inside := -1
	outside := -1
	for i := range d.Objects {
		switch d.Objects[i].Name {
		case labelNodePrefix + "0":
			inside = i
		case "c":
			outside = i
		}
	}
	if inside < 0 || outside < 0 {
		t.Fatalf("missing the label node or the node outside: %d, %d", inside, outside)
	}
	if !slices.Contains(cluster.Nodes, inside) {
		t.Errorf("the label node is not one of the cluster's own: %v", cluster.Nodes)
	}
	if slices.Contains(cluster.Nodes, outside) {
		t.Errorf("the node outside the cluster was taken into it: %v", cluster.Nodes)
	}
}

// The label of an edge that closes a cycle sits between the edge's ends,
// and the arrow still points where the model pointed it.
func TestPixelLabelOnABackEdgeSitsBetweenItsEnds(t *testing.T) {
	d, err := RenderThemed(t.Context(), mustTheme(t, theme.ClaudeDOT()), `digraph { a -> b -> c; c -> a [label="no"] }`, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a, c, l := d.Object("a"), d.Object("c"), d.Object(labelNodePrefix+"0")
	if a == nil || c == nil || l == nil {
		t.Fatal("the drawing is missing a node")
	}
	ya, yc, yl := layout.TextY(a.LDraw), layout.TextY(c.LDraw), layout.TextY(l.LDraw)
	if ya == 0 || yc == 0 || yl == 0 {
		t.Fatalf("missing a label: a=%v c=%v label=%v", ya, yc, yl)
	}
	if !(min(ya, yc) < yl && yl < max(ya, yc)) {
		t.Errorf("the label is at %v, not between its ends at %v and %v", yl, ya, yc)
	}
	// The chain runs a -> label -> c, and the head is on the half that
	// touches a: drawn as a back arrow on that half.
	first, second := d.Between("a", labelNodePrefix+"0"), d.Between(labelNodePrefix+"0", "c")
	if first == nil || second == nil {
		t.Fatal("the back edge was not chained the other way round")
	}
	if heads(first) != 1 || heads(second) != 0 {
		t.Errorf("the arrow moved: half at a has %d heads, half at c has %d", heads(first), heads(second))
	}
}

// Inlining labels doubles the ranks, so ranksep is halved as dot does for
// its own label nodes — whichever is in force: the model's, else the
// theme's, else dot's half inch. A model writing dot's default draws the
// same chain as one writing nothing, and a theme's inch is a half.
func TestPixelLabelsHalveRanksepAsDotDoes(t *testing.T) {
	claude := mustTheme(t, theme.ClaudeDOT())
	height := func(th *theme.Theme, src string) float64 {
		d, err := RenderThemed(t.Context(), th, src, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		return d.H
	}
	bare := height(claude, `digraph { a -> b [label="x"] }`)
	if dflt := height(claude, `digraph { ranksep=0.5; a -> b [label="x"] }`); dflt != bare {
		t.Errorf("a labelled chain is %vpt with dot's default written and %vpt with none; the model's ranksep was not halved", dflt, bare)
	}
	if model := height(claude, `digraph { ranksep=1; a -> b [label="x"] }`); !(model > bare) {
		t.Errorf("the model's ranksep was overridden: %vpt at 1, %vpt at the default", model, bare)
	}
	if themed := height(mustTheme(t, `graph [ranksep=1]`), `digraph { a -> b [label="x"] }`); !(themed > bare) {
		t.Errorf("the theme's ranksep was overridden: %vpt themed, %vpt at the default", themed, bare)
	}
}

// A picture wider than the window is laid out top-down as well, as the
// glyph rungs do — rows scroll, columns run out — and the orientation that
// keeps more of its type is the picture: top-down when that fits at the
// cell's own type and as written does not, top-down when both are squeezed
// and it is squeezed less, and as written when top-down is too tall for
// the ceiling — which is the picture that was there before this law. The
// cut says which way it went, and the zoom it chose is read where it is
// chosen, with nothing painted.
func TestPixelCutFlipsTopDownBeforeSqueezing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	th := mustTheme(t, theme.ClaudeDOT())
	geom := term.Geom{CellW: 10, CellH: 24}
	chain := func(n int) string {
		var b strings.Builder
		b.WriteString("digraph { rankdir=LR; ")
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteString(" -> ")
			}
			fmt.Fprintf(&b, "step_number_%02d", i)
		}
		b.WriteString(" }")
		return b.String()
	}

	wide := chain(6)
	_, p, zoom, err := cut(t.Context(), th, wide, 100, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != cgraph.TBRank || p.Cols > 100 {
		t.Errorf("a chain too wide for 100 columns was cut %d wide, laid out %q; want top-down within the width", p.Cols, p.Rankdir)
	}
	if zoom < 1 {
		t.Errorf("flipped top-down and still squeezed: zoom %v", zoom)
	}
	_, p, zoom, err = cut(t.Context(), th, wide, len(rowColumnDiacritics), geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != "" || zoom < 1 {
		t.Errorf("a chain with room to spare was laid out %q at zoom %v; want as written at the cell's own type", p.Rankdir, zoom)
	}

	_, p, zoom, err = cut(t.Context(), th, wide, 12, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != cgraph.TBRank || p.Cols != 12 || zoom >= 1 {
		t.Errorf("a chain too wide either way was laid out %q, %d wide at zoom %v; want top-down, squeezed less", p.Rankdir, p.Cols, zoom)
	}

	tall := chain(60)
	_, p, zoom, err = cut(t.Context(), th, tall, 100, geom)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rankdir != "" || p.Cols != 100 || zoom >= 1 {
		t.Errorf("a chain too tall top-down was laid out %q, %d wide at zoom %v; want as written, squeezed into 100 columns", p.Rankdir, p.Cols, zoom)
	}
}
