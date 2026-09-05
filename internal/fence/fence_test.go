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
	out := Stream("  ```dot\n  digraph { a -> b }\n  ```\n", true, &st, s.emit)
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
	if out := Stream(in, true, &st, s.emit); out != in {
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
		if out := Stream(in, true, &st, s.emit); out != in {
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
	if out := Stream("```dot\ndigraph { rankdir=LR; a -> ", false, &st, s.emit); out != "" {
		t.Errorf("half a fence was displayed: %q", out)
	}
	if len(s.srcs) != 0 {
		t.Fatalf("emit was handed half a source: %q", s.srcs)
	}
	if out := Stream("b }\n```\n", true, &st, s.emit); out != mark+"\n" {
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
	out.WriteString(Stream(in[:12], false, &st, s.emit))
	out.WriteString(Stream(in[12:], true, &st, s.emit))
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
