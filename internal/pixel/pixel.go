// pixel.go — the same drawing, in real pixels, where the terminal can.
//
// The cells and the sub-cell strokes are the floor: they need nothing but
// this binary and draw in any terminal that can show a `┌`. This is the
// ceiling, and it is optional in the strongest sense — every stage fails
// open to the glyph drawing, so a terminal that does not draw the
// placeholder cells, a graph too big for the block it was given, or a
// deadline already spent all end in the picture that was already there. No
// error from here reaches the screen.
//
// One thing has to be true, and it is about the terminal alone: it has to
// draw kitty's Unicode placeholders — kitty or ghostty — because those
// cells are the one way an image can live in cells and therefore scroll,
// wrap and copy exactly like text. Nothing else is asked of the box. The
// wasm writes the layout as json, which is xdot — every polygon, bezier,
// ellipse and text anchor graphviz would have painted — and paint.go
// paints it, in this process, in type this binary carries.
//
// graphviz's own PNG backend is still not the answer, and the reason is
// not the dependency: measured, goccy/go-graphviz's renderer silently
// drops an edge stroke past hairline and loses them outright at dpi=192,
// fills a shape without stroking the outline xdot says to stroke, and sets
// type in Go Regular, which is proportional, while the layout was measured
// in Courier. What it shares with this file is the rasteriser underneath;
// what it does with the drawing is the part that had to be written here.

package pixel

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/theme"
)

// ---------- capability ----------

// Placeholders says whether a terminal draws kitty's Unicode placeholder
// cells, which are the one way a picture can live in cells and therefore
// scroll, wrap and copy exactly like text. kitty invented them; ghostty
// implements them, measured on the rig 2026-09-06 — streaming, complete and
// scrolled all hold, inside tmux and out, aligned to the same rows kitty
// puts them on. Nobody else measured that night drew them: wezterm,
// konsole and alacritty each printed 576 tofu boxes where the picture was,
// so this is a gate and not a hint. A terminal not named here gets glyphs,
// which every font carries.
//
// The name is the terminal's, resolved: term.Name answers with the terminal
// behind tmux rather than tmux's own TERM, so this predicate never has to
// know a multiplexer exists. It is handed in rather than read here, because
// under tmux reading it is a fork and a socket round-trip: the run asks
// once and every predicate below it reads that one answer.
func Placeholders(name string) bool {
	return strings.Contains(name, "kitty") || strings.Contains(name, "ghostty")
}

// ---------- the drawing, themed ----------

// The theme goes on through cgraph, on the parsed graph, and only where the
// model left an attribute unset: a node it painted keeps its paint, and
// around that paint graphviz's own defaults apply — `fillcolor=pink` gets
// black text, as `dot` would give it. A shape it asked for is the shape it
// gets, records and diamonds included. Nothing is spliced into the source
// text: that text is the model's, it arrives split at boundaries nobody
// chose, and it is the readable fallback — a theme that edits it can damage
// the one thing that always has to keep working.
//
// What the theme says is in internal/theme; Claude Code's stands the picture
// on the terminal's own ground. Measured on a translucent kitty over a
// wallpaper: an opaque slab was the one thing in the picture that said
// "pasted in", and it was the first thing a reader saw.

// Type is measured in Courier, always. The wasm has no font system:
// graphviz measures a label from tables built into it, and the tables know
// Courier's advance exactly — 0.6em, which is every terminal monospace's
// advance too. Any other name, "monospace" included, falls back to Times
// metrics and the labels run out of their boxes; measured, "API Server 1"
// overran its box by two glyphs. So fontname is the one attribute always
// written, over the model and over the theme alike: a label measured in one
// face and set in another is that overrun again. What the picture is
// finally set in is paint.go's answer, and Go Mono's advance is 0.602em
// against Courier's 0.600.
const (
	pxLayoutFont = "Courier"
	pxAdvance    = 0.6 // em per glyph: Courier's, and every terminal's
)

// FontPt is the type size that puts one glyph in one cell. The picture is
// cut to whole columns at a zoom of one, so a label set at this size is the
// terminal's own text size and a node reads as text that grew a border.
// Zero when the cell is unknown, and graphviz keeps its 14pt.
func FontPt(cellW int) float64 {
	if cellW <= 0 {
		return 0
	}
	return float64(cellW) / pxAdvance / pxPerPt
}

// RenderThemed lays the source out in a theme and reads back what graphviz
// would have drawn.
//
// Node sizes are graphviz's own here, unlike the cell renderer's, which
// forces every box to its label's width in cells. There the cells do the
// typography and graphviz only places boxes; here graphviz is drawing the
// picture, so it gets to measure its own type.
//
// force overrides the orientation the source asked for; empty leaves the
// author's choice alone.
func RenderThemed(ctx context.Context, th *theme.Theme, src string, fontPt float64, force cgraph.RankDir) (*layout.Drawing, error) {
	var d *layout.Drawing
	err := layout.Door(ctx, src, func(ctx context.Context, g *graphviz.Graphviz, graph *cgraph.Graph) error {
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

		var err error
		d, err = layout.Render(ctx, g, graph)
		return err
	})
	return d, err
}

// ---------- pixels ----------

// A drawing is measured in points, and a point is 96dpi's ninety-sixth of
// an inch: the size the picture has always been rendered at. Measured, not
// assumed — the reference 672pt × 121pt picture came back 896 × 162 px at
// zoom 1, and 672 × 96/72 is 896.
const pxPerPt = 96.0 / 72.0

// ---------- names ----------

// fnv1a32 hashes a cut into a name. Names here are derived, never minted:
// kitty's image ids are a namespace shared with every program on the same
// terminal, and a counter beside a screen that cannot check it is the bug
// that drew the first diagram of every reply as the last one. See
// ImageID.
func fnv1a32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// ---------- placeholders ----------

// PlaceholderRune is the placeholder character. A cell holding it, coloured
// with an image id, tells kitty to draw that image's pixels there — and it
// is ordinary text to everything else, which is the entire reason a picture
// can ride through CC's display wire.
const PlaceholderRune = '\U0010EEEE'

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
