// notice_test.go — laws for the notice: it says why, in the room it has,
// and where there is no room it says nothing at all.

package notice

import (
	"context"
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/grid"
)

// Source that will not draw used to look exactly like source somebody
// wanted to read. A window three columns too narrow and a graph with a
// typo in it were the same picture, and neither said so.
func TestAFenceThatWillNotDrawSaysWhy(t *testing.T) {
	cases := []struct {
		name, src   string
		w, region   int
		wantNumbers bool
	}{
		{"too wide", "digraph { rankdir=LR; alpha -> beta -> gamma -> delta }", 26, 6, true},
		{"will not parse", "digraph { a -> ", 90, 8, false},
	}
	for _, c := range cases {
		rows := Draw(Reason(t.Context(), c.src, c.w, c.region), c.w, c.region)
		if rows == nil {
			t.Fatalf("%s: nothing drawn, and the reader learns nothing", c.name)
		}
		if len(rows) > c.region {
			t.Fatalf("%s: notice is %d rows in a %d-row region", c.name, len(rows), c.region)
		}
		for _, r := range rows {
			if grid.Cells(r) > c.w {
				t.Fatalf("%s: notice is %d cells wide in %d columns: %q",
					c.name, grid.Cells(r), c.w, r)
			}
		}
		joined := strings.Join(rows, " ")
		if c.wantNumbers && !strings.ContainsAny(joined, "0123456789") {
			t.Fatalf("%s: notice carries no measurement: %q", c.name, joined)
		}
	}
}

// A reason names a number the reader can act on, and never one smaller
// than the room they already have. The measurement here is graphviz's
// footprint, and the rung that failed sizes its own boxes and its own
// gaps: where the footprint fits and the drawing still did not come out,
// the old answer was "38 columns would draw it sideways" to a reader
// sitting at 100.
func TestAReasonNeverNamesLessRoomThanTheReaderHas(t *testing.T) {
	// Five nodes, fifteen edges: graphviz places it small and the rung
	// runs out of lanes.
	const dense = `digraph { a->b; a->c; a->d; a->e; b->c; b->d; b->e;
	  c->d; c->e; d->e; e->a; d->a; c->a; e->b; d->b }`
	const width, region = 100, 40
	got := Reason(t.Context(), dense, width, region)
	for _, n := range numbersIn(got) {
		if n < width && n != region {
			t.Errorf("the reason offers %d to a reader who has %d columns: %q", n, width, got)
		}
	}
}

// numbersIn is every run of digits in a reason, as numbers.
func numbersIn(s string) []int {
	var out []int
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		n := 0
		for _, c := range s[i:j] {
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
		i = j
	}
	return out
}

// A deadline that ran out at the door is not a fence anybody wrote wrong.
// The reason used to be graphviz's own error text, which said "context
// deadline exceeded" under a heading that blamed the source.
func TestADeadlineIsNotABadFence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := Reason(ctx, "digraph { a -> b }", 90, 8)
	if got != "the layout ran out of time" {
		t.Fatalf("a spent context yields %q", got)
	}
}

// A notice too small to read is worse than the source it would cover, so
// there is a floor below which nothing is drawn at all.
func TestATinyRegionKeepsItsSource(t *testing.T) {
	if rows := Draw("needs 44 columns, this window has 12", 12, 6); rows != nil {
		t.Fatalf("drew a notice into 12 columns: %q", rows)
	}
	if rows := Draw("needs 44 columns", 60, 2); rows != nil {
		t.Fatalf("drew a notice into 2 rows: %q", rows)
	}
}
