// mux_test.go — laws for the terminal behind tmux.

package term

import "testing"

// Under tmux TERM is tmux's own and names no terminal, so the answer is
// the mark the terminal left in the environment the server inherited.
// Measured 2026-09-06: kitty writes KITTY_WINDOW_ID, ghostty writes
// GHOSTTY_RESOURCES_DIR and not KITTY_WINDOW_ID. Outside tmux the marks are
// not read at all — a ghostty started from inside a kitty inherits
// KITTY_WINDOW_ID, and TERM is the one that knows better.
func TestNameIsTheTerminalBehindTmux(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no tmux", map[string]string{"TERM": "xterm-ghostty", "KITTY_WINDOW_ID": "1"}, "xterm-ghostty"},
		{"kitty under tmux", map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "KITTY_WINDOW_ID": "1"}, "xterm-kitty"},
		{"ghostty under tmux", map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"}, "xterm-ghostty"},
		{"an unmarked terminal under tmux", map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/s,1,0"}, "tmux-256color"},
	} {
		for _, k := range []string{"TERM", "TMUX", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR"} {
			t.Setenv(k, c.env[k])
		}
		if got := Name(); got != c.want {
			t.Errorf("%s: the terminal is %q, want %q", c.name, got, c.want)
		}
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
