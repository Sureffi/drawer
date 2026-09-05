// fence.go — the transducer: a fence in, a drawing or the same bytes out.
//
// A fence is what markdown says one is: a run of three or more backticks
// or tildes at the start of its own line, indented or not, with an info
// string after it, closed by a run of the same character at least as long
// with nothing else on the line. A shorter run inside is content, so a
// ```dot quoted in a four-backtick fence is text and never an opener. A
// marker only counts at the start of its line: prose that mentions one is
// prose — sessions about this code are mostly that, and a scan for the
// substring swallowed the sentence it appeared in.
//
// Every fence is tracked and only a graph's is drawn. A fence labelled
// `dot` or `graphviz` is a graph's, whatever is in it — the label is the
// model's word, and a graph that will not draw gets told why. An
// unlabelled fence is a graph's when its first line opens one, `digraph {`
// or `graph {`, and prose otherwise. A fence labelled anything else is
// streamed through as it arrives, and so is everything inside it, its
// closer the only thing watched for.
//
// What holding a fence costs, stated plainly: suppressed deltas are content
// taken off the screen on the promise of putting something better back. If
// the promise is not kept the text is simply gone, so the give-back at the
// end of stream is the load-bearing half of this file, not the drawing.

package fence

import (
	"context"
	"regexp"
	"strings"

	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
)

// Opener is a fence's opening line as read: the whitespace before the run,
// the run itself, and the first word of what followed it. The run says
// what closes the fence; the indent is what the drawing stands in.
type Opener struct {
	Indent string `json:"indent"`
	Run    string `json:"run"`
	Info   string `json:"info"`
}

var fenceRe = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})(.*)$")

// graphStart is a DOT graph's first line, as a model writes one: the
// keyword, a name if any, and the brace. Mermaid's `graph LR` has no
// brace, and is left to be what it is.
var graphStart = regexp.MustCompile(`(?i)^\s*(strict\s+)?(di)?graph\b[^{]*\{`)

// OpenerOf reads a line as a fence opener. A backtick run followed by
// text with a backtick in it is inline code, not a fence.
func OpenerOf(line string) (Opener, bool) {
	m := fenceRe.FindStringSubmatch(strings.TrimRight(line, " \t\r"))
	if m == nil || (m[2][0] == '`' && strings.Contains(m[3], "`")) {
		return Opener{}, false
	}
	info := ""
	if f := strings.Fields(m[3]); len(f) > 0 {
		info = strings.ToLower(f[0])
	}
	return Opener{Indent: m[1], Run: m[2], Info: info}, true
}

// Closes says whether a line ends this fence: a run of its character, at
// least as long, and nothing else.
func (f Opener) Closes(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= len(f.Run) && strings.Trim(t, f.Run[:1]) == ""
}

// labelled says the fence was declared a graph's; unlabelled, that its
// first line will have to say.
func (f Opener) labelled() bool   { return f.Info == "dot" || f.Info == "graphviz" }
func (f Opener) unlabelled() bool { return f.Info == "" }

// State is the transducer's half-finished work between two deltas: what
// has arrived and not been decided on, and the fence being captured. The
// hook keeps one per message id in a file.
type State struct {
	// Buf is raw delta text that has arrived and not yet been decided on:
	// an incomplete last line, which cannot be classified until its
	// newline shows up, because it might still grow into a fence marker.
	Buf string `json:"buf"`
	// Held is the fence being captured, raw, from its opening marker.
	Held string `json:"held"`
	// InFence is a fence being held: a graph's, or one not yet decided.
	InFence bool `json:"in_fence"`
	// Ours says the held fence is a graph's — by its label, or by its
	// first line. Until it is, the fence is held only as far as that line.
	Ours bool `json:"ours"`
	// Foreign is a fence that is not ours: its text streams as it
	// arrives, and only its closer is watched for.
	Foreign bool `json:"foreign"`
	// Fence is the opener of the fence being held or streamed.
	Fence Opener `json:"fence"`
	// A fence drawn before its closing ``` arrived leaves that marker
	// still in the stream, one delta behind. Nothing else knows it is
	// owed, so it is carried.
	PendingClose bool `json:"pending_close"`
	// Next is the index of the delta expected next: the turn.
	Next int `json:"next"`
}

// completeLines reports how many bytes of s form whole, newline-terminated
// lines. The rest is a fragment nothing can be decided about yet.
func completeLines(s string) int {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// findLine finds the first whole line in s that match accepts and returns
// its bounds, the end being past its newline. An unterminated last line is
// offered only at the end of a message: until then it may still be
// growing, and the closing ``` of a reply routinely arrives with no
// newline after it.
func findLine(s string, atEnd bool, match func(line string) bool) (start, end int) {
	for p := 0; p < len(s); {
		q := strings.IndexByte(s[p:], '\n')
		if q < 0 {
			if atEnd && match(s[p:]) {
				return p, len(s)
			}
			return -1, -1
		}
		if match(s[p : p+q]) {
			return p, p + q + 1
		}
		p += q + 1
	}
	return -1, -1
}

// openerLine is findLine for any fence opener, and the fence it read.
func openerLine(s string, atEnd bool) (start, end int, f Opener) {
	start, end = findLine(s, atEnd, func(line string) bool {
		var ok bool
		f, ok = OpenerOf(line)
		return ok
	})
	return start, end, f
}

// firstText is the first line of a body with anything on it, trimmed.
func firstText(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// Stream is the transducer. It takes one delta and the state left by the
// delta before it, and returns what should be displayed in its place. emit
// is the product: given one complete fence source and the cells its fence
// is indented by, it answers the rows that replace the fence, or nil to
// leave it exactly as it arrived; the rows come back standing in that
// indent. The hook passes drawBlock at the terminal's width; the oracle
// and the laws pass their own.
//
// The one law it must not break: **everything it emits is a substring of
// what it was handed.** An earlier version rebuilt lines with strings.Join
// and so inserted a newline wherever a delta boundary fell mid-line —
// invisible when the fence drew, and a corrupted message when it did not.
// The fallback is the slice it was given, never a reconstruction; -deltas
// is the oracle for it.
//
// Held text leaves in exactly two ways: as what emit made of it, or as the
// bytes it arrived as. There is no third exit, which is what keeps a suppressed
// delta from becoming a lost one.
//
// The context bounds the completeness question — which is a layout, and the
// most-asked one, being asked of every held fence on every delta. emit
// carries its own; the caller that supplies it has the same context in
// hand.
func Stream(ctx context.Context, delta string, final bool, st *State, emit func(src string, indent int) []string) string {
	var out strings.Builder
	st.Buf += delta
	closer := func() (int, int) { return findLine(st.Buf, final, st.Fence.Closes) }

	for {
		if st.PendingClose {
			s, e := closer()
			if s == 0 {
				// The drawing replaced the whole fence, closer included, so
				// the closer's own line ending is what decides whether the
				// text after it starts on a new row.
				if strings.HasSuffix(st.Buf[:e], "\n") {
					out.WriteString("\n")
				}
				st.Buf = st.Buf[e:]
				st.PendingClose = false
				continue
			}
			if s < 0 && !final && completeLines(st.Buf) == 0 {
				break // it has not arrived yet
			}
			st.PendingClose = false // whatever is here, it is not our closer
			continue
		}

		if st.Foreign {
			// Not ours: every whole line goes out as it came, and the
			// closer takes the fence with it.
			if s, e := closer(); s >= 0 {
				out.WriteString(st.Buf[:e])
				st.Buf = st.Buf[e:]
				st.Foreign = false
				continue
			}
			n := completeLines(st.Buf)
			if final {
				n = len(st.Buf)
			}
			out.WriteString(st.Buf[:n])
			st.Buf = st.Buf[n:]
			break
		}

		if !st.InFence {
			if s, e, f := openerLine(st.Buf, final); s >= 0 {
				out.WriteString(st.Buf[:s])
				st.Fence = f
				switch {
				case f.labelled() || f.unlabelled():
					st.InFence, st.Ours, st.Held = true, f.labelled(), st.Buf[s:e]
				default:
					out.WriteString(st.Buf[s:e])
					st.Foreign = true
				}
				st.Buf = st.Buf[e:]
				continue
			}
			n := completeLines(st.Buf)
			if final {
				n = len(st.Buf)
			}
			out.WriteString(st.Buf[:n])
			st.Buf = st.Buf[n:]
			break
		}

		// inside a held fence
		if !st.Ours {
			// Unlabelled: held only as far as its first line, which says
			// whether there is a graph in it, and never past its closer.
			// Until that line is whole there is nothing to decide on.
			n := completeLines(st.Buf)
			if final {
				n = len(st.Buf)
			}
			closed := false
			if s, _ := closer(); s >= 0 && s <= n {
				n, closed = s, true
			}
			st.Held += st.Buf[:n]
			st.Buf = st.Buf[n:]
			first := firstText(Body(st.Held, st.Fence))
			switch {
			case first != "" && graphStart.MatchString(first):
				st.Ours = true
				continue
			case first != "" || closed || final:
				// Prose, or nothing at all: not ours. What was held goes
				// out as it came, and the rest streams to the closer.
				out.WriteString(st.Held)
				st.InFence, st.Held = false, ""
				st.Foreign = true
				continue
			}
			break
		}
		if s, e := closer(); s >= 0 {
			st.Held += st.Buf[:e]
			st.Buf = st.Buf[e:]
			out.WriteString(replaceFence(st.Held, st.Fence, emit))
			st.InFence, st.Ours, st.Held = false, false, ""
			continue
		}
		st.Held += st.Buf
		st.Buf = ""

		// A complete graph does not need its closing ``` to be drawable:
		// graphviz is the completeness oracle, so a source that lays out
		// is a source that is finished. Emitting before the closer is what
		// makes a notice safe — a fence cut mid-graph does not lay out, so
		// the transducer keeps holding instead of answering "finished?"
		// with "yes, and here is why it is broken" on the first delta. A
		// source that is finished and wrong is told apart at the close,
		// where emit gets it whatever it is.
		if src := Body(st.Held, st.Fence); layout.Complete(ctx, src) {
			if rows := emitIn(st.Fence, src, emit); rows != nil {
				out.WriteString(strings.Join(rows, "\n"))
				st.InFence, st.Ours, st.Held = false, false, ""
				st.PendingClose = true
				continue
			}
		}
		if final {
			// The message is over and nothing drew. Give back every byte:
			// a visible DOT fence is the worst acceptable outcome, and
			// silence is not on the list.
			out.WriteString(st.Held)
			st.InFence, st.Ours, st.Held = false, false, ""
		}
		break
	}
	return out.String()
}

// emitIn is emit for a fence: the rows for its source at the width its
// indent leaves, each standing in that indent, so a drawing under a list
// item stays under it.
func emitIn(f Opener, src string, emit func(string, int) []string) []string {
	rows := emit(src, grid.Cells(f.Indent))
	for i := range rows {
		rows[i] = f.Indent + rows[i]
	}
	return rows
}

// replaceFence turns one closed fence into what emit makes of it, or hands
// back the exact bytes it was given.
func replaceFence(held string, f Opener, emit func(string, int) []string) string {
	rows := emitIn(f, Body(held, f), emit)
	if rows == nil {
		return held
	}
	block := strings.Join(rows, "\n")
	if strings.HasSuffix(held, "\n") {
		block += "\n"
	}
	return block
}

// Body is the source inside a held fence: the opener's line and the
// closer's, where it has arrived, taken off, and the opener's indent taken
// off every line that carries it, so a graph written under a list item
// reads as the model meant it.
func Body(held string, f Opener) string {
	lines := strings.Split(strings.TrimSuffix(held, "\n"), "\n")
	lines = lines[1:] // the opener, by construction
	if n := len(lines); n > 0 && f.Closes(lines[n-1]) {
		lines = lines[:n-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, f.Indent)
	}
	return strings.Join(lines, "\n")
}
