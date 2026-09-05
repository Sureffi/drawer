package drawer

// A fence does not arrive whole. CC splits a streamed reply into deltas at
// boundaries it does not promise and does not repeat: the same prompt gave
// ["```dot\n<source>\n", "```"] on one run and ["```dot\n", "<source>\n",
// "```"] on the next. A hook is one process per delta, so the transducer
// that reassembles the fence has to keep its half-finished work somewhere
// the next process can find it.
//
// And the processes are not one after another. Measured: the processes
// for a reply's two deltas started eleven microseconds apart and ran side
// by side, so the second read an empty state, saw no fence open and gave
// its half of the source back raw, while the first opened a fence nothing
// ever closed. So a process takes its turn: the state carries the index
// of the delta it expects next, a lock file per message serialises the
// readers, and a process whose delta is ahead of the count waits for the
// one before it — a few milliseconds, the draw included — before it reads.
// A turn nobody takes is waited for only so long, then taken anyway,
// which is the old behaviour and its old hazard.
//
// What that costs, stated plainly: a file per message id, and one hazard
// — suppressed deltas are content taken off the screen on the promise of
// putting something better back. If the promise
// is not kept the text is simply gone, so `flush` is the load-bearing
// half of this file, not the drawing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// A fence marker only counts at the start of its own line with nothing
// after it. Prose that mentions one is prose — sessions about this code
// are mostly that, and a scan for the substring swallowed the sentence it
// appeared in.
const FenceOpen = "```dot"
const FenceTick = "```"

// stateDir is where the hook keeps what one process leaves for the next.
func stateDir() string {
	dir := os.Getenv("DRAWER_STATE")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "drawer")
	}
	os.MkdirAll(dir, 0o700)
	return dir
}

// safeName keeps of an id only what cannot escape a directory. Message and
// session ids are uuids from CC.
func safeName(id string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '_'
	}, id)
}

func statePath(msgID string) string {
	return filepath.Join(stateDir(), safeName(msgID)+".json")
}

// sweepState drops files older than `age` — state left by turns that ended
// without a final delta: an abort mid-fence writes a file nothing will ever
// come back for. The cost of dropping a live one is a fence that shows its
// source, which is the failure this whole file is built to fall back to
// anyway. Directories are somebody else's and are left alone.
func sweepState(dir string, age time.Duration) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil || e.IsDir() || time.Since(info.ModTime()) < age {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

func loadState(msgID string) State {
	var s State
	p := statePath(msgID)
	sweepState(filepath.Dir(p), 10*time.Minute)
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	json.Unmarshal(b, &s)
	return s
}

// saveState keeps the state for the next delta's process, or at the
// message's final delta takes it away, lock and all.
func saveState(msgID string, s State, final bool) {
	if final {
		os.Remove(statePath(msgID))
		os.Remove(lockPath(msgID))
		return
	}
	b, _ := json.Marshal(s)
	os.WriteFile(statePath(msgID), b, 0o600)
}

func lockPath(msgID string) string { return statePath(msgID) + ".lock" }

// turnPatience is how long a process waits for the delta before its own.
// Claude Code allows a hook far longer, and a turn nobody is coming to
// take should not hold a reply that long.
const turnPatience = 2 * time.Second

// takeTurn waits for a delta's turn on its message and takes it: the
// state as the process before left it, under the message's lock, which
// the caller holds through the draw and releases with done. A delta the
// state has already counted past — a repeat — takes its turn at once.
func takeTurn(msgID string, index int, patience time.Duration) (State, func()) {
	f, err := os.OpenFile(lockPath(msgID), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return loadState(msgID), func() {}
	}
	deadline := time.Now().Add(patience)
	for {
		syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		st := loadState(msgID)
		if index <= st.Next || !time.Now().Before(deadline) {
			return st, func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
		}
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		time.Sleep(3 * time.Millisecond)
	}
}

// State is the transducer's half-finished work between two deltas: what
// has arrived and not been decided on, and the fence being captured. The
// hook keeps one per message id in a file; a caller that owns its own
// process keeps it wherever it likes.
type State struct {
	// Buf is raw delta text that has arrived and not yet been decided on:
	// an incomplete last line, which cannot be classified until its
	// newline shows up, because it might still grow into a fence marker.
	Buf string `json:"buf"`
	// Held is the fence being captured, raw, from its opening marker.
	Held    string `json:"held"`
	InFence bool   `json:"in_fence"`
	// A fence reserved before its closing ``` arrived leaves that marker
	// still in the stream, one delta behind. Nothing else knows it is
	// owed, so it is carried — the category of things kept precisely
	// because no screen can derive them.
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

// markerLine finds the first whole line in s that is exactly marker and
// returns its bounds, the end being past its newline. An unterminated last
// line matches only at the end of a message: until then it may still be
// growing, and the closing ``` of a reply routinely arrives with no
// newline after it.
func markerLine(s, marker string, atEnd bool) (start, end int) {
	for p := 0; p < len(s); {
		q := strings.IndexByte(s[p:], '\n')
		if q < 0 {
			if atEnd && strings.TrimRight(s[p:], " \t\r") == marker {
				return p, len(s)
			}
			return -1, -1
		}
		if strings.TrimRight(s[p:p+q], " \t\r") == marker {
			return p, p + q + 1
		}
		p += q + 1
	}
	return -1, -1
}

// Stream is the transducer. It takes one delta and the state left by the
// delta before it, and returns what should be displayed in its place. emit
// is the product: given one complete fence source it answers the rows that
// replace the fence, or nil to leave it exactly as it arrived. The drawer
// passes Draw; an embedding program passes its own.
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
func Stream(delta string, final bool, st *State, emit func(src string) []string) string {
	var out strings.Builder
	st.Buf += delta

	for {
		if st.PendingClose {
			s, e := markerLine(st.Buf, FenceTick, final)
			if s == 0 {
				// The region replaced the whole fence, closer included, so
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

		if !st.InFence {
			if s, e := markerLine(st.Buf, FenceOpen, final); s >= 0 {
				out.WriteString(st.Buf[:s])
				st.InFence, st.Held = true, st.Buf[s:e]
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

		// inside a fence
		if s, e := markerLine(st.Buf, FenceTick, final); s >= 0 {
			st.Held += st.Buf[:e]
			st.Buf = st.Buf[e:]
			out.WriteString(replaceFence(st.Held, emit))
			st.InFence, st.Held = false, ""
			continue
		}
		st.Held += st.Buf
		st.Buf = ""

		// A complete graph does not need its closing ``` to be drawable:
		// graphviz is the completeness oracle, so a source that lays out
		// is a source that is finished. Emitting before the closer is what
		// makes a notice safe — a fence cut mid-graph does not lay out, so
		// the transducer keeps holding instead of answering "finished?"
		// with "yes, and here is why it is broken" on the first delta.
		if src := fenceBody(st.Held); complete(src) {
			if rows := emit(src); rows != nil {
				out.WriteString(strings.Join(rows, "\n"))
				st.InFence, st.Held = false, ""
				st.PendingClose = true
				continue
			}
		}
		if final {
			// The message is over and nothing drew. Give back every byte:
			// a visible DOT fence is the worst acceptable outcome, and
			// silence is not on the list.
			out.WriteString(st.Held)
			st.InFence, st.Held = false, ""
		}
		break
	}
	return out.String()
}

// complete says whether a source is a whole graph: graphviz reads it, and
// there is a graph in it. A fence still streaming fails here, and so does
// one that is finished and wrong — the second is told apart at the close,
// when emit gets the source whatever it is.
func complete(src string) bool {
	l, err := layoutDOT(src, "")
	return err == nil && l != nil
}

// replaceFence turns one closed fence into what emit makes of it, or hands
// back the exact bytes it was given.
func replaceFence(held string, emit func(string) []string) string {
	rows := emit(fenceBody(held))
	if rows == nil {
		return held
	}
	block := strings.Join(rows, "\n")
	if strings.HasSuffix(held, "\n") {
		block += "\n"
	}
	return block
}

// fenceBody strips the opening marker and any closing one from held text.
func fenceBody(held string) string {
	lines := strings.Split(strings.TrimSuffix(held, "\n"), "\n")
	if len(lines) > 0 && strings.TrimRight(lines[0], " \t") == FenceOpen {
		lines = lines[1:]
	}
	if n := len(lines); n > 0 && strings.TrimRight(lines[n-1], " \t") == FenceTick {
		lines = lines[:n-1]
	}
	return strings.Join(lines, "\n")
}
