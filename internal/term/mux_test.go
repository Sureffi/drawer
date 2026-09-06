// mux_test.go — laws for the terminal behind tmux. The one command this
// file's subject runs is stubbed, so what is asserted is the judgement and
// not whether there is a tmux on this machine.

package term

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stubTmux stands a tmux up that answers one question, and remembers what it
// was asked.
func stubTmux(t *testing.T, answer string) *string {
	t.Helper()
	was := askTmux
	asked := new(string)
	askTmux = func(_ context.Context, socket string, args ...string) string {
		*asked = socket + " " + strings.Join(args, " ")
		return answer
	}
	t.Cleanup(func() { askTmux = was })
	return asked
}

// Under tmux TERM is tmux's own and names no terminal. The answer is the
// TERM of the client attached to this pane, which is what tmux itself knows
// and the environment does not: a server keeps the environment it was born
// in, so KITTY_WINDOW_ID reaches every client that ever attaches. Measured
// 2026-09-06: an alacritty attached to a kitty-born server was handed that
// mark and drew 576 tofu boxes.
func TestNameIsTheClientTmuxSaysIsAttached(t *testing.T) {
	for _, c := range []struct {
		name   string
		env    map[string]string
		answer string
		want   string
	}{
		{"no tmux: TERM is the terminal's own",
			map[string]string{"TERM": "xterm-ghostty", "KITTY_WINDOW_ID": "1"}, "", "xterm-ghostty"},
		{"a kitty client on a kitty-born server",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "TMUX_PANE": "%0", "KITTY_WINDOW_ID": "1"},
			"xterm-kitty", "xterm-kitty"},
		{"an alacritty client on that same server, mark and all",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "TMUX_PANE": "%3", "KITTY_WINDOW_ID": "1"},
			"alacritty", "alacritty"},
		{"a ghostty client, mark absent and irrelevant",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "TMUX_PANE": "%0"},
			"xterm-ghostty", "xterm-ghostty"},
		{"tmux would not say: TERM stands, and it names no terminal",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "TMUX_PANE": "%0", "KITTY_WINDOW_ID": "1"},
			"", "tmux-256color"},
		{"a pane with no name to ask about",
			map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"},
			"xterm-kitty", "tmux-256color"},
	} {
		for _, k := range []string{"TERM", "TMUX", "TMUX_PANE", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR"} {
			t.Setenv(k, c.env[k])
		}
		stubTmux(t, c.answer)
		if got := Name(t.Context()); got != c.want {
			t.Errorf("%s: the terminal is %q, want %q", c.name, got, c.want)
		}
	}
}

// The question is asked of this server, about this pane, and it is a read.
func TestTheTmuxQuestionIsScopedAndReadOnly(t *testing.T) {
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/tmp/tmux-1000/rig,9,0")
	t.Setenv("TMUX_PANE", "%7")
	asked := stubTmux(t, "alacritty")
	Name(t.Context())
	want := "/tmp/tmux-1000/rig display-message -p -t %7 #{client_termname}"
	if *asked != want {
		t.Errorf("tmux was asked %q,\n                    want %q", *asked, want)
	}
}

// Outside tmux nothing is asked at all: TERM is the terminal's own and
// there is nobody in between to ask.
func TestNothingIsAskedOutsideTmux(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "")
	asked := stubTmux(t, "alacritty")
	if got := Name(t.Context()); got != "xterm-kitty" {
		t.Errorf("the terminal is %q, want xterm-kitty", got)
	}
	if *asked != "" {
		t.Errorf("tmux was asked %q with no tmux in the way", *asked)
	}
}

// The socket is addressed directly, so a tmux command from here reaches
// this server whatever environment it inherits.
func TestTmuxSocketIsTheFirstFieldOfTMUX(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	if !Tmux() {
		t.Fatal("TMUX is set and Tmux() says no")
	}
	if got := TmuxSocket(); got != "/tmp/tmux-1000/default" {
		t.Errorf("socket is %q", got)
	}
	t.Setenv("TMUX", "")
	if Tmux() {
		t.Error("TMUX is unset and Tmux() says yes")
	}
}

// A tmux that will not let go of the pipe is not a tmux that gets to hold
// the hook. The stub exits at once and leaves a child on the pipe it
// inherited — a shell wrapper's shape — and without WaitDelay Wait reads
// that pipe until the child is done, which measured on eefabb0 as 20 s for
// a 2 s deadline. The name that comes back is TERM, tmux's own, which names
// no terminal and lands on the glyph rung: the safe direction.
func TestATmuxHoldingThePipeIsNotAnAnswer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"),
		[]byte("#!/bin/sh\nsleep 30 &\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// ahead of the PATH, not instead of it: the stub has to be the tmux
	// that runs, and it still needs a shell with a sleep in it.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/nowhere,1,0")
	t.Setenv("TMUX_PANE", "%0")
	start := time.Now()
	got := Name(t.Context())
	if d := time.Since(start); d > muxTimeout {
		t.Errorf("Name() came back after %v; the deadline is %v", d, muxTimeout)
	}
	if got != "tmux-256color" {
		t.Errorf("the terminal is %q; a tmux that never answered leaves TERM standing", got)
	}
}

// wedgedTmux puts a tmux on the PATH that never finishes: it opens a fifo
// nobody will ever write to, which is a tmux server that has stopped
// answering seen from here. Nothing is forked — the blocked process is the
// direct child — so the only thing that can end it is the deadline.
func wedgedTmux(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "never")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no fifo to wedge on here: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmux"),
		[]byte("#!/bin/sh\nexec cat "+fifo+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The one question this file asks runs on the caller's clock, narrowed to
// muxTimeout — not on a fresh one. A hook has a deadline for everything it
// draws, and a name asked for on a clock of its own is time the drawing
// does not get: 150 ms of context is 150 ms of tmux, and a context already
// over asks nothing at all. TERM stands in both, which names no terminal
// and lands on the glyph rung — the safe direction.
func TestTheNameIsAskedOnWhatTheCallerHasLeft(t *testing.T) {
	wedgedTmux(t)
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/nowhere,1,0")
	t.Setenv("TMUX_PANE", "%0")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := Name(ctx)
	switch d := time.Since(start); {
	case d > muxTimeout/2:
		t.Errorf("Name under a 150ms context came back after %v; tmux's own clock is %v", d, muxTimeout)
	case d < 100*time.Millisecond:
		t.Errorf("Name came back after %v; the wedged tmux was never waited on at all", d)
	}
	if got != "tmux-256color" {
		t.Errorf("the terminal is %q; a tmux that never answered leaves TERM standing", got)
	}

	over, stop := context.WithCancel(t.Context())
	stop()
	start = time.Now()
	got = Name(over)
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("Name under a context already over took %v; there was no time to spend", d)
	}
	if got != "tmux-256color" {
		t.Errorf("the terminal is %q with no time to ask; TERM stands", got)
	}
}

// nastyTmux is a tmux that answers with an OSC title, a clear-screen, a
// kitty graphics escape and a megabyte of filler. Not a hypothetical: the
// answer to `#{client_termname}` is whatever the client's TERM holds, and
// TERM is set by whoever started the terminal.
func nastyTmux(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	sh := "#!/bin/sh\n" +
		"printf '\\033]0;pwned\\007\\033[2J\\033_Ga=T;x\\033\\\\'\n" +
		"s=0123456789abcdef\ni=0\n" +
		"while [ $i -lt 16 ]; do s=$s$s; i=$((i+1)); done\n" +
		"printf '%s\\n' \"$s\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// What tmux says is another program's stdout on its way to a reader's
// screen, so it is bounded on the way in and made printable on the way out.
// Measured: a stub answering an OSC title, a clear-screen and a kitty
// graphics escape had -doctor run all three against the terminal it was
// reporting on, and a stub answering 256 MB had drawer print 536 MB.
func TestWhatTmuxSaysIsBoundedAndPrintable(t *testing.T) {
	nastyTmux(t)
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/nowhere,1,0")
	t.Setenv("TMUX_PANE", "%0")
	got := Name(t.Context())
	if strings.ContainsAny(got, "\x1b\x07\n") {
		t.Errorf("the terminal's name carries an escape: %q", got)
	}
	if len(got) > printableMax {
		t.Errorf("the terminal's name is %d bytes; the line's worth is %d", len(got), printableMax)
	}
	if got == "" || got == "tmux-256color" {
		t.Errorf("the answer was dropped whole rather than cleaned: %q", got)
	}
}

// A TERM set to nothing names no terminal, so it falls through to the
// parent exactly as an absent one does. env's rule is the other one — a
// variable somebody set to nothing is an answer there — and it is TMUX's,
// where an empty value means there is no multiplexer and asking claude
// about it would find the one claude was started under.
//
// The two rules were one for a while, and a hook whose TERM had been
// scrubbed to empty rather than removed lost the terminal it could have
// read from /proc.
func TestABlankTERMIsNotAnAnswer(t *testing.T) {
	parent := parentEnv("TERM")
	if parent == "" {
		t.Skip("this process's parent carries no TERM to fall through to")
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	if got := Name(t.Context()); got != parent {
		t.Errorf("a blank TERM answered %q; the parent's environment says %q", got, parent)
	}
	if got := env("TERM"); got != "" {
		t.Errorf("env answered %q for a variable set to nothing", got)
	}
}
