// tmux.go — the shape a graphics escape has to have to get past tmux, and
// what has to be true of the pane for tmux to forward it.
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
// The fact that there is a tmux at all is term's — this file is the bytes,
// and the two questions and one write that decide whether they get through.

package pixel

import (
	"context"
	"errors"
	"fmt"
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
// What this process asked, found and did about passthrough is kept here,
// because none of it can change under a process that is the only one
// writing it: one hook draws a picture and may repaint a whole ledger of
// them, and each of those used to fork a tmux to ask the same questions
// over again and write the same line into the tee.
type Tmux struct {
	Socket, Pane string

	asked bool   // whether this process has run `show -A` yet
	was   string // the value in force it said
	err   error  // or why it could not be asked at all

	ownAsked bool   // whether this pane's own value has been read
	own      string // what the pane itself was set to, nothing inherited
	ownErr   error

	tried  bool  // whether this process has tried to set it
	setErr error // and how that went

	noted bool // whether the tee already has this process's note
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

// Passthrough is the allow-passthrough in force for this pane: "on",
// "all", "off", or whatever else tmux says. -A is what makes it the value
// in force rather than the pane's own — an option nobody set on the pane
// reads as empty, and what governs it then is the server's. PaneOption is
// the other question, and Allow says when each of them is asked.
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
//
// The policy, in three sentences. The value in force says whether an escape
// gets through, and "on" and "all" both say yes — "all" is the more
// permissive of the two, not the lesser, so a pane already sitting on it is
// left exactly as it is. Anything else is asked again without -A, which
// answers with what this pane itself was set to and nothing inherited from
// the server. Only a pane with no value of its own is written to, and what
// it is written is "on": a pane carrying any value at all — "off" from a
// reader who said no here, or a word this version of tmux spells and this
// one does not know — is left exactly where it is, which makes it a closed
// wire and draws the graph in glyphs.
//
// The note is empty where it was already through and there was nothing to
// do, because a note about nothing is noise — and it is written once
// whatever happened, because a hook draws a picture and may repaint a
// ledger of them behind it, and the tee is a fixture a replay reads and
// not a log.
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
		return t.note("tmux: allow-passthrough could not be asked about" + pane +
			": " + reason(err)), false
	}
	if was == "on" || was == "all" {
		return "", true
	}
	own, err := t.PaneOption(ctx)
	if err != nil {
		return t.note("tmux: allow-passthrough is " + Quoted(was) + pane +
			" and this pane's own value could not be read: " + reason(err)), false
	}
	if own != "" {
		// Not only "off": the sentence above says a pane with no value of
		// its own, and a value nobody here recognises is still a value
		// somebody set. Overwriting one drawer cannot read is the same
		// trespass as overwriting one it can.
		return t.note("tmux: allow-passthrough is " + own + pane +
			", set there and not inherited from the server: drawer leaves a pane's" +
			" own value alone and draws in glyphs"), false
	}
	if !t.tried {
		t.setErr, t.tried = t.allow(ctx), true
	}
	if t.setErr != nil {
		return t.note("tmux: allow-passthrough is " + Quoted(was) + pane +
			" and would not be set: " + reason(t.setErr)), false
	}
	t.was, t.err = "on", nil
	return t.note("tmux: allow-passthrough was " + Quoted(was) + "; set on" + pane +
		" (this pane only, until it closes)"), true
}

// Through says whether a picture would cross this wire, changing nothing:
// the judgement Allow makes, without the write. The doctor asks it so that
// its rung line names what this wire gets and not what the terminal could
// show — measured on the rig: a pane that said off drew glyphs while the
// doctor said pixels, and a reader who greps rung: got the wrong answer.
// The hook never asks it, because the hook is what writes. A pane with no
// value of its own answers yes here, which is the hook's own reading: that
// is the pane it would set on.
func (t *Tmux) Through(ctx context.Context) bool {
	if t == nil {
		return true
	}
	was, err := t.Passthrough(ctx)
	if err != nil {
		return false
	}
	if was == "on" || was == "all" {
		return true
	}
	own, err := t.PaneOption(ctx)
	return err == nil && own == ""
}

// note hands back something worth writing down, once. Every failing path
// used to hand its note back on every call, and a hook that drew a picture
// and repainted a ledger behind it put four identical lines about the same
// tmux into the tee — measured. The wire is asked once and answered once,
// so it is said once.
func (t *Tmux) note(s string) string {
	if t.noted {
		return ""
	}
	t.noted = true
	return s
}

// PaneOption is what this pane itself was set to, with nothing inherited
// from the server: "off" from here is a reader who said no in this pane,
// empty is a pane nobody has said anything about either way, and anything
// else is still this pane's own answer and not drawer's to write over. -A
// is deliberately absent — the value in force is the other question, and
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

// Quoted names an option value a reader has to be able to tell from a value
// nobody set. Exported because -doctor prints the same values this file
// puts in its notes, and one spelling of "unset" is enough.
func Quoted(v string) string {
	if v == "" {
		return "unset"
	}
	return v
}

// reason is what to put in the note when tmux would not answer: the error,
// which run has already folded tmux's own complaint into. Not stdout — that
// is the option's value, and a tmux that failed did not give one.
func reason(err error) string { return term.Printable(err.Error()) }

// allow is the one change this package makes to somebody else's state.
// (Not the one write: Send lays a picture in a temp file for the terminal
// to collect, and sweepPictures unlinks the ones it never did.) There is no
// value to bring back — `set` says nothing when it works — so the only
// answer is whether it could be told at all.
func (t *Tmux) allow(ctx context.Context) error {
	if t.Pane == "" {
		return errNoPane
	}
	_, err := t.run(ctx, "set", "-p", "-t", t.Pane, "allow-passthrough", "on")
	return err
}

// run is one tmux command against this server. The socket is addressed
// directly rather than through TMUX, so the answer is about this server
// whatever environment the command inherits.
//
// Two answers, and they are not one: what tmux said, and whether it ran at
// all. A tmux that exited 0 and printed nothing is an answer of nothing; a
// tmux that is not on the PATH is no answer, and the two read the same
// until the error is carried out with the output. Both are bounded and made
// printable: this is somebody else's program writing onto a reader's
// screen by way of a note.
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
	var out, said capped
	cmd.Stdout, cmd.Stderr = &out, &said
	err := cmd.Run()
	if err != nil {
		// tmux says why on stderr, and that belongs in the note beside the
		// error. It does not belong in the option's value: the two were one
		// string here, so a tmux that complained about something else
		// entirely could be read back as the pane's answer.
		if s := term.Printable(said.String()); s != "" {
			err = fmt.Errorf("%w: %s", err, s)
		}
	}
	return term.Printable(out.String()), err
}

// tmuxMaxOut bounds what tmux is allowed to say here, for the reason
// term's own bound exists: this is another program's output on its way to
// a reader's screen. An option's value is three bytes.
const tmuxMaxOut = 4 << 10

// capped is a buffer that stops, and tells the writer it did not: a tmux
// answering with a megabyte finishes rather than dying on a closed pipe,
// and what is past the bound is dropped.
type capped struct{ b []byte }

func (c *capped) Write(p []byte) (int, error) {
	if n := tmuxMaxOut - len(c.b); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		c.b = append(c.b, p[:n]...)
	}
	return len(p), nil
}

func (c *capped) String() string { return string(c.b) }
