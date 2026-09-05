// pixel.go — the same drawing, in real pixels, where the terminal can.
//
// The cells and the sub-cell strokes are the floor: they need nothing but
// this binary and draw in any terminal that can show a `┌`. This is the
// ceiling, and it is optional in the strongest sense — every stage fails
// open to the glyph drawing, so a missing rasteriser, a slow one, a
// terminal that is not kitty, or a graph cairo chokes on all end in the
// picture that was already there. No error from here reaches the screen.
//
// Two things have to be true. The terminal has to be kitty, because the
// graphics protocol's Unicode placeholders are the one way an image can
// live in cells and therefore scroll, wrap and copy exactly like text. And
// something on the PATH has to turn SVG into pixels. graphviz's own PNG
// backend is not that something: goccy/go-graphviz carries graphviz's real
// SVG writer, which is exact, and its own rasteriser, which silently drops
// any edge stroke past hairline and loses them outright at dpi=192. So the
// wasm writes SVG and cairo — rsvg-convert, else magick — makes the pixels.

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// ---------- capability ----------

// raster is the rasteriser this machine actually has: a name, and the one
// call it can make. A func rather than a command line so a law can stand in
// a stub and read the zoom it was asked for, with no rasteriser anywhere in
// the loop.
type raster struct {
	name string
	run  func(svg []byte, zoom float64) ([]byte, error)
}

// probeRaster answers whether pixels are possible here: the terminal is
// kitty and a rasteriser exists.
func probeRaster() *raster {
	if !strings.Contains(os.Getenv("TERM"), "kitty") && os.Getenv("KITTY_WINDOW_ID") == "" {
		return nil
	}
	return findRaster()
}

// findRaster is the rasteriser on the PATH, whatever the terminal: for a
// picture that is going to a file rather than a screen.
func findRaster() *raster {
	if p, err := exec.LookPath("rsvg-convert"); err == nil {
		return &raster{name: "rsvg-convert", run: func(svg []byte, zoom float64) ([]byte, error) {
			return rasterExec(p, svg, "--zoom", strconv.FormatFloat(zoom, 'f', 4, 64))
		}}
	}
	if p, err := exec.LookPath("magick"); err == nil {
		// magick has no --zoom: it rasterises SVG at a density, and 96dpi is
		// the density rsvg renders at unzoomed, so the same number means the
		// same picture on either.
		return &raster{name: "magick", run: func(svg []byte, zoom float64) ([]byte, error) {
			return rasterExec(p, svg, "-background", "none",
				"-density", strconv.FormatFloat(96*zoom, 'f', 2, 64), "svg:-", "png:-")
		}}
	}
	return nil
}

// A rasteriser is another process on somebody else's machine: it can hang,
// it can hand back a gigabyte, it can be a shell script. Bound both ends and
// read anything outside them as no picture at all.
const (
	rasterTimeout = 2 * time.Second
	rasterMaxPNG  = 4 << 20
	// A tiny graph in a tall region would otherwise be blown up without
	// limit; past this the picture is not better, only heavier.
	rasterMaxZoom = 8.0
)

func rasterExec(bin string, svg []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rasterTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(svg)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.Len() == 0 || out.Len() > rasterMaxPNG {
		return nil, errors.New("rasteriser output out of bounds")
	}
	return out.Bytes(), nil
}

// ---------- SVG, themed ----------

// The theme goes on through cgraph, on the parsed graph, and only where the
// model left an attribute unset: a node it painted keeps its paint, and
// around that paint graphviz's own defaults apply — `fillcolor=pink` gets
// black text, as `dot` would give it. A shape it asked for is the shape it
// gets, records and diamonds included. Nothing is spliced into the source
// text: that text is the model's, it arrives split at boundaries nobody
// chose, and it is the readable fallback — a theme that edits it can damage
// the one thing that always has to keep working.
//
// What the theme says is in theme.go; Claude Code's stands the picture on
// the terminal's own ground. Measured on a translucent kitty over a
// wallpaper: an opaque slab was the one thing in the picture that said
// "pasted in", and it was the first thing a reader saw.

// Type is measured in Courier and set in the theme's face. The wasm has no
// fontconfig: graphviz measures a label from tables built into it, and the
// tables know Courier's advance exactly — 0.6em, which is every terminal
// monospace's advance too. Any other name, "monospace" included, falls back
// to Times metrics and the labels run out of their boxes; measured, "API
// Server 1" overran its box by two glyphs. So the layout is done in Courier
// and on the way out the SVG is told the face, which fontconfig resolves —
// "monospace" to the one the terminal itself is showing. Neither the model
// nor the theme gets to choose the layout font: a label measured in one
// face and set in another is the overrun again, so fontname is the one
// attribute always written.
const (
	pxLayoutFont = "Courier"
	pxAdvance    = 0.6 // em per glyph: Courier's, and every terminal's
)

// pxFontPt is the type size that puts one glyph in one cell. The picture is
// cut to whole columns at a zoom of one, so a label set at this size is the
// terminal's own text size and a node reads as text that grew a border.
// Zero when the cell is unknown, and graphviz keeps its 14pt.
func pxFontPt(cellW int) float64 {
	if cellW <= 0 {
		return 0
	}
	return float64(cellW) / pxAdvance / pxPerPt
}

// renderThemedSVG lays the source out and writes graphviz's SVG for it.
//
// Node sizes are graphviz's own here, unlike the cell renderer's, which
// forces every box to its label's width in cells. There the cells do the
// typography and graphviz only places boxes; here graphviz is drawing the
// picture, so it gets to measure its own type.
//
// force overrides the orientation the source asked for; empty leaves the
// author's choice alone.
func renderThemedSVG(th *theme, src string, fontPt float64, force cgraph.RankDir) ([]byte, error) {
	var svg []byte
	err := door(src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
		if force != "" {
			graph.SetRankDir(force)
		}
		inlineEdgeLabels(graph, th, fontPt, isDigraph(src))

		type getter = func(string) string
		type setter = func(string, string, string) error
		// unset writes an attribute only where the source left it empty. An
		// attribute the source declared at `node [...]` reads as set on every
		// node, which is what the model meant by declaring it there.
		unset := func(get getter, set setter, key, val string) {
			if val != "" && get(key) == "" {
				set(key, val, "")
			}
		}
		// apply is a theme's declarations, less the ones that are rules.
		apply := func(get getter, set setter, decl map[string]string, rules ...string) {
			for k, v := range decl {
				if !slices.Contains(rules, k) {
					unset(get, set, k, v)
				}
			}
		}
		size := ""
		if fontPt > 0 {
			size = strconv.FormatFloat(fontPt, 'f', 2, 64)
		}
		// typeset is the two type rules: always Courier, and the theme's size
		// where it has one, else the cell's.
		typeset := func(get getter, set setter, decl map[string]string) {
			set("fontname", pxLayoutFont, "")
			if fs := decl["fontsize"]; fs != "" {
				unset(get, set, "fontsize", fs)
			} else {
				unset(get, set, "fontsize", size)
			}
		}
		for n, _ := graph.FirstNode(); n != nil; n, _ = graph.NextNode(n) {
			apply(n.GetStr, n.SafeSet, th.Node, "fillcolor", "style", "fontcolor", "fontname", "fontsize")
			// The fill rule. A node the model filled, or styled filled, is the
			// model's: its fill, its style and its text colour stay as graphviz
			// would give them. Any other node takes the theme's fill, with
			// "filled" added to whichever style it has, and the theme's text.
			style := n.GetStr("style")
			modelFilled := n.GetStr("fillcolor") != "" || strings.Contains(style, "filled")
			switch fill := th.Node["fillcolor"]; {
			case fill == "":
				unset(n.GetStr, n.SafeSet, "style", th.Node["style"])
				unset(n.GetStr, n.SafeSet, "fontcolor", th.Node["fontcolor"])
			case !modelFilled:
				if style == "" {
					style = th.Node["style"]
				}
				if !strings.Contains(style, "filled") {
					style = strings.TrimPrefix(style+",filled", ",")
				}
				n.SafeSet("style", style, "")
				n.SafeSet("fillcolor", fill, "")
				unset(n.GetStr, n.SafeSet, "fontcolor", th.Node["fontcolor"])
			}
			typeset(n.GetStr, n.SafeSet, th.Node)
			for e, _ := graph.FirstOut(n); e != nil; e, _ = graph.NextOut(e) {
				apply(e.GetStr, e.SafeSet, th.Edge, "fontname", "fontsize")
				typeset(e.GetStr, e.SafeSet, th.Edge)
			}
		}
		// Clusters before the root. A subgraph answers GetStr with the root's
		// value for anything it never set itself, but graphviz paints it from
		// its own record, which is empty — measured as a black serif "kitty"
		// over an otherwise themed cluster. Read the clusters while the root is
		// still the model's, then theme the root.
		var clusters func(*cgraph.Graph)
		clusters = func(sg *cgraph.Graph) {
			for c, _ := sg.FirstSubGraph(); c != nil; c, _ = c.NextSubGraph() {
				apply(c.GetStr, c.SafeSet, th.Graph, "fontname", "fontsize")
				typeset(c.GetStr, c.SafeSet, th.Graph)
				clusters(c)
			}
		}
		clusters(graph)
		apply(graph.GetStr, graph.SafeSet, th.Graph, "fontname", "fontsize")
		typeset(graph.GetStr, graph.SafeSet, th.Graph)

		var buf bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
			return err
		}
		// graphviz writes Courier as a family with its generic behind it.
		svg = bytes.ReplaceAll(buf.Bytes(),
			[]byte(`font-family="`+pxLayoutFont+`,monospace"`), []byte(`font-family="`+th.face()+`"`))
		return nil
	})
	return svg, err
}

// ---------- pixels ----------

// An SVG length is in points and a rasteriser renders a point at 96dpi.
// Measured, not assumed: the reference 672pt × 121pt render comes back
// 896 × 162 px at --zoom 1, and 672 × 96/72 is 896.
const pxPerPt = 96.0 / 72.0

var svgSizeRe = regexp.MustCompile(`width="([0-9.]+)pt"\s+height="([0-9.]+)pt"`)

// svgSize reads the size graphviz wrote into its own output. The layout
// already answered how big this picture is; measuring it again from the
// geometry inside would be a second guess at an answer already given.
func svgSize(svg []byte) (float64, float64, error) {
	head := svg
	if len(head) > 4096 {
		head = head[:4096]
	}
	m := svgSizeRe.FindSubmatch(head)
	if m == nil {
		return 0, 0, errors.New("svg carries no size")
	}
	w, h := atof(string(m[1])), atof(string(m[2]))
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("svg size is not a size")
	}
	return w, h, nil
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// pngSize reads the dimensions out of the IHDR chunk, which is the first
// chunk of every PNG and holds them in its first eight bytes. Decoding the
// whole image to learn two numbers would cost the pixels twice.
func pngSize(png []byte) (int, int, error) {
	if len(png) < 24 || !bytes.Equal(png[:8], pngMagic) || string(png[12:16]) != "IHDR" {
		return 0, 0, errors.New("not a png")
	}
	w := int(binary.BigEndian.Uint32(png[16:20]))
	h := int(binary.BigEndian.Uint32(png[20:24]))
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("png has no size")
	}
	return w, h, nil
}

// ---------- names ----------

// fnv1a32 hashes a cut into a name. Names here are derived, never minted:
// kitty's image ids are a namespace shared with every program on the same
// terminal, and a counter beside a screen that cannot check it is the bug
// that drew the first diagram of every reply as the last one. See
// hookImageID.
func fnv1a32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// ---------- placeholders ----------

// The placeholder character. A cell holding it, coloured with an image id,
// tells kitty to draw that image's pixels there — and it is ordinary text
// to everything else, which is the entire reason a picture can ride through
// CC's display wire.
const placeholderRune = '\U0010EEEE'

// rowColumnDiacritics carries the row and column of a placeholder cell as
// combining marks. Not guessed: this is kitty's own rowcolumn-diacritics.txt
// (297 entries), fetched 2026-08-27 from
// https://sw.kovidgoyal.net/kitty/graphics-protocol/ — combining class 230
// characters from Unicode 6.0.0 with no decomposition mappings, so no
// normalisation anywhere along the wire can fuse one into its base
// character. Index is the number; the first is U+0305 for 0.
var rowColumnDiacritics = [...]rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243,
	0x1D244,
}
