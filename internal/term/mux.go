// mux.go — tmux, between the parent and the terminal.
//
// tmux is the one thing in this wire that both hides the terminal and holds
// its escapes back, so the pixels rung needs two facts about it: that it is
// there, and which terminal is behind it. That it is there is read out of
// the environment. Which terminal is behind it is asked of tmux — read-only,
// one exec, never a byte down the tty, because a hook writes to a tty it
// cannot read a reply from and a probe that needs an answer is not available
// on this path at all.
//
// Measured 2026-09-06, tmux 3.7b on archbox, kitty 0.48.1 and ghostty
// 1.3.1-arch2, in an attached pane:
//
//   - a pane's TERM is tmux's own, tmux-256color, and so is TERM_PROGRAM,
//     which tmux sets to "tmux" over ghostty's own "ghostty". Neither names
//     the terminal any more.
//   - an attached pane's TIOCGWINSZ carries the real window's pixel size:
//     kitty at 100x40 answered 1000x880 through tmux exactly as it did
//     without it, so the cell size the pixels rung cuts to is unchanged. (A
//     detached session answers 1280x768 for 80x24, which is nobody's cell;
//     the parent of a hook is an attached claude, so that answer is not on
//     this path.)
//
// The mark is not the terminal. kitty's KITTY_WINDOW_ID and ghostty's
// GHOSTTY_RESOURCES_DIR do survive into a pane — but they are the tmux
// SERVER's environment, and a server keeps the environment it was born in
// for every client that ever attaches to it. Measured 2026-09-06: a tmux
// server started in a kitty, attached from an alacritty, handed the
// alacritty KITTY_WINDOW_ID, and drawer drew 576 tofu boxes where the
// picture was. Under tmux the marks are not read at all.
//
// What is read instead is tmux's own answer about the client attached to
// this pane:
//
//	tmux -S <socket> display-message -p -t <pane> '#{client_termname}'
//
// which is that client's TERM — "xterm-kitty" from a kitty, "alacritty" from
// an alacritty — so everything downstream reads one kind of name and nothing
// downstream has to know a multiplexer exists. One exec per process, under
// tmux only; Name is asked once by the run every door is built from, and the
// name is carried in the run from there rather than asked again.
//
// Where tmux will not answer — no client attached, no tmux on the PATH — the
// answer is empty and TERM, tmux's own, stands. That names no terminal and
// lands on the glyph rung, which is the safe direction: a wrong pixel rung
// is tofu, a wrong glyph rung is still a drawing.

package term

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// env is one variable as this process's parent chain has it: the hook's own
// environment first, then claude's, which is where anything CC scrubbed is
// still readable. A variable somebody set to nothing is an answer and stops
// there — only one that is not there at all is asked of the parent.
//
// That rule is TMUX's and TMUX_PANE's, which is all this file asks for.
// TERM's is the other one and lives in Name: a TERM set to nothing names
// no terminal, so a blank one falls through to the parent as an absent one
// does.
func env(key string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return parentEnv(key)
}

// Tmux says whether tmux stands between the parent and the terminal. TMUX
// is set in every pane tmux owns and nowhere else.
func Tmux() bool { return env("TMUX") != "" }

// TmuxSocket is the server's socket, the first field of TMUX. Addressing
// the server by socket rather than by the variable means a command run
// from here reaches this server whatever its own environment says.
func TmuxSocket() string {
	s, _, _ := strings.Cut(env("TMUX"), ",")
	return s
}

// TmuxPane is the pane the parent is in, for a command scoped to it. It is
// made printable on the way out because it is put on a line a reader reads
// — the tee's note, -doctor's tmux fact — as well as on a command line.
func TmuxPane() string { return Printable(env("TMUX_PANE")) }

// printableMax is a line's worth. A terminal's name is a dozen bytes and an
// option's value is three; anything past this is not an answer to the
// question that was asked.
const printableMax = 200

// Printable is a string another program said, made fit to print. tmux hands
// back whatever the client's TERM or the pane's option happens to hold, and
// that goes into a note, onto -doctor's terminal: line, and through CC's
// display wire: measured, a stub answering an OSC title, a clear-screen and
// a kitty graphics escape had -doctor run all three against the reader's
// terminal. Control bytes and escapes are dropped rather than shown,
// because there is nothing in them a reader asked for, and what is left is
// cut to a line.
func Printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > printableMax {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// muxMaxOut bounds what tmux is allowed to say. This is another program's
// stdout arriving in a hook's memory: measured, a stub that answered with
// 256 MB had drawer hold and print 536 MB of it. Four kilobytes is a
// thousand times more than any answer this asks for.
const muxMaxOut = 4 << 10

// capped is a buffer that stops. What is past the bound is dropped and not
// kept, and the writer is still told it all went in, so a tmux that answers
// with a megabyte finishes rather than dying on a closed pipe.
type capped struct{ b []byte }

func (c *capped) Write(p []byte) (int, error) {
	if n := muxMaxOut - len(c.b); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		c.b = append(c.b, p[:n]...)
	}
	return len(p), nil
}

func (c *capped) String() string { return string(c.b) }

// muxTimeout bounds the one question this file asks. tmux answers on a
// local socket in single-digit milliseconds or it is not answering, and a
// hook waiting on it is CC not painting.
const muxTimeout = 2 * time.Second

// muxWaitDelay bounds what is left after the deadline: killing the process
// does not close the pipe a child of it is still holding, and Wait reads
// that pipe until somebody does. A tmux wrapper that forks is the usual
// shape, and measured on eefabb0 it turned this two-second question into a
// twenty-second one. The delay runs from the command being over, so it
// costs nothing on a tmux that answers.
const muxWaitDelay = muxTimeout / 4

// askTmux is the one command this file runs, as a variable so a law can
// stand a tmux up without one. Read-only: display-message -p prints a
// format and changes nothing. What it says is bounded on the way in and
// made printable on the way out: it is another program's stdout, and it
// ends up on a reader's screen. Its stderr is nowhere, because the only
// answer this has for a tmux that failed is the empty one.
//
// The deadline is derived from the caller's, not from nothing: this is
// another process on the far side of a socket, and a question asked on its
// own clock is a question that can outlive the drawing it was asked for.
// Two seconds is what tmux gets, and never more than what is left of the
// process.
var askTmux = func(ctx context.Context, socket string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, muxTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux",
		append([]string{"-S", socket}, args...)...)
	cmd.WaitDelay = muxWaitDelay
	var out capped
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return Printable(strings.TrimSpace(out.String()))
}

// behindTmux names the terminal a pane is really in: the TERM of the client
// attached to it, as tmux reports it. Empty where there is no pane to ask
// about, no socket to ask on, or no client attached — and then TERM, tmux's
// own, is still the best answer there is.
func behindTmux(ctx context.Context) string {
	sock, pane := TmuxSocket(), TmuxPane()
	if sock == "" || pane == "" {
		return ""
	}
	return askTmux(ctx, sock, "display-message", "-p", "-t", pane, "#{client_termname}")
}
