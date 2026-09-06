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
// tmux only; Name is asked once by the run every door is built from.
//
// Where tmux will not answer — no client attached, no tmux on the PATH — the
// answer is empty and TERM, tmux's own, stands. That names no terminal and
// lands on the glyph rungs, which is the safe direction: a wrong pixel rung
// is tofu, a wrong glyph rung is still a drawing.

package term

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// env is one variable as this process's parent chain has it: the hook's own
// environment first, then claude's, which is where anything CC scrubbed is
// still readable. A variable somebody set to nothing is an answer and stops
// there — only one that is not there at all is asked of the parent.
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

// TmuxPane is the pane the parent is in, for a command scoped to it.
func TmuxPane() string { return env("TMUX_PANE") }

// muxTimeout bounds the one question this file asks. tmux answers on a
// local socket in single-digit milliseconds or it is not answering, and a
// hook waiting on it is CC not painting.
const muxTimeout = 2 * time.Second

// askTmux is the one command this file runs, as a variable so a law can
// stand a tmux up without one. Read-only: display-message -p prints a
// format and changes nothing.
var askTmux = func(socket string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), muxTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux",
		append([]string{"-S", socket}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// behindTmux names the terminal a pane is really in: the TERM of the client
// attached to it, as tmux reports it. Empty where there is no pane to ask
// about, no socket to ask on, or no client attached — and then TERM, tmux's
// own, is still the best answer there is.
func behindTmux() string {
	sock, pane := TmuxSocket(), TmuxPane()
	if sock == "" || pane == "" {
		return ""
	}
	return askTmux(sock, "display-message", "-p", "-t", pane, "#{client_termname}")
}
