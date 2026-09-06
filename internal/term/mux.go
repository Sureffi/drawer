// mux.go — tmux, between the parent and the terminal.
//
// tmux is the one thing in this wire that both hides the terminal and holds
// its escapes back, so the pixels rung needs two facts about it: that it is
// there, and which terminal is behind it. Both are read out of the
// environment and never asked for down the wire — a hook writes to a tty it
// cannot read a reply from, so a probe that needs an answer is not
// available here at all.
//
// Measured 2026-09-06, tmux 3.7b on archbox, kitty 0.48.1 and ghostty
// 1.3.1-arch2, in an attached pane:
//
//   - a pane's TERM is tmux's own, tmux-256color, and so is TERM_PROGRAM,
//     which tmux sets to "tmux" over ghostty's own "ghostty". Neither names
//     the terminal any more.
//   - the terminal's mark survives: kitty's KITTY_WINDOW_ID and ghostty's
//     GHOSTTY_RESOURCES_DIR both arrive in a pane of a server that terminal
//     started. ghostty does NOT set KITTY_WINDOW_ID — a ghostty started from
//     inside a kitty merely inherits it, which is how that got believed —
//     so the two marks are read apart and neither stands for the other.
//   - an attached pane's TIOCGWINSZ carries the real window's pixel size:
//     kitty at 100x40 answered 1000x880 through tmux exactly as it did
//     without it, so the cell size the pixels rung cuts to is unchanged. (A
//     detached session answers 1280x768 for 80x24, which is nobody's cell;
//     the parent of a hook is an attached claude, so that answer is not on
//     this path.)
//
// What the mark cannot answer: it is the tmux server's environment, not the
// attached client's, so a session started under one terminal and attached
// from another names the first. Written here rather than guessed at
// somewhere else.

package term

import (
	"os"
	"strings"
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

// behindTmux names the terminal a pane is really in, by the mark that
// terminal left in the environment tmux inherited. Empty where nothing
// marked it, and then TERM — tmux's own — is still the best answer there
// is. The answer is that terminal's own TERM, so everything downstream
// reads one kind of name.
func behindTmux() string {
	switch {
	case env("KITTY_WINDOW_ID") != "":
		return "xterm-kitty"
	case env("GHOSTTY_RESOURCES_DIR") != "":
		return "xterm-ghostty"
	}
	return ""
}
