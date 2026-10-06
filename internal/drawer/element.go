// element.go — the hooks module's door: one fence's source in, its drawing
// out as data, for a ui.render hook to hand Claude Code as its own
// elements.
//
// The MessageDisplay hook is asked about one screen only: the live turn of
// the session's main thread. A fork's view, a subagent's, a transcript
// replayed by --resume and a background job reattached are drawn from the
// transcript, and no display hook hears of any of them — measured on
// Claude Code 2.1.283, where every one of them showed the fence as source.
// A function hook on ui.render is asked about every assistant text block
// on every one of those screens, and it answers with elements rather than
// text: an Image is a picture Claude Code places, scrolls and clips itself,
// and Text is glyphs in colour.
//
// So this door hands back the drawing drawBlock makes, as data, one JSON
// object on stdout:
//
//	{"kind":"image","png":"…","columns":40,"rows":9,"alt":"…"}
//	{"kind":"text","lines":[[{"t":"╭──","c":"#d77757","d":true}, …], …]}
//	{"kind":"none"}
//
// text with "notice":true is a notice over the source, which a fence the
// reply never closed does not get: that one is a reply cut off, not a
// graph somebody got wrong. none is a fence that stays as it arrived. The
// picture travels as bytes, not as a file name: Claude Code draws a named
// file only where the terminal reads files on this machine, which a
// terminal across ssh does not, and bytes everywhere it draws a picture at
// all.
//
// The hooks module runs this binary as a child of claude itself — measured
// on 2.1.283, its parent's fd 0 and 1 are the session's tty — so the
// window and the cell size are read exactly as the display hook reads
// them. The width is not: the module hands over the columns ui.render
// reports, which are the columns of the screen being drawn, and a fork's
// view is not always the size the main thread last saw. It hands over the
// fence's indent besides, and the drawing is as wide as the display hook
// would draw that fence: the body's width less the indent it stands in.
//
// The deadline is the process's own. A layout under way runs to its end
// whatever the context says (layout.Door), and a large graph's runs for
// tens of seconds; this door answers none at the deadline instead and
// returns, and the process ending with the answer is what stops the
// layout.

package drawer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sureffi/drawer/internal/pixel"
)

// element is what one fence becomes on a render hook's screen.
type element struct {
	Kind    string   `json:"kind"`              // image, text or none
	PNG     string   `json:"png,omitempty"`     // image: the picture, base64
	Columns int      `json:"columns,omitempty"` // image: its box, in cells
	Rows    int      `json:"rows,omitempty"`
	Alt     string   `json:"alt,omitempty"`    // image: what it says where it cannot be a picture
	Lines   [][]span `json:"lines,omitempty"`  // text: the rows, as runs of one style
	Notice  bool     `json:"notice,omitempty"` // text: the rows say why there is no drawing, over the source
}

// span is a run of one style in a row of text.
type span struct {
	Text  string `json:"t"`
	Color string `json:"c,omitempty"` // #rrggbb; empty is the terminal's own
	Dim   bool   `json:"d,omitempty"`
}

// Claude Code's own bounds on an Image, read from the 2.1.283 type
// declarations: 1 to 255 cells on each axis, at most 2 MiB of picture.
const (
	imageMaxCells = 255
	imageMaxBytes = 2 << 20
)

// sourceMax bounds what the door reads. A fence is a few hundred bytes; a
// megabyte is a model that has lost the plot, and the layout's deadline
// is not the place to find that out.
const sourceMax = 1 << 20

// runElement is the whole of the -element door: the source on stdin, one
// element on stdout. cols is the screen's width in cells, as ui.render
// reports it; zero or less reads the window, as the display hook does.
// indent is the columns the fence stands in; less than zero is none.
func (r run) runElement(ctx context.Context, cols, indent int) int {
	b, err := io.ReadAll(io.LimitReader(os.Stdin, sourceMax))
	if err != nil || !utf8.Valid(b) {
		json.NewEncoder(os.Stdout).Encode(element{Kind: "none"})
		return 0
	}
	probed, _ := r.probe()
	if cols <= 0 {
		cols = probed
	}
	width := hookWidth(cols) - max(indent, 0)
	el := answerBy(ctx, func() element { return r.element(ctx, string(b), width) })
	json.NewEncoder(os.Stdout).Encode(el)
	return 0
}

// answerBy is the drawing, or none where the deadline comes first. The
// drawing goes on in the background where it is late, so the caller ends
// the process with the answer rather than starting another layout beside
// it: two graphviz instances at once are a fatal error (layout.Door).
func answerBy(ctx context.Context, draw func() element) element {
	done := make(chan element, 1)
	go func() { done <- draw() }()
	select {
	case el := <-done:
		return el
	case <-ctx.Done():
		select {
		case el := <-done:
			return el
		default:
			return element{Kind: "none"}
		}
	}
}

// element draws one fence for a render hook: the picture where Claude Code
// will draw one, the glyphs everywhere else, and a notice over the source
// where neither fits.
func (r run) element(ctx context.Context, src string, width int) element {
	if width <= 0 {
		width = 100
	}
	if r.elementRung() == rungPixels {
		if el, ok := r.image(ctx, src, min(width, imageMaxCells)); ok {
			return el
		}
	}
	if rows, isNotice := drawRows(ctx, src, width); rows != nil {
		return element{Kind: "text", Lines: spansOf(rows), Notice: isNotice}
	}
	return element{Kind: "none"}
}

// elementRung is pickRung for a picture Claude Code draws rather than one
// this binary sends. The terminal still has to draw placeholders, and the
// cut still wants the cell's size in pixels; what changes is who decides
// whether a picture goes out at all, and that is imagesDraw.
func (r run) elementRung() rung {
	if r.rung == rungCells || !imagesDraw(r.term) {
		return rungCells
	}
	if r.rung == rungPixels || r.geom.OK() {
		return rungPixels
	}
	return rungCells
}

// imagesDraw is Claude Code's own rule for whether an Image is pixels,
// read from the 2.1.283 binary: always, where CLAUDE_CODE_FORCE_TERMINAL_IMAGES
// is set; never in a background session (CLAUDE_CODE_SESSION_KIND=bg, the
// worker a job runs in) and never inside tmux or screen; elsewhere, where
// the terminal answers a graphics query and names itself kitty (0.28 on)
// or ghostty. An Image it will not draw is its alt, one dim line, and the
// graph is gone — so where this says no, the drawing is glyphs.
//
// The query is Claude Code's to ask and its answer is not handed to a
// hook. The terminal's name stands in for it, as it does for the display
// hook's rung: the same list, pixel.Placeholders.
func imagesDraw(name string) bool {
	if os.Getenv("CLAUDE_CODE_FORCE_TERMINAL_IMAGES") != "" {
		return true
	}
	if os.Getenv("CLAUDE_CODE_SESSION_KIND") == "bg" || os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	return pixel.Placeholders(name)
}

// image is the pixels rung as an element: the same cut the display hook
// sends down the tty, handed back as bytes for Claude Code to send.
func (r run) image(ctx context.Context, src string, width int) (element, bool) {
	png, p, err := pixel.Cut(ctx, r.theme.get(ctx), src, width, r.geom)
	if err != nil || len(png) > imageMaxBytes || p.Cols < 1 || p.Rows < 1 || p.Cols > imageMaxCells || p.Rows > imageMaxCells {
		return element{}, false
	}
	return element{
		Kind:    "image",
		PNG:     base64.StdEncoding.EncodeToString(png),
		Columns: p.Cols,
		Rows:    p.Rows,
		Alt:     altOf(src),
	}, true
}

// altOf is what a picture says where it cannot be one, and to a screen
// reader: the source, on one line, cut where a line stops being read, with
// no control character in it.
func altOf(src string) string {
	words := strings.Fields(src)
	kept := words[:0]
	for _, w := range words {
		if w = printing(w); w != "" {
			kept = append(kept, w)
		}
	}
	s := strings.Join(kept, " ")
	if s == "" {
		return "graph"
	}
	const most = 200
	if utf8.RuneCountInString(s) <= most {
		return s
	}
	return string([]rune(s)[:most-1]) + "…"
}

// spansOf reads rows of glyphs back into runs of one style. The rows are
// the cells rung's, and its wire writes four escapes and no others —
// SGR 39 and 38;2;r;g;b for the pen, 2 and 22 for the dim that structure
// nobody coloured is drawn in — so those four are read here, a reset
// besides, and anything else is passed over rather than printed. A
// control character a label or a notice's quoted source carries is
// dropped from the run: Claude Code refuses a whole tree whose Text holds
// one.
func spansOf(rows []string) [][]span {
	out := make([][]span, 0, len(rows))
	var cur span
	for _, row := range rows {
		line := []span{} // a row with no glyphs is [], never null: the module maps every row
		var b strings.Builder
		flush := func() {
			if t := printing(b.String()); t != "" {
				line = append(line, span{Text: t, Color: cur.Color, Dim: cur.Dim})
			}
			b.Reset()
		}
		for i := 0; i < len(row); i++ {
			if row[i] != 0x1b || i+1 >= len(row) || row[i+1] != '[' {
				b.WriteByte(row[i])
				continue
			}
			j := i + 2
			for j < len(row) && (row[j] < 0x40 || row[j] > 0x7e) {
				j++
			}
			if j < len(row) && row[j] == 'm' {
				flush()
				cur = sgr(cur, row[i+2:j])
			}
			i = j
		}
		flush()
		out = append(out, foldAir(line))
	}
	return out
}

// printing is s without its control characters: C0 but the tab, DEL and
// C1. They are dropped rather than shown as U+FFFD because the cells rung
// lays each one as a zero-width mark on the glyph before it, so a row
// without them is still as wide as its box.
func printing(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// foldAir joins runs of one style, and gives a run of spaces to its
// neighbour whatever its style: a space shows no pen and no dim, and a
// run for one is an element spent on nothing.
func foldAir(line []span) []span {
	out := line[:0]
	for i, s := range line {
		if strings.Trim(s.Text, " ") == "" {
			if n := len(out); n > 0 {
				out[n-1].Text += s.Text
				continue
			}
			if i+1 < len(line) {
				line[i+1].Text = s.Text + line[i+1].Text
				continue
			}
		}
		if n := len(out); n > 0 && out[n-1].Color == s.Color && out[n-1].Dim == s.Dim {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// sgr is the style one SGR sequence leaves a run in.
func sgr(s span, params string) span {
	f := strings.Split(params, ";")
	for i := 0; i < len(f); i++ {
		switch f[i] {
		case "", "0":
			s.Color, s.Dim = "", false
		case "2":
			s.Dim = true
		case "22":
			s.Dim = false
		case "39":
			s.Color = ""
		case "38":
			if i+4 < len(f) && f[i+1] == "2" {
				s.Color = hexOf(f[i+2], f[i+3], f[i+4])
				i += 4
			} else {
				return s // a pen this wire never writes: leave the run as it was
			}
		}
	}
	return s
}

// hexOf writes three decimal channels as #rrggbb.
func hexOf(r, g, b string) string {
	var sb strings.Builder
	sb.WriteByte('#')
	for _, c := range []string{r, g, b} {
		v, err := strconv.Atoi(c)
		if err != nil || v < 0 || v > 255 {
			v = 0
		}
		sb.WriteString(strconv.FormatInt(int64(0x100+v), 16)[1:])
	}
	return sb.String()
}
