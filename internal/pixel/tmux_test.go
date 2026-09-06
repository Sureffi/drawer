// tmux_test.go — laws for the two things that decide whether a picture
// arrives: which terminals draw the cells, and the shape the escape has to
// have to get past tmux.

package pixel

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
		if !Send(t.Context(), tty, []byte("not really a png"), 0x0100_0042, 12, 3, mux) {
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
	if note, through := none.Allow(t.Context()); note != "" || !through {
		t.Errorf("Allow with no tmux said %q, through=%v; want nothing in the way", note, through)
	}
	if got, err := none.Passthrough(t.Context()); got != "" || err != nil {
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
		note, through := (&Tmux{Socket: "/nowhere/tmux-does-not-exist", Pane: "%0"}).Allow(t.Context())
		if through {
			t.Errorf("%s: was read as a wire the picture can cross", c.name)
		}
		if !strings.Contains(note, "allow-passthrough") || !strings.Contains(note, "%0") {
			t.Errorf("%s: the note does not say what happened, or to which pane: %q", c.name, note)
		}
	}
}

// hangingTmux puts a tmux on the PATH that exits at once and leaves a child
// holding the pipe it inherited. That is the shape of a tmux wrapper — a
// shell script that starts something and returns — and it is the shape that
// used to hold this process open long past its own deadline.
func hangingTmux(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"),
		[]byte("#!/bin/sh\nsleep 30 &\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// ahead of the PATH, not instead of it: the stub has to be the tmux
	// that runs, and it still needs a shell with a sleep in it.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A deadline that only kills the process is not a deadline. Wait goes on
// reading the pipes until the last writer closes them, so a tmux that
// forked is a tmux this process waits on for as long as the child lives —
// measured on eefabb0 as 20 s for a 2 s timeout, with the hook path stalled
// a minute behind it. WaitDelay is what makes the number on the const the
// number a hook actually pays, and the answer is an error rather than a
// hang, which is what sends the rung above to the glyphs.
func TestATmuxHoldingThePipeStillAnswersInTime(t *testing.T) {
	hangingTmux(t)
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	start := time.Now()
	out, err := mux.run(t.Context(), "show", "-p", "-t", "%0", "-A", "-v", "allow-passthrough")
	if d := time.Since(start); d > tmuxTimeout {
		t.Errorf("run() came back after %v; the deadline is %v", d, tmuxTimeout)
	}
	if err == nil {
		t.Errorf("a tmux whose pipe never closed answered %q with no error", out)
	}
	// and the whole door reads it as the closed wire it is
	start = time.Now()
	note, through := mux.Allow(t.Context())
	if d := time.Since(start); d > tmuxTimeout {
		t.Errorf("Allow came back after %v; the deadline is %v", d, tmuxTimeout)
	}
	if through || !strings.Contains(note, "allow-passthrough") {
		t.Errorf("Allow said %q, through=%v; want a wire nobody can post through", note, through)
	}
}

// optionTmux puts a tmux on the PATH that answers the two questions apart:
// `show -A` with the value in force, and `show` without it with what the
// pane itself was set to. It writes down which one it was asked, so a law
// can count the execs as well as read the answers.
func optionTmux(t *testing.T, inForce, own string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "asked")
	sh := "#!/bin/sh\nshift 2\nc=$1\ncase \" $* \" in *' -A '*) c=show-A;; esac\n" +
		"echo \"$c\" >>" + log + "\n" +
		"case $c in show-A) echo '" + inForce + "';; show) echo '" + own + "';; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// countingTmux is that tmux with the server off and the pane itself unset:
// the ordinary pane, the one drawer turns on.
func countingTmux(t *testing.T) string {
	t.Helper()
	return optionTmux(t, "off", "")
}

// asked is what that tmux was asked to do, in order.
func asked(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

// One hook process asks the pane about passthrough once. It draws a picture
// and it may repaint every picture the session has, and each of those went
// through here — a fork, a socket, and the same answer, in front of CC's
// own painting. The answer is remembered instead, and setting it on is
// written into the same memory, so the second picture finds it on and the
// note about somebody else's tmux is written once.
func TestThePassthroughIsAskedAboutOncePerProcess(t *testing.T) {
	log := countingTmux(t)
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	note, through := mux.Allow(t.Context())
	if !through || !strings.Contains(note, "set on") {
		t.Fatalf("the first picture said %q, through=%v; want the pane turned on", note, through)
	}
	note, through = mux.Allow(t.Context())
	if !through {
		t.Errorf("a pane this process turned on read as closed")
	}
	if note != "" {
		t.Errorf("the note was written twice: %q", note)
	}
	if got := asked(t, log); len(got) != 3 || got[0] != "show-A" || got[1] != "show" || got[2] != "set" {
		t.Errorf("tmux was run %v; want the two reads and the one set that followed them", got)
	}
}

// The doctor asks the same question, and it is the same answer: a run that
// has already drawn a picture does not fork a tmux to say so.
func TestTheDoctorReadsTheSameMemory(t *testing.T) {
	log := countingTmux(t)
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	for i := 0; i < 3; i++ {
		if _, err := mux.Passthrough(t.Context()); err != nil {
			t.Fatalf("Passthrough: %v", err)
		}
	}
	if got := asked(t, log); len(got) != 1 || got[0] != "show-A" {
		t.Errorf("tmux was run %v; want one show", got)
	}
}

// TMUX set and TMUX_PANE unset is a tmux this process cannot name a pane
// in, and the option it would otherwise write is pane-scoped. Measured:
// `tmux set -p -t "" allow-passthrough on` does not fail — it lands on
// whatever pane tmux picks, and it picked one in another session, which is
// drawer writing a setting into a window nobody here is looking at.
//
// So it is a closed wire: nothing is asked, nothing is set, the note says
// which pane it was about, and the rung above falls to the glyphs.
func TestAnUnnamedPaneIsNotAPaneToWriteTo(t *testing.T) {
	log := countingTmux(t)
	mux := &Tmux{Socket: "/nowhere", Pane: ""}
	note, through := mux.Allow(t.Context())
	if through {
		t.Errorf("a tmux with no pane to name was read as a wire a picture can cross")
	}
	if !strings.Contains(note, "TMUX_PANE") {
		t.Errorf("the note does not say what was missing: %q", note)
	}
	if got := asked(t, log); got != nil {
		t.Errorf("tmux was run %v; want nothing run at all", got)
	}
	// the escape is still wrapped: the passthrough is a shape, not a pane
	if got := mux.wrap("\x1bX"); got != "\x1bPtmux;\x1b\x1bX\x1b\\" {
		t.Errorf("wrap needs a pane it does not have: %q", got)
	}
}

// "all" is more than "on", not less: tmux forwards any DCS on it, so a pane
// already sitting there is a pane a picture already crosses. Reading it as
// "not on" wrote an option nobody needed written, into somebody else's
// multiplexer, and narrowed what the reader had chosen.
func TestAllIsAlreadyThrough(t *testing.T) {
	log := optionTmux(t, "all", "all")
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	note, through := mux.Allow(t.Context())
	if !through || note != "" {
		t.Errorf("a pane on `all` said %q, through=%v; want nothing to do", note, through)
	}
	if got := asked(t, log); len(got) != 1 || got[0] != "show-A" {
		t.Errorf("tmux was run %v; want the one question in force", got)
	}
}

// A pane that says off in its own right is a reader who said no here, and
// the server's value in force cannot tell them apart: -A answers "off" both
// for a pane nobody set and for a pane somebody set off. So the pane is
// asked without -A before anything is written, and an explicit off is left
// exactly where it is — closed wire, glyphs, and the note says which of the
// two it was.
func TestAPaneThatSaidOffIsLeftAlone(t *testing.T) {
	log := optionTmux(t, "off", "off")
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	note, through := mux.Allow(t.Context())
	if through {
		t.Errorf("a pane a reader set off was read as a wire a picture can cross")
	}
	if !strings.Contains(note, "set there and not inherited") {
		t.Errorf("the note does not say the pane itself said no: %q", note)
	}
	if got := asked(t, log); len(got) != 2 || got[0] != "show-A" || got[1] != "show" {
		t.Errorf("tmux was run %v; want the value in force and then the pane's own", got)
	}
}

// A value this binary has no word for is still a value the pane carries.
// The policy is written as "only a pane with no value of its own is written
// to", and it used to be read as "only a pane that did not say off": a pane
// answering anything else at all was written over. tmux's option vocabulary
// is tmux's to extend, and drawer is a guest here.
func TestAPaneCarryingAValueNobodyKnowsIsLeftAlone(t *testing.T) {
	log := optionTmux(t, "off", "sometimes")
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	note, through := mux.Allow(t.Context())
	if through {
		t.Errorf("a pane answering \"sometimes\" was read as a wire a picture can cross")
	}
	if !strings.Contains(note, "sometimes") {
		t.Errorf("the note does not say what the pane's own value was: %q", note)
	}
	if got := asked(t, log); len(got) != 2 || got[0] != "show-A" || got[1] != "show" {
		t.Errorf("tmux was run %v; want the two reads and no write at all", got)
	}
}

// A pane nobody has said anything about is the pane drawer writes to, and
// the write is the only one it makes: the value in force is off because the
// server's default is off, and the pane's own answer is empty.
func TestAPaneNobodySetIsTheOneWritten(t *testing.T) {
	log := optionTmux(t, "off", "")
	mux := &Tmux{Socket: "/nowhere", Pane: "%0"}
	note, through := mux.Allow(t.Context())
	if !through || !strings.Contains(note, "set on") {
		t.Errorf("an unset pane said %q, through=%v; want it turned on", note, through)
	}
	if got := asked(t, log); len(got) != 3 || got[0] != "show-A" || got[1] != "show" || got[2] != "set" {
		t.Errorf("tmux was run %v; want both reads and the one write", got)
	}
}

// nastyTmux answers `show` with an OSC title, a clear-screen, a kitty
// graphics escape and a megabyte of filler, and complains on stderr with
// the same. An option's value is whatever somebody set it to, and this one
// travels to the tee's note and to -doctor's line.
func nastyTmux(t *testing.T, code int) {
	t.Helper()
	dir := t.TempDir()
	sh := "#!/bin/sh\n" +
		"s=0123456789abcdef\ni=0\n" +
		"while [ $i -lt 16 ]; do s=$s$s; i=$((i+1)); done\n" +
		"printf '\\033]0;pwned\\007\\033[2J%s\\n' \"$s\"\n" +
		"printf '\\033]0;stderr too\\007%s\\n' \"$s\" >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// What tmux says goes into the tee's note and onto -doctor's line, so it is
// bounded on the way in and made printable on the way out — whether it came
// back as the option's value or as the reason the option could not be read.
// Measured: a stub answering an OSC title, a clear-screen and a kitty
// graphics escape had -doctor execute all three, and a 256 MB answer was
// held and printed as 536 MB.
func TestTheNoteCarriesNothingTheTerminalWouldObey(t *testing.T) {
	for _, c := range []struct {
		name string
		code int
	}{
		{"tmux answered", 0},
		{"tmux failed and said why on stderr", 1},
	} {
		nastyTmux(t, c.code)
		note, _ := (&Tmux{Socket: "/nowhere", Pane: "%0"}).Allow(t.Context())
		if strings.ContainsAny(note, "\x1b\x07\n") {
			t.Errorf("%s: the note carries an escape: %q", c.name, note)
		}
		if len(note) > 1024 {
			t.Errorf("%s: the note is %d bytes long", c.name, len(note))
		}
	}
}

// The note is written once per process, and so is every exec behind it —
// on the paths that fail as much as on the one that works. A hook draws a
// picture and may repaint a whole ledger of them behind it, and each of
// those calls Allow: measured, one process put four identical lines about
// the same tmux into the tee, and tried the same doomed `set` four times.
//
// The tee is a fixture a replay reads, not a log, so a line repeated is a
// line that says something happened again.
func TestAClosedWireIsWrittenDownOnce(t *testing.T) {
	for _, c := range []struct {
		name string
		log  func(*testing.T) string
		runs []string
	}{
		{"a tmux that cannot be asked at all", func(t *testing.T) string {
			t.Setenv("PATH", "")
			return ""
		}, nil},
		{"a set that would not take", func(t *testing.T) string {
			return failingSetTmux(t)
		}, []string{"show-A", "show", "set"}},
	} {
		log := c.log(t)
		mux := &Tmux{Socket: "/nowhere/tmux-does-not-exist", Pane: "%0"}
		first, through := mux.Allow(t.Context())
		if through || first == "" {
			t.Errorf("%s: said %q, through=%v; want a closed wire, written down", c.name, first, through)
		}
		for i := 0; i < 3; i++ {
			if note, through := mux.Allow(t.Context()); note != "" || through {
				t.Errorf("%s: call %d said %q again, through=%v", c.name, i+2, note, through)
			}
		}
		if log != "" {
			if got := asked(t, log); len(got) != len(c.runs) {
				t.Errorf("%s: tmux was run %v; want %v", c.name, got, c.runs)
			}
		}
	}
}

// failingSetTmux answers both reads and refuses the write, which is what a
// tmux with a read-only option or a pane that went away does.
func failingSetTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "asked")
	sh := "#!/bin/sh\nshift 2\nc=$1\ncase \" $* \" in *' -A '*) c=show-A;; esac\n" +
		"echo \"$c\" >>" + log + "\n" +
		"case $c in set) echo \"can't set option: allow-passthrough\" >&2; exit 1;; " +
		"show-A) echo off;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}
