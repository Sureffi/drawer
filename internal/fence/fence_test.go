// fence_test.go — laws for the transducer with the drawing stubbed out:
// what reaches emit, and what is never handed to it at all.
//
// The real drawings are the wire's laws, and they live where the wire does.
// What is left here is the transducer's own half: which runs of backticks
// are a fence, which fence is a graph's, and that a fence split across two
// deltas is one source by the time emit sees it.

package fence

import (
	"strings"
	"testing"
)

// mark is what a stub draws: one row no fence could have carried.
const mark = "<<drawn>>"

// stub is an emit that answers a fixed row and remembers what it was asked.
type stub struct {
	srcs    []string
	indents []int
}

func (s *stub) emit(src string, indent int) []string {
	s.srcs = append(s.srcs, src)
	s.indents = append(s.indents, indent)
	return []string{mark}
}

// A ```dot fence is handed to emit as its source alone — no markers, no
// indent — and with the cells its indent spends, which is the width the
// drawing does not have.
func TestADotFenceReachesEmitWithItsSourceAndIndent(t *testing.T) {
	var s stub
	var st State
	out := Stream(t.Context(), "  ```dot\n  digraph { a -> b }\n  ```\n", true, &st, s.emit)
	if len(s.srcs) != 1 {
		t.Fatalf("emit was called %d times, want once", len(s.srcs))
	}
	if s.srcs[0] != "digraph { a -> b }" {
		t.Errorf("emit was handed %q, not the fence's source", s.srcs[0])
	}
	if s.indents[0] != 2 {
		t.Errorf("emit was told the indent is %d, want 2", s.indents[0])
	}
	if out != "  "+mark+"\n" {
		t.Errorf("the fence was not replaced by the drawing in its indent: %q", out)
	}
	if st.InFence || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// A shorter run inside a longer one is content: a ```dot quoted in a
// four-backtick fence is text, and every byte of it comes back.
func TestAQuotedDotFenceIsContent(t *testing.T) {
	var s stub
	var st State
	in := "````\n```dot\ndigraph { a -> b }\n```\n````\n"
	if out := Stream(t.Context(), in, true, &st, s.emit); out != in {
		t.Errorf("a quoted fence was touched:\n in: %q\nout: %q", in, out)
	}
	if len(s.srcs) != 0 {
		t.Errorf("emit was handed %q", s.srcs)
	}
}

// A marker only counts at the start of its line. Prose that mentions one
// mid-sentence is prose, and so is inline code.
func TestAMarkerMidSentenceIsProse(t *testing.T) {
	for _, in := range []string{
		"Use a ```dot fence when you want a drawing.\n",
		"```dot``` is the marker\n",
	} {
		var s stub
		var st State
		if out := Stream(t.Context(), in, true, &st, s.emit); out != in {
			t.Errorf("prose was touched:\n in: %q\nout: %q", in, out)
		}
		if len(s.srcs) != 0 {
			t.Errorf("%q: emit was handed %q", in, s.srcs)
		}
	}
}

// CC splits a reply where it likes. A fence cut in half by a delta boundary
// is one source by the time emit sees it, and the half that arrived first
// showed nothing in the meantime.
func TestAFenceSplitOverTwoDeltasReassembles(t *testing.T) {
	var s stub
	var st State
	if out := Stream(t.Context(), "```dot\ndigraph { rankdir=LR; a -> ", false, &st, s.emit); out != "" {
		t.Errorf("half a fence was displayed: %q", out)
	}
	if len(s.srcs) != 0 {
		t.Fatalf("emit was handed half a source: %q", s.srcs)
	}
	if out := Stream(t.Context(), "b }\n```\n", true, &st, s.emit); out != mark+"\n" {
		t.Errorf("the reassembled fence was not replaced by the drawing: %q", out)
	}
	if len(s.srcs) != 1 || s.srcs[0] != "digraph { rankdir=LR; a -> b }" {
		t.Errorf("emit was handed %q, not the whole source", s.srcs)
	}
	if st.InFence || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// A fence labelled anything else is not ours: its text streams as it
// arrives, only its closer is watched for, and emit never hears about it.
func TestAForeignFenceStreamsThrough(t *testing.T) {
	var s stub
	var st State
	in := "```go\nfunc main() {}\n```\nafter\n"
	var out strings.Builder
	out.WriteString(Stream(t.Context(), in[:12], false, &st, s.emit))
	out.WriteString(Stream(t.Context(), in[12:], true, &st, s.emit))
	if got := out.String(); got != in {
		t.Errorf("a foreign fence was touched:\n in: %q\nout: %q", in, got)
	}
	if len(s.srcs) != 0 {
		t.Errorf("emit was handed %q", s.srcs)
	}
	if st.InFence || st.Foreign || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// A graph that lays out is drawn before its closer arrives, and the drawing
// stands for the whole fence: every line between the graph and the closer
// — a blank one, a trailing comment, a shorter run in a longer fence — is
// the fence's, and none of it reaches the prose after it.
func TestEverythingUpToAnEarlyDrawingsCloserIsTheFences(t *testing.T) {
	for _, deltas := range [][]string{
		{"```dot\ndigraph { a -> b }\n", "\n```\n", "After.\n"},
		{"```dot\ndigraph { a -> b }\n", "// the end\n```\n", "After.\n"},
		{"````dot\ndigraph { a -> b }\n", "```\n````\n", "After.\n"},
	} {
		var s stub
		var st State
		var out strings.Builder
		for i, d := range deltas {
			out.WriteString(Stream(t.Context(), d, i == len(deltas)-1, &st, s.emit))
		}
		if got := out.String(); got != mark+"\nAfter.\n" {
			t.Errorf("%q: the fence's tail leaked past the drawing: %q", deltas, got)
		}
		if len(s.srcs) != 1 || s.srcs[0] != "digraph { a -> b }" {
			t.Errorf("%q: emit was handed %q", deltas, s.srcs)
		}
	}
}

// A message that ends with an early-drawn fence still open gives back what
// came after the drawing as it came, starting on the row after the
// drawing's last: it was never drawn, so it is not the drawing's to take.
func TestAnUnclosedTailAfterAnEarlyDrawingIsGivenBackOnItsOwnRow(t *testing.T) {
	var s stub
	var st State
	out := Stream(t.Context(), "```dot\ndigraph { a -> b }\n", false, &st, s.emit)
	out += Stream(t.Context(), "and then nothing closed it\nMore prose.\n", true, &st, s.emit)
	if want := mark + "\nand then nothing closed it\nMore prose.\n"; out != want {
		t.Errorf("the tail was not given back on its own row:\n got: %q\nwant: %q", out, want)
	}
	if st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// An early drawing stands for the fewest whole lines that lay out, so
// where the deltas fell does not move it: an unclosed fence handed over in
// one delta draws the same lines as streamed, and what followed the graph
// — whole lines or a fragment — comes back on its own row, never into the
// drawing.
func TestAnEarlyDrawingStandsForTheFewestLinesThatLayOut(t *testing.T) {
	for _, deltas := range [][]string{
		{"```dot\ndigraph { a -> b }\n", "and then nothing closed it\nMore prose.\n"},
		{"```graphviz\n", "graph { a -- b }\nTrailing with no newline", ""},
	} {
		streamed, sSrcs := drawAll(t, deltas)
		whole, wSrcs := drawAll(t, []string{strings.Join(deltas, "")})
		if streamed != whole {
			t.Errorf("%q: the display depends on where the deltas fell:\n streamed: %q\n   in one: %q", deltas, streamed, whole)
		}
		if len(sSrcs) != 1 || len(wSrcs) != 1 || sSrcs[0] != wSrcs[0] || strings.Contains(sSrcs[0], "\n") {
			t.Errorf("%q: emit was handed %q streamed and %q in one, not the graph's one line", deltas, sSrcs, wSrcs)
		}
		if !strings.HasPrefix(streamed, mark+"\n") {
			t.Errorf("%q: the rest did not start on its own row: %q", deltas, streamed)
		}
	}
}

// drawAll streams deltas through the transducer, the last one final, and
// answers what was displayed and what emit was handed.
func drawAll(t *testing.T, deltas []string) (string, []string) {
	var s stub
	var st State
	var out strings.Builder
	for i, d := range deltas {
		out.WriteString(Stream(t.Context(), d, i == len(deltas)-1, &st, s.emit))
	}
	if st.InFence || st.PendingClose || st.Buf != "" {
		t.Errorf("%q: state left behind: %+v", deltas, st)
	}
	return out.String(), s.srcs
}

// A closer whose line has not ended yet is still this fence's closer: it
// waits for its newline where closers are looked for, and the fence never
// reaches into the next one.
func TestACloserWithoutItsNewlineClosesItsOwnFence(t *testing.T) {
	var s stub
	var st State
	deltas := []string{
		"```dot\ngraph LR\n  A --> B\n```",
		"\n\nThen:\n\n```dot\ndigraph { a -> b }\n```\n",
		"Done.",
	}
	var out strings.Builder
	for i, d := range deltas {
		out.WriteString(Stream(t.Context(), d, i == len(deltas)-1, &st, s.emit))
	}
	if want := mark + "\n\nThen:\n\n" + mark + "\nDone."; out.String() != want {
		t.Errorf("the fences ran together:\n got: %q\nwant: %q", out.String(), want)
	}
	if len(s.srcs) != 2 || s.srcs[0] != "graph LR\n  A --> B" || s.srcs[1] != "digraph { a -> b }" {
		t.Errorf("emit was handed %q, not each fence's own source", s.srcs)
	}
}
