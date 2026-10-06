// element_test.go — laws for the element door: the drawing a render hook
// is handed, and the rung it is drawn on.

package drawer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/term"
)

// The cells rung's wire writes four escapes and a render hook draws none:
// every one of them comes back as a style on a run, a run of air goes to
// its neighbour, and an escape the wire never writes is passed over rather
// than printed.
func TestSpansReadTheEscapesTheWireWrites(t *testing.T) {
	row := "\x1b[2m╭──\x1b[22m  \x1b[38;2;215;119;87mA\x1b[39m\x1b[5m b\x1b[0m\x1b[2m┤\x1b[22m"
	got := spansOf([]string{row, ""})
	want := [][]span{
		{
			{Text: "╭──  ", Dim: true},
			{Text: "A", Color: "#d77757"},
			{Text: " b"},
			{Text: "┤", Dim: true},
		},
		{},
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("spans\n got %s\nwant %s", g, w)
	}
}

// The element is the display hook's drawing, not another one: row for row
// the same text, with the escapes read into styles.
func TestTheElementIsTheDisplayHooksDrawing(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	src := "digraph { rankdir=LR; parse -> check [color=\"#f7768e\"]; check -> emit }"
	el := r.element(t.Context(), src, 90)
	if el.Kind != "text" || el.Notice {
		t.Fatalf("a graph that fits came back as %q (notice %v)", el.Kind, el.Notice)
	}
	rows, _ := drawRows(t.Context(), src, 90)
	if len(el.Lines) != len(rows) {
		t.Fatalf("%d lines for %d rows", len(el.Lines), len(rows))
	}
	coloured := false
	for i, line := range el.Lines {
		var b strings.Builder
		for _, s := range line {
			b.WriteString(s.Text)
			coloured = coloured || s.Color == "#f7768e"
		}
		if want := grid.StripSGR(rows[i]); b.String() != want {
			t.Errorf("line %d is %q, the hook's row is %q", i, b.String(), want)
		}
	}
	if !coloured {
		t.Error("the edge the graph coloured lost its colour on the way")
	}
}

// A fence that will not fit is a notice over its source here too, and it
// says it is one: a reply cut off mid-fence gets its source back rather
// than a reason, and only the hook can tell which rows those are.
func TestAnElementNoticeSaysItIsOne(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	src := "digraph { rankdir=LR; alpha -> \"a label far wider than thirty columns of window\" }"
	el := r.element(t.Context(), src, 30)
	if el.Kind != "text" || !el.Notice {
		t.Fatalf("a graph too wide came back as %q (notice %v)", el.Kind, el.Notice)
	}
	var all strings.Builder
	for _, line := range el.Lines {
		for _, s := range line {
			all.WriteString(s.Text)
		}
		all.WriteByte('\n')
	}
	if !strings.Contains(all.String(), "no diagram") || !strings.Contains(all.String(), "alpha ->") {
		t.Fatalf("notice without its source, or source without its notice:\n%s", all.String())
	}
}

// Every row is an array, the empty one too: the module maps over every
// row it is handed, and a notice always carries the blank row between the
// reason and the source.
func TestAnElementHasNoNullRow(t *testing.T) {
	r := run{rung: rungCells, theme: &inForce{}}
	src := "digraph { rankdir=LR; alpha -> \"a label far wider than thirty columns of window\" }"
	el := r.element(t.Context(), src, 30)
	if !el.Notice {
		t.Fatalf("a graph too wide came back as %q, not a notice", el.Kind)
	}
	b, err := json.Marshal(el)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("null")) {
		t.Fatalf("a notice's JSON carries a null: %s", b)
	}
}

// An indented fence is drawn as the display hook draws it: at the body's
// width less the indent, which is a different drawing from the one the
// same fence gets at the margin.
func TestAnIndentedFencesElementIsTheDisplayHooksDrawing(t *testing.T) {
	clearImageEnv(t)
	t.Setenv("TERM", "xterm-256color")
	src := "digraph { rankdir=LR; alpha -> bravo -> charlie -> delta -> echo }"
	const cols, indent = 80, 30
	el := elementThroughMain(t, src, "-render", "cells", "-element", "-cols", "80", "-indent", "30")
	r := run{rung: rungCells, theme: &inForce{}}
	hook := r.drawBlock(t.Context(), src, hookWidth(cols)-indent)
	margin := r.drawBlock(t.Context(), src, hookWidth(cols))
	if hook == nil || slices.Equal(hook, margin) {
		t.Fatalf("the fence draws the same at the margin and indented %d; the law reads nothing", indent)
	}
	rows := hook[1 : len(hook)-1]
	if len(el.Lines) != len(rows) {
		t.Fatalf("%d lines for the hook's %d rows", len(el.Lines), len(rows))
	}
	for i, line := range el.Lines {
		var b strings.Builder
		for _, s := range line {
			b.WriteString(s.Text)
		}
		if want := grid.StripSGR(rows[i]); b.String() != want {
			t.Errorf("line %d is %q, the hook's row is %q", i, b.String(), want)
		}
	}
}

// A drawing the deadline overtakes is none, answered at the deadline: a
// layout under way does not stop for a context, so the door does not wait
// for it.
func TestALateDrawingIsNone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	release := make(chan struct{})
	defer close(release)
	el := answerBy(ctx, func() element {
		<-release
		return element{Kind: "text"}
	})
	if el.Kind != "none" {
		t.Errorf("a drawing past its deadline came back as %q", el.Kind)
	}
	if el := answerBy(t.Context(), func() element { return element{Kind: "text"} }); el.Kind != "text" {
		t.Errorf("a drawing inside its deadline came back as %q", el.Kind)
	}
}

// elementThroughMain is the door through Main: src on stdin, args on the
// command line, the one element it prints.
func elementThroughMain(t *testing.T, src string, args ...string) element {
	t.Helper()
	in, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString(src)
	in.Seek(0, 0)
	was := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = was; in.Close() }()
	out := captured(t)
	if code := Main(args); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var el element
	if err := json.Unmarshal([]byte(out()), &el); err != nil {
		t.Fatal(err)
	}
	return el
}

// clearImageEnv stands a test outside every place Claude Code refuses a
// picture, whatever the shell running the suite is standing in.
func clearImageEnv(t *testing.T) {
	for _, k := range []string{"CLAUDE_CODE_FORCE_TERMINAL_IMAGES", "CLAUDE_CODE_SESSION_KIND", "TMUX", "STY"} {
		t.Setenv(k, "")
	}
}

// Claude Code's rule, as its 2.1.283 binary has it: forced, always; a
// background session, tmux or screen, never; elsewhere the terminals that
// draw placeholder images. An Image it refuses is one dim line of alt, so
// the rule is followed exactly, or the graph is lost.
func TestImagesDrawWhereClaudeCodeDrawsThem(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		term string
		want bool
	}{
		{"kitty", nil, "xterm-kitty", true},
		{"ghostty", nil, "xterm-ghostty", true},
		{"anything else", nil, "xterm-256color", false},
		{"kitty under tmux", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, "xterm-kitty", false},
		{"kitty under screen", map[string]string{"STY": "1.pts-0.host"}, "xterm-kitty", false},
		{"a background session", map[string]string{"CLAUDE_CODE_SESSION_KIND": "bg"}, "xterm-kitty", false},
		{"forced, under tmux", map[string]string{"CLAUDE_CODE_FORCE_TERMINAL_IMAGES": "1", "TMUX": "x"}, "tmux-256color", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			clearImageEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if got := imagesDraw(c.term); got != c.want {
				t.Errorf("imagesDraw(%q) is %v, want %v", c.term, got, c.want)
			}
		})
	}
}

// The element's rung is the display hook's, with Claude Code deciding
// whether a picture goes out at all: glyphs where it would not draw one,
// whatever was asked for, and glyphs where the cell's size is unknown and
// nothing asked for pixels.
func TestTheElementRungFollowsClaudeCode(t *testing.T) {
	clearImageEnv(t)
	geom := term.Geom{CellW: 10, CellH: 21}
	for _, c := range []struct {
		name  string
		want  rung
		term  string
		geom  term.Geom
		draws rung
	}{
		{"kitty and a cell size", rungAuto, "xterm-kitty", geom, rungPixels},
		{"kitty, no cell size", rungAuto, "xterm-kitty", term.Geom{}, rungCells},
		{"cells asked for", rungCells, "xterm-kitty", geom, rungCells},
		{"pixels asked for where none draw", rungPixels, "xterm-256color", geom, rungCells},
		{"pixels asked for in kitty", rungPixels, "xterm-kitty", term.Geom{}, rungPixels},
	} {
		r := run{rung: c.want, term: c.term, geom: c.geom}
		if got := r.elementRung(); got != c.draws {
			t.Errorf("%s: draws in %s, want %s", c.name, got, c.draws)
		}
	}
	t.Setenv("TMUX", "x")
	if got := (run{term: "xterm-kitty", geom: geom}).elementRung(); got != rungCells {
		t.Errorf("kitty under tmux draws in %s; Claude Code draws no picture there", got)
	}
}

// A picture for Claude Code: a whole PNG within its two megabytes, a box
// inside its 255 cells each way, and an alt that is one line.
func TestAnImageElementIsOneClaudeCodeTakes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearImageEnv(t)
	r := run{rung: rungAuto, term: "xterm-kitty", geom: term.Geom{CellW: 10, CellH: 21}, theme: &inForce{}}
	src := "digraph {\n  rankdir=LR;\n  wire -> grid -> paint\n}"
	el := r.element(t.Context(), src, 300)
	if el.Kind != "image" {
		t.Fatalf("kitty with a cell size came back as %q", el.Kind)
	}
	png, err := base64.StdEncoding.DecodeString(el.PNG)
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) || len(png) > imageMaxBytes {
		t.Fatalf("not a PNG Claude Code takes: %d bytes, %v", len(png), err)
	}
	if el.Columns < 1 || el.Columns > imageMaxCells || el.Rows < 1 || el.Rows > imageMaxCells {
		t.Errorf("a %d×%d box is outside Claude Code's 1 to 255", el.Columns, el.Rows)
	}
	if el.Alt != "digraph { rankdir=LR; wire -> grid -> paint }" {
		t.Errorf("alt is %q", el.Alt)
	}
}

// The alt is read on one line, where a picture could not be one, and it
// stops where a line stops being read.
func TestTheAltIsOneLine(t *testing.T) {
	long := "digraph { " + strings.Repeat("a -> b;\n", 100) + "}"
	alt := altOf(long)
	if strings.ContainsAny(alt, "\n\t") || len([]rune(alt)) > 200 || !strings.HasSuffix(alt, "…") {
		t.Errorf("alt is not one cut line: %q", alt)
	}
	if altOf("  \n ") != "graph" {
		t.Errorf("an empty source has alt %q", altOf("  \n "))
	}
}

// A control character in a label or in a notice's quoted source reaches
// neither a run nor the alt: Claude Code refuses a whole tree whose Text
// holds one. What prints around it stays.
func TestNoControlCharacterReachesAnElement(t *testing.T) {
	const controls = "\x00\x01\a\b\n\v\f\r\x1b\x1f\x7f\u0080\u0085\u009b\u009f"
	src := "digraph { a [label=\"x\ay\x1bq\x01r\x7fs\u009bt\"]; a -> \"a label far wider than thirty columns\" }"
	r := run{rung: rungCells, theme: &inForce{}}
	for _, c := range []struct {
		name   string
		width  int
		notice bool
	}{
		{"drawing", 80, false},
		{"notice", 30, true},
	} {
		el := r.element(t.Context(), src, c.width)
		if el.Kind != "text" || el.Notice != c.notice {
			t.Fatalf("%s: came back as %q (notice %v)", c.name, el.Kind, el.Notice)
		}
		var all strings.Builder
		for _, line := range el.Lines {
			for _, s := range line {
				if strings.ContainsAny(s.Text, controls) {
					t.Errorf("%s: a run carries a control character: %q", c.name, s.Text)
				}
				all.WriteString(s.Text)
			}
		}
		if !strings.Contains(all.String(), "xyqrst") {
			t.Errorf("%s: the label lost what prints around its controls:\n%s", c.name, all.String())
		}
	}
	alt := altOf(src + "\n\a \x1b\u0085end")
	if strings.ContainsAny(alt, controls+"\t") || !strings.HasSuffix(alt, "xyqrst\"]; a -> \"a label far wider than thirty columns\" } end") {
		t.Errorf("alt is %q", alt)
	}
}

// The door itself, through Main: the source on stdin, one JSON object on
// stdout, whatever the source is.
func TestTheElementDoorPrintsOneObject(t *testing.T) {
	clearImageEnv(t)
	t.Setenv("TERM", "xterm-256color")
	for _, c := range []struct{ src, kind string }{
		{"digraph { a -> b }", "text"},
		{"\xff\xfe not text", "none"},
	} {
		in, err := os.CreateTemp(t.TempDir(), "stdin-*")
		if err != nil {
			t.Fatal(err)
		}
		in.WriteString(c.src)
		in.Seek(0, 0)
		was := os.Stdin
		os.Stdin = in
		out := captured(t)
		code := Main([]string{"-element", "-cols", "80"})
		os.Stdin = was
		in.Close()
		got := out()
		if code != 0 {
			t.Fatalf("%q: exit %d", c.src, code)
		}
		var el element
		if err := json.Unmarshal([]byte(got), &el); err != nil || strings.Count(strings.TrimSpace(got), "\n") != 0 {
			t.Fatalf("%q: not one JSON object: %q", c.src, got)
		}
		if el.Kind != c.kind {
			t.Errorf("%q came back as %q, want %q", c.src, el.Kind, c.kind)
		}
	}
}
