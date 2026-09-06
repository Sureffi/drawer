// tmux_test.go — laws for the two things that decide whether a picture
// arrives: which terminals draw the cells, and the shape the escape has to
// have to get past tmux.

package pixel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The gate, in one place. Every terminal in this law was driven on the rig
// with this very mechanism: kitty and ghostty drew the picture in its rows;
// wezterm, konsole and alacritty drew 576 tofu boxes where it should have
// been. A name nobody measured is not on the list.
func TestPlaceholdersIsTheListOfTerminalsThatDrawThem(t *testing.T) {
	for name, want := range map[string]bool{
		"xterm-kitty":    true,
		"xterm-ghostty":  true,
		"xterm-256color": false,
		"wezterm":        false,
		"konsole-256":    false,
		"alacritty":      false,
		"tmux-256color":  false, // tmux's own: term.Name answers the terminal behind it
		"":               false,
	} {
		if got := Placeholders(name); got != want {
			t.Errorf("Placeholders(%q) is %v, want %v", name, got, want)
		}
	}
}

// The wrap, byte for byte. tmux ends a passthrough at the first ESC it
// finds, so every ESC in the payload is doubled: seven bytes for the
// introducer, two for the terminator, and two more for the escape's own
// pair — measured on the rig as 109 bytes going out and 120 arriving.
func TestTheWrapIsTmuxsPassthrough(t *testing.T) {
	esc := "\x1b_Ga=T,U=1,i=7;cGF0aA==\x1b\\"
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	got := mux.wrap(esc)
	if !strings.HasPrefix(got, "\x1bPtmux;") || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("not inside tmux's passthrough: %q", got)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(got, "\x1bPtmux;"), "\x1b\\")
	if strings.ReplaceAll(body, "\x1b\x1b", "\x1b") != esc {
		t.Errorf("unwrapping does not give the escape back:\n got %q\nwant %q", body, esc)
	}
	if len(got) != len(esc)+11 {
		t.Errorf("the wrap costs %d bytes, want 11", len(got)-len(esc))
	}
}

// And Send is where it goes on: the picture's escape is wrapped when there
// is a tmux in the way and is the APC it always was when there is not. The
// tty is an ordinary file here, which is the whole of what Send needs of
// one.
func TestSendWrapsTheEscapeOnlyForTmux(t *testing.T) {
	send := func(mux *Tmux) string {
		tty := filepath.Join(t.TempDir(), "tty")
		if err := os.WriteFile(tty, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if !Send(tty, []byte("not really a png"), 0x0100_0042, 12, 3, mux) {
			t.Fatal("Send would not write")
		}
		b, err := os.ReadFile(tty)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	bare := send(nil)
	if !strings.HasPrefix(bare, "\x1b_Ga=T,U=1,") || !strings.HasSuffix(bare, "\x1b\\") {
		t.Fatalf("the bare escape is not the APC it always was: %q", bare)
	}
	if strings.Contains(bare, "\x1bPtmux;") {
		t.Errorf("a picture with no multiplexer was wrapped: %q", bare)
	}
	wrapped := send(&Tmux{Socket: "/nowhere", Pane: "%0"})
	if !strings.HasPrefix(wrapped, "\x1bPtmux;\x1b\x1b_Ga=T,U=1,") || !strings.HasSuffix(wrapped, "\x1b\x1b\\\x1b\\") {
		t.Fatalf("Send did not put the escape inside the passthrough: %q", wrapped)
	}
}

// A nil multiplexer is "there is no tmux", and every method reads it that
// way rather than making the caller ask twice.
func TestNoMultiplexerIsNothingToDo(t *testing.T) {
	var none *Tmux
	if got := none.wrap("\x1bX"); got != "\x1bX" {
		t.Errorf("wrap changed an escape with no tmux: %q", got)
	}
	if note, through := none.Allow(); note != "" || !through {
		t.Errorf("Allow with no tmux said %q, through=%v; want nothing in the way", note, through)
	}
	if got, err := none.Passthrough(); got != "" || err != nil {
		t.Errorf("Passthrough answered with no tmux: %q, %v", got, err)
	}
}

// A tmux that will not answer is a wire this process knows is closed: the
// note says so and the answer is false, which is what sends the rung above
// back to the glyphs rather than leaving a reader rows of nothing.
//
// Two ways for tmux not to answer, and they used to read apart. run() gave
// back the same empty string for "exited 0 and said nothing" and for "never
// ran at all", and Allow read the empty one as "the option is not set" and
// went on — so on a box with no tmux on the PATH the wire read as open, the
// box went out, and the pixels had nowhere to come from. The PATH case is
// the one that ships: a GUI process's PATH routinely has no /opt/homebrew/bin
// in it.
func TestAllowSaysSoWhenTheWireIsClosed(t *testing.T) {
	for _, c := range []struct{ name, path string }{
		{"no server on that socket", os.Getenv("PATH")},
		{"no tmux on the PATH at all", ""},
	} {
		t.Setenv("PATH", c.path)
		note, through := (&Tmux{Socket: "/nowhere/tmux-does-not-exist", Pane: "%0"}).Allow()
		if through {
			t.Errorf("%s: was read as a wire the picture can cross", c.name)
		}
		if !strings.Contains(note, "allow-passthrough") || !strings.Contains(note, "%0") {
			t.Errorf("%s: the note does not say what happened, or to which pane: %q", c.name, note)
		}
	}
}
