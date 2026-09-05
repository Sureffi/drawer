package drawer

// The native way in. CC ships a MessageDisplay hook that hands us the
// text it is about to draw and takes back a replacement, so a ```dot
// fence can become a drawing without anything being rewritten upstream
// of CC at all.
//
// A hook stands upstream of CC's layout and downstream of the model, which
// is the one place where the source is known and the text is still free:
// nothing can be invented downstream of the layout, so whatever replaces a
// fence has to be made here.
//
// Everything here is fail-open: whatever we cannot draw is handed back as
// the exact bytes we were given, and any error at all leaves the delta
// alone. A hook that fails must be
// invisible, because the thing it is failing at is CC's own display.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/sureffi/drawer/internal/fence"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
)

// hookIn is CC's payload. Only the fields we use are named; the rest of
// the key set (transcript_path, cwd, prompt_id, turn_id, hook_event_name)
// is measured and deliberately unread — a hook that binds to more of the
// payload than it needs is a hook that breaks on more of CC's changes than
// it has to. The session id keys the ledger of pictures drawn.
type hookIn struct {
	Delta     string `json:"delta"`
	Final     bool   `json:"final"`
	MessageID string `json:"message_id"`
	Index     int    `json:"index"`
	SessionID string `json:"session_id"`
}

// The output shape is nested and the nesting is load-bearing: a flat
// {"displayContent": ...} is accepted, parsed, and silently ignored —
// no error on screen, no entry in the hook log, the original simply
// displays. The binary carries the check as
// `hookSpecificOutput is missing required field "hookEventName"`.
type hookOut struct {
	Specific hookSpecific `json:"hookSpecificOutput"`
}

type hookSpecific struct {
	HookEventName  string `json:"hookEventName"`
	DisplayContent string `json:"displayContent"`
}

// CC's own left margin for a message body.
const inset = 6

// hookWidth is the room a message body has in a window that many columns
// wide. The window is the terminal's to describe; the margin is CC's.
func hookWidth(cols int) int {
	if cols > inset {
		return cols - inset
	}
	return cols
}

// teePayload writes a live turn down, one payload per line, in exactly the
// shape -deltas reads. One armed session therefore produces a fixture
// rather than a log. Where it writes is -hooktee on the command line, or
// DRAWER_TEE in the environment: a variable set for claude — the shell's,
// or settings.json's `env` — reaches a hook (measured on 2.1.261; an
// earlier measurement said the environment was scrubbed, and on 2.1.257
// it was not either way that mattered here).
func (r run) teePayload(raw []byte) {
	if r.tee == "" {
		return
	}
	f, err := os.OpenFile(r.tee, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(bytes.TrimRight(raw, "\n"), '\n'))
	f.Close()
}

// runHook is the whole of the -hook entry point: one payload in, one
// replacement out, through drawBlock. Any failure prints an empty object,
// which CC reads as "display the original".
func (r run) runHook() int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Print("{}")
		return 0
	}
	r.teePayload(raw)
	var in hookIn
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Print("{}")
		return 0
	}
	r.sess = in.SessionID
	width := hookWidth(r.probe())
	// The first delta of a message is the first thing the hook hears after
	// a theme switch; ledger.go says why, and what is repainted.
	if in.Index == 0 && r.pickRung() == rungPixels {
		r.repaintPictures(term.TTYOut(), pixel.Probe())
	}
	st, done := takeTurn(in.MessageID, in.Index, turnPatience)
	defer done()
	before := st
	text := fence.Stream(in.Delta, in.Final, &st, func(src string, indent int) []string {
		return r.drawBlock(src, width-indent)
	})
	st.Next = max(st.Next, in.Index+1)
	saveState(in.MessageID, st, in.Final)
	// Nothing held, nothing drawn, nothing suppressed: let CC display its
	// own delta rather than handing back a copy of it.
	if !before.InFence && !before.PendingClose && before.Buf == "" &&
		!st.InFence && !st.PendingClose && st.Buf == "" && text == in.Delta {
		fmt.Print("{}")
		return 0
	}
	json.NewEncoder(os.Stdout).Encode(hookOut{Specific: hookSpecific{
		HookEventName:  "MessageDisplay",
		DisplayContent: text,
	}})
	return 0
}

// sessionContext is what a model should know at the start of a session, for a
// SessionStart hook to hand it: that a ```dot fence in a reply is drawn in
// place, and what this terminal's rung can draw, which is what a graph
// should be written for. Without it a model that has never heard of the
// hook writes mermaid, or boxes out of hyphens, and neither is a picture.
func (r run) sessionContext() string {
	r.probe()
	var can string
	switch r.pickRung() {
	case rungPixels:
		can = "as graphviz's own picture, so everything dot draws, draws"
	case rungOctants, rungBraille:
		can = "in strokes: clusters, node shapes, multi-line labels and dashed edges draw; record and HTML labels print their markup, and node colours are not painted"
	default:
		can = "in box-drawing characters: boxes with one-line labels and routed edges; clusters and node shapes do not draw"
	}
	return "drawer: a ```dot fence in a reply is drawn in place, " + can +
		". For any diagram, write graphviz DOT in a ```dot fence, not mermaid and not ASCII art. " +
		"Up to about a dozen nodes it draws as written; past that it goes top-down and tall.\n"
}
