// tmux.go — the shape a graphics escape has to have to get past tmux.
//
// tmux reads everything an application writes and forwards only what it
// understands; a kitty graphics APC is not on that list and is dropped
// without a word. The one way through is tmux's own passthrough — the
// escape wrapped in a DCS tmux; … ST, with every ESC inside it doubled,
// which tmux unwraps and writes to the terminal verbatim. Measured on the
// rig 2026-09-06: bare, the 109-byte APC reaches the pane and the picture
// never appears; wrapped, the same escape is 120 bytes and the graph draws
// in its placeholder rows, streaming and settled alike, on ghostty and
// kitty both.
//
// Only the escape is wrapped. The placeholder cells are ordinary text and
// go through CC's display wire as they always did; a cursor move or a colour
// inside the wrap would be tmux's screen being written behind tmux's back.
//
// The fact that there is a tmux at all is term's — this file is only the
// bytes, and the one command that lets them past.

package pixel

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/sureffi/drawer/internal/term"
)

// Tmux is the multiplexer between a picture and the screen: which server,
// and which pane the parent is in. A nil *Tmux is "there is no
// multiplexer", and every method here reads that as nothing to do, so a
// caller has one path whether or not tmux is in the way.
//
// What the pane answered about passthrough is kept here, because the answer
// cannot change under a process that is the only one writing it: one hook
// draws a picture and may repaint a whole ledger of them, and each of those
// used to fork a tmux to ask the same question over again.
type Tmux struct {
	Socket, Pane string

	asked bool   // whether this process has run `show` yet
	was   string // what it said
	err   error  // or why it could not be asked at all

	ownAsked bool   // whether this pane's own value has been read
	own      string // what the pane itself was set to, nothing inherited
	ownErr   error
}

// errNoPane is a tmux this process cannot name a pane in. TMUX_PANE is set
// in every pane tmux owns, but a shell that inherited TMUX and not it — a
// tmux run from a script, a login shell started by hand inside one — leaves
// TMUX set and the pane unknown. Measured: `tmux set -p -t "" allow-passthrough
// on` does not fail there, it lands on whatever pane tmux happens to pick,
// and it picked one in another session. So an unnamed pane is an error and
// not an empty answer: this is somebody else's tmux, and the one write
// drawer makes in it goes to the pane claude is in or to no pane at all.
var errNoPane = errors.New("TMUX_PANE names no pane to ask about")

// Multiplexer is the tmux this process is under, or nil.
func Multiplexer() *Tmux {
	if !term.Tmux() {
		return nil
	}
	return &Tmux{Socket: term.TmuxSocket(), Pane: term.TmuxPane()}
}

// wrap puts an escape inside tmux's passthrough. Doubling ESC is not
// decoration: tmux ends the passthrough at the first ESC it finds, so a
// single one would truncate the picture to its own first byte.
func (t *Tmux) wrap(esc string) string {
	if t == nil {
		return esc
	}
	return "\x1bPtmux;" + strings.ReplaceAll(esc, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// tmuxTimeout bounds a call to somebody else's tmux. It answers in
// milliseconds or it is not answering; a hook that waited on it would be
// holding up CC's own display.
const tmuxTimeout = 2 * time.Second

// tmuxWaitDelay bounds the wait after that: a deadline kills the process,
// but Wait goes on reading the pipes until the last writer closes them, and
// a tmux that forks a child holding the one it inherited never has a last
// writer. Measured on eefabb0, with such a wrapper on the PATH: a 2 s
// deadline took 20 s to come back and the hook path stalled a minute. The
// delay is a quarter of the deadline because it starts only where the
// command is already over — the answer is late by then whatever it says.
const tmuxWaitDelay = tmuxTimeout / 4

// Passthrough is whether this pane lets a passthrough through: "on", "off",
// or whatever else tmux says. -A asks for the value in force rather than
// the one set on the pane, because an option nobody set on the pane reads
// as empty while the server's own answer is the one that counts.
//
// The error is the other answer, and it is not the same as an empty one: a
// tmux that could not be asked at all — no server on that socket, no tmux
// on the PATH — knows nothing about this pane, and reading its silence as
// "no option set" is how a closed wire came to read as an open one.
//
// Asked once, then remembered: a fork and a socket round-trip per drawn
// picture is a cost a hook pays in front of CC's own painting, and the
// value cannot move underneath a process that is the only one setting it.
func (t *Tmux) Passthrough(ctx context.Context) (string, error) {
	if t == nil {
		return "", nil
	}
	if !t.asked {
		t.asked = true
		if t.Pane == "" {
			t.err = errNoPane
		} else {
			out, err := t.run(ctx, "show", "-p", "-t", t.Pane, "-A", "-v", "allow-passthrough")
			t.was, t.err = strings.TrimSpace(out), err
		}
	}
	return t.was, t.err
}

// Allow turns the pane's passthrough on where it is not on already,
// because tmux drops a passthrough nobody allowed and the picture is then
// simply gone — no error, no cells missing, nothing to see. Two answers:
// what it found and did, for the tee, and whether an escape can get
// through afterwards. False is the fail-open signal the rung above needs:
// a wire this one knows is closed is a wire to draw glyphs down instead of
// leaving a reader eight blank rows.
//
// Pane-scoped, on purpose and without an option to do otherwise: this is
// somebody else's tmux and drawer is a guest in it. The change reaches the
// pane claude is running in and no other, it is never written to a file,
// the server's own setting is left where it was, and it dies with the pane.
// The policy, in three sentences. The value in force says whether an escape
// gets through, and "on" and "all" both say yes — "all" is the more
// permissive of the two, not the lesser, so a pane already sitting on it is
// left exactly as it is. Anything else is asked again without -A, which
// answers with what this pane itself was set to and nothing inherited from
// the server, and a pane whose own value is "off" is a reader who said no
// here: drawer leaves it alone and draws in glyphs. Only a pane with no
// answer of its own is written to, and what it is written is "on".
//
// The note is empty where it was already through and there was nothing to
// do, because a note about nothing is noise — and a second picture in the
// same process finds it on, because setting it is what this wrote down.
func (t *Tmux) Allow(ctx context.Context) (string, bool) {
	if t == nil {
		return "", true
	}
	pane := t.paneIn()
	was, err := t.Passthrough(ctx)
	if err != nil {
		// A tmux nobody could ask is a wire nobody can post a picture
		// through. It read as open once — run() gave back the same empty
		// string for "exited 0, said nothing" and for "never ran at all",
		// and a box with no tmux on the PATH went out expecting pixels
		// that had nowhere to come from.
		return "tmux: allow-passthrough could not be asked about" + pane +
			": " + reason(was, err), false
	}
	if was == "on" || was == "all" {
		return "", true
	}
	own, err := t.PaneOption(ctx)
	if err != nil {
		return "tmux: allow-passthrough is " + quoted(was) + pane +
			" and this pane's own value could not be read: " + reason(own, err), false
	}
	if own == "off" {
		return "tmux: allow-passthrough is off" + pane +
			", set there and not inherited from the server: a reader said no in this" +
			" pane, so drawer leaves it and draws in glyphs", false
	}
	if out, err := t.allow(ctx); err != nil {
		return "tmux: allow-passthrough is " + quoted(was) + pane +
			" and would not be set: " + reason(out, err), false
	}
	t.was, t.err = "on", nil
	return "tmux: allow-passthrough was " + quoted(was) + "; set on" + pane +
		" (this pane only, until it closes)", true
}

// PaneOption is what this pane itself was set to, with nothing inherited
// from the server: "off" from here is a reader who said no in this pane,
// and empty is a pane nobody has said anything about either way. -A is
// deliberately absent — the value in force is the other question, and
// Passthrough asks it.
//
// Asked once and remembered, like the other one, and asked at all only on
// the path that would otherwise write: a pane already through costs no
// exec here.
func (t *Tmux) PaneOption(ctx context.Context) (string, error) {
	if t == nil {
		return "", nil
	}
	if !t.ownAsked {
		t.ownAsked = true
		if t.Pane == "" {
			t.ownErr = errNoPane
		} else {
			out, err := t.run(ctx, "show", "-p", "-t", t.Pane, "-v", "allow-passthrough")
			t.own, t.ownErr = strings.TrimSpace(out), err
		}
	}
	return t.own, t.ownErr
}

// paneIn names the pane a note is about, and says so where there is none to
// name.
func (t *Tmux) paneIn() string {
	if t.Pane == "" {
		return " for a pane TMUX_PANE did not name"
	}
	return " for pane " + t.Pane
}

// quoted names a value a reader has to be able to tell from nothing at all.
func quoted(v string) string {
	if v == "" {
		return "unset"
	}
	return v
}

// reason is what to put in the note: what tmux said, or where it failed
// when it said nothing.
func reason(out string, err error) string {
	if out != "" {
		return out
	}
	return err.Error()
}

// allow is the one write this package makes to anything but a tty: what
// tmux said about it, and the error where it could not be told at all.
func (t *Tmux) allow(ctx context.Context) (string, error) {
	if t.Pane == "" {
		return "", errNoPane
	}
	out, err := t.run(ctx, "set", "-p", "-t", t.Pane, "allow-passthrough", "on")
	return strings.TrimSpace(out), err
}

// run is one tmux command against this server. The socket is addressed
// directly rather than through TMUX, so the answer is about this server
// whatever environment the command inherits.
//
// Two answers, and they are not one: what tmux said, and whether it ran at
// all. A tmux that exited 0 and printed nothing is an answer of nothing; a
// tmux that is not on the PATH is no answer, and the two read the same
// until the error is carried out with the output.
//
// The deadline is derived from the caller's rather than started fresh. A
// hook has one deadline for the whole of what it draws, and a question put
// to somebody else's tmux on a clock of its own is time the drawing does
// not get: measured, three of twenty fences came back as "ran out of time"
// notices because tmux had spent the layout's budget.
func (t *Tmux) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, tmuxTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-S", t.Socket}, args...)...)
	cmd.WaitDelay = tmuxWaitDelay
	out, err := cmd.CombinedOutput()
	return string(out), err
}
