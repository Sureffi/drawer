// fence_test.go — laws for the transducer: what the hook wire may and may
// not do to a reply, and how a delta's process takes its turn.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// drawAt is Draw's emit bound to a width, in the cells rung: what the
// transducer laws hand stream.
func drawAt(w int) func(string, int) []string {
	r := run{rung: rungCells, theme: &inForce{}}
	return func(src string, indent int) []string {
		return r.drawBlock(src, w-indent)
	}
}

// The hook may replace a fence. It may not touch a byte outside one, and
// prose that merely mentions a fence is prose.
//
// Seven bytes at a time on purpose: CC splits a reply where it likes and
// the boundary lands mid-line often enough to matter. An earlier version
// of the transducer rebuilt lines with strings.Join and so inserted a
// newline at every such boundary — invisible whenever the fence drew, and
// a corrupted message whenever it did not.
func TestProseSurvivesTheHookWire(t *testing.T) {
	in := "Use a ```dot fence when you want a drawing.\nRows get reserved for it.\n```\ndone\n"
	var st state
	var b strings.Builder
	for i := 0; i < len(in); i += 7 {
		end := i + 7
		if end > len(in) {
			end = len(in)
		}
		b.WriteString(stream(in[i:end], end == len(in), &st, drawAt(100)))
	}
	if got := b.String(); got != in {
		t.Errorf("hook wire damaged prose:\n in: %q\nout: %q", in, got)
	}
	if st.InFence || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// Everything the transducer emits is a substring of what it was handed.
// A drawn fence is the one exception and it is a whole-block swap, so a
// mid-line delta boundary must never show up as a line break.
func TestDeltaBoundariesAreNotLineBreaks(t *testing.T) {
	in := "digraph { a -> b } and some prose after it, all on one line with no newline at all"
	for _, chunk := range []int{1, 3, 7, 13} {
		var st state
		var b strings.Builder
		for i := 0; i < len(in); i += chunk {
			end := i + chunk
			if end > len(in) {
				end = len(in)
			}
			b.WriteString(stream(in[i:end], end == len(in), &st, drawAt(100)))
		}
		if got := b.String(); got != in {
			t.Errorf("chunk %d: text was rebuilt rather than passed through:\n in: %q\nout: %q",
				chunk, in, got)
		}
	}
}

// A hook is one process per delta, and CC splits a fence where it likes:
// the same prompt gave ["```dot\n<src>\n", "```"] on one run and
// ["```dot\n", "<src>\n", "```"] on the next. Both must draw, or the
// feature works on a coin flip.
func TestFenceDrawsAcrossEitherSplit(t *testing.T) {
	src := "digraph { rankdir=LR; a -> b -> c }"
	splits := [][]string{
		{"```dot\n" + src + "\n", "```"},
		{"```dot\n", src + "\n", "```"},
		{"```dot\n" + src + "\n```"},
	}
	for i, deltas := range splits {
		var st state
		var shown strings.Builder
		for j, d := range deltas {
			shown.WriteString(stream(d, j == len(deltas)-1, &st, drawAt(90)))
		}
		out := shown.String()
		if !strings.Contains(out, "▶") {
			t.Fatalf("split %d drew nothing:\n%s", i, out)
		}
		if strings.Contains(out, src) || !strings.HasPrefix(out, fenceTick+"\n") {
			t.Fatalf("split %d did not replace the fence with a bare drawn one:\n%s", i, out)
		}
		if st.InFence || st.PendingClose {
			t.Fatalf("split %d left state behind: %+v", i, st)
		}
	}
}

// A fence is what markdown says one is, and a shorter run inside a longer
// one is content. A ```dot quoted inside a four-backtick fence used to be
// read as an opener and drawn; it is text, and every byte of it comes back.
// Seven bytes at a time, so the boundaries land inside the markers too.
func TestAQuotedFenceIsText(t *testing.T) {
	in := "Write it like this:\n\n````\n```dot\ndigraph { a -> b }\n```\n````\n\nand it draws.\n"
	var st state
	var b strings.Builder
	for i := 0; i < len(in); i += 7 {
		end := min(i+7, len(in))
		b.WriteString(stream(in[i:end], end == len(in), &st, drawAt(100)))
	}
	if got := b.String(); got != in {
		t.Errorf("a quoted fence was touched:\n in: %q\nout: %q", in, got)
	}
	if st.InFence || st.Foreign || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// A fence indented under a list item is a fence, and the drawing stands
// in its indent: every row of the block carries it, and the drawing was
// made for the width the indent leaves. The prose around it is untouched.
func TestAFenceUnderAListItemDraws(t *testing.T) {
	in := "- the flow:\n\n  ```dot\n  digraph { rankdir=LR; a -> b }\n  ```\n\n- done\n"
	var st state
	gotIndent := -1
	out := stream(in, true, &st, func(src string, indent int) []string {
		gotIndent = indent
		return drawAt(100)(src, indent)
	})
	if !strings.HasPrefix(out, "- the flow:\n\n") || !strings.HasSuffix(out, "\n\n- done\n") {
		t.Fatalf("prose around the fence damaged:\n%q", out)
	}
	if gotIndent != 2 {
		t.Errorf("the drawing was asked for at indent %d, want 2", gotIndent)
	}
	block := strings.TrimSuffix(strings.TrimPrefix(out, "- the flow:\n\n"), "\n\n- done\n")
	rows := strings.Split(block, "\n")
	if len(rows) < 3 || strings.TrimSpace(rows[0]) != fenceTick || strings.TrimSpace(rows[len(rows)-1]) != fenceTick {
		t.Fatalf("the fence was not replaced by a bare drawn one:\n%q", block)
	}
	if !strings.Contains(block, "▶") {
		t.Fatalf("nothing drawn:\n%s", block)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r, "  ") {
			t.Errorf("a row left the list item's indent: %q", r)
		}
	}
}

// What is drawn: a fence labelled dot or graphviz, whatever is in it, and
// an unlabelled fence whose first line opens a graph. What is left alone,
// byte for byte: a fence labelled anything else, an unlabelled one with
// anything else in it, and mermaid, whose `graph LR` opens no brace.
func TestOnlyGraphsAreDrawn(t *testing.T) {
	cases := []struct {
		name, in string
		drawn    bool
	}{
		{"dot", "```dot\ndigraph { a -> b }\n```\n", true},
		{"graphviz", "```graphviz\ndigraph { a -> b }\n```\n", true},
		{"dot with a title", "```dot title=\"flow\"\ndigraph { a -> b }\n```\n", true},
		{"tildes", "~~~dot\ndigraph { a -> b }\n~~~\n", true},
		{"four ticks", "````dot\ndigraph { a -> b }\n````\n", true},
		{"unlabelled digraph", "```\ndigraph G {\n  a -> b\n}\n```\n", true},
		{"unlabelled strict graph", "```\nstrict graph { a -- b }\n```\n", true},
		{"python", "```python\nprint('hi')\n```\n", false},
		{"python quoting a marker", "```python\n```dot\nprint('hi')\n```\n", false},
		{"unlabelled prose", "```\nsome text\n```\n", false},
		{"unlabelled empty", "```\n```\n", false},
		{"mermaid", "```mermaid\ngraph LR\n  A --> B\n```\n", false},
		{"unlabelled mermaid", "```\ngraph LR\n  A --> B\n```\n", false},
		{"inline code", "```dot``` is the marker\n", false},
	}
	for _, c := range cases {
		var st state
		out := stream(c.in, true, &st, drawAt(100))
		if st.InFence || st.Foreign || st.PendingClose || st.Buf != "" {
			t.Errorf("%s: state left behind: %+v", c.name, st)
		}
		if c.drawn && !strings.Contains(out, "╭") {
			t.Errorf("%s: not drawn:\n%q", c.name, out)
		}
		if !c.drawn && out != c.in {
			t.Errorf("%s: touched:\n in: %q\nout: %q", c.name, c.in, out)
		}
	}
}

// A fence that is not ours streams as it arrives. Holding it to its close
// would take a page of code off the screen for the length of the reply,
// on no promise at all; an unlabelled fence is held only as far as its
// first line, which is where the decision lives.
func TestAForeignFenceIsNotHeld(t *testing.T) {
	var st state
	if got := stream("```python\nx = 1\n", false, &st, drawAt(100)); got != "```python\nx = 1\n" {
		t.Errorf("a labelled fence was held: %q", got)
	}
	if got := stream("y = 2\n```\nafter\n", true, &st, drawAt(100)); got != "y = 2\n```\nafter\n" {
		t.Errorf("the rest of it was touched: %q", got)
	}
	st = state{}
	if got := stream("```\n", false, &st, drawAt(100)); got != "" {
		t.Errorf("an unlabelled fence was let go before its first line: %q", got)
	}
	if got := stream("some text\n", false, &st, drawAt(100)); got != "```\nsome text\n" {
		t.Errorf("an unlabelled fence with prose in it was held past its first line: %q", got)
	}
	if got := stream("```\n", true, &st, drawAt(100)); got != "```\n" {
		t.Errorf("its closer was touched: %q", got)
	}
	if st.InFence || st.Foreign || st.PendingClose || st.Buf != "" {
		t.Errorf("state left behind: %+v", st)
	}
}

// Suppressing a delta is taking content off the screen against a promise
// to put something better back. A source that never draws must come back
// whole rather than vanish — the worst acceptable outcome is a visible
// DOT fence, and silence is not on the list.
func TestHeldTextIsNeverLost(t *testing.T) {
	var st state
	var shown strings.Builder
	shown.WriteString(stream("```dot\ndigraph { a -> ", false, &st, drawAt(90)))
	shown.WriteString(stream("b", true, &st, drawAt(90)))
	out := shown.String()
	for _, want := range []string{"```dot", "digraph { a -> ", "b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("held text lost %q on flush:\n%q", want, out)
		}
	}
	if st.InFence {
		t.Fatal("flush left the fence open")
	}
}

// Prose that merely mentions a fence is prose. Sessions about this code
// are mostly that, and the hook sees raw markdown before CC has laid any
// of it out.
func TestProseAroundAFenceSurvivesTheHook(t *testing.T) {
	var st state
	in := "Before.\n\n```dot\ndigraph { x -> y }\n```\n\nAfter."
	out := stream(in, true, &st, drawAt(90))
	if !strings.HasPrefix(out, "Before.") || !strings.HasSuffix(out, "After.") {
		t.Fatalf("prose damaged:\n%q", out)
	}
	if !strings.ContainsAny(out, "▶▼") {
		t.Fatalf("fence was not drawn:\n%q", out)
	}
}

// ---------- turns ----------

// A delta's process waits for the one before it: started first with the
// later index, it takes its turn after the earlier delta's process has
// saved, and reads what that one saved. A turn nobody comes to take is
// waited for only so long.
func TestHookDeltasTakeTurns(t *testing.T) {
	t.Setenv("DRAWER_STATE", t.TempDir())
	var mu sync.Mutex
	var order []int
	took := func(i int) { mu.Lock(); order = append(order, i); mu.Unlock() }
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		st, done := takeTurn("m", 1, 2*time.Second)
		defer done()
		took(1)
		if st.Next != 1 || st.Buf != "held by 0" {
			t.Errorf("delta 1 took its turn on state %+v, not the one delta 0 saved", st)
		}
		st.Next = 2
		saveState("m", st, true)
	}()
	time.Sleep(30 * time.Millisecond) // delta 1 is waiting
	go func() {
		defer wg.Done()
		st, done := takeTurn("m", 0, 2*time.Second)
		defer done()
		took(0)
		time.Sleep(40 * time.Millisecond) // the draw
		st.Next, st.Buf = 1, "held by 0"
		saveState("m", st, false)
	}()
	wg.Wait()
	if len(order) != 2 || order[0] != 0 || order[1] != 1 {
		t.Errorf("turns were taken in the order %v, want [0 1]", order)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("DRAWER_STATE"), "m.json.lock")); err == nil {
		t.Error("the lock file outlived the message")
	}
	start := time.Now()
	_, done := takeTurn("nobody", 3, 50*time.Millisecond)
	done()
	if waited := time.Since(start); waited < 50*time.Millisecond || waited > time.Second {
		t.Errorf("a turn nobody takes was waited for %v, want about the patience", waited)
	}
}
