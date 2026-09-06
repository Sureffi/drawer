// mux_test.go — laws for the terminal behind tmux. The one command this
// file's subject runs is stubbed, so what is asserted is the judgement and
// not whether there is a tmux on this machine.

package term

import (
	"strings"
	"testing"
)

// stubTmux stands a tmux up that answers one question, and remembers what it
// was asked.
func stubTmux(t *testing.T, answer string) *string {
	t.Helper()
	was := askTmux
	asked := new(string)
	askTmux = func(socket string, args ...string) string {
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
		if got := Name(); got != c.want {
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
	Name()
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
	if got := Name(); got != "xterm-kitty" {
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
