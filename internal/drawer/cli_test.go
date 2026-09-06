// cli_test.go — laws for the offline doors.

package drawer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

// The offline picture is the hook's picture: cut to whole columns of the
// cell it was asked for. Skipped where there is nothing to rasterise with.
func TestRunPNGWritesTheHooksPicture(t *testing.T) {
	if pixel.Find() == nil {
		t.Skip("no rasteriser on the PATH")
	}
	dir := t.TempDir()
	dot := dir + "/g.dot"
	png := dir + "/g.png"
	if err := os.WriteFile(dot, []byte("digraph { rankdir=LR; a -> b [label=\"x\"]; b -> c }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := (run{theme: &inForce{}}).runPNG(t.Context(), dot, png, 100, term.Geom{CellW: 10, CellH: 24}); code != 0 {
		t.Fatalf("runPNG exited %d", code)
	}
	b, err := os.ReadFile(png)
	if err != nil {
		t.Fatal(err)
	}
	w, h, err := pngSize(b)
	if err != nil {
		t.Fatal(err)
	}
	if w%10 != 0 || w > 1000 || h <= 0 {
		t.Errorf("picture is %dx%d px; want a whole number of 10px columns within 100", w, h)
	}
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// pngSize reads the dimensions out of the IHDR chunk, which is the first
// chunk of every PNG and holds them in its first eight bytes. Decoding the
// whole image to learn two numbers would cost the pixels twice.
func pngSize(png []byte) (int, int, error) {
	if len(png) < 24 || !bytes.Equal(png[:8], pngMagic) || string(png[12:16]) != "IHDR" {
		return 0, 0, errors.New("not a png")
	}
	w := int(binary.BigEndian.Uint32(png[16:20]))
	h := int(binary.BigEndian.Uint32(png[20:24]))
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("png has no size")
	}
	return w, h, nil
}

// The version line is what scripts/release.sh asks the binary it has just
// built, and what a stranger reads to say which release they are running.
// The linker writes the variable; a law that sets it reads the same line
// back out.
func TestVersionSaysTheReleaseItWasBuiltFrom(t *testing.T) {
	was := version
	version = "9.9.9-law"
	defer func() { version = was }()
	var b strings.Builder
	if code := runVersion(&b); code != 0 {
		t.Fatalf("-version exited %d", code)
	}
	if b.String() != "drawer 9.9.9-law\n" {
		t.Fatalf("-version said %q, want %q", b.String(), "drawer 9.9.9-law\n")
	}
}

// The door itself, not only the writing: it asks for the window and hands
// what came back down. The window is the one thing a law must not let it
// ask for real — go test runs under the go command, whose stdin is a
// terminal on the machine this was written on and a pipe on CI — so the
// probe is a parameter and this one answers with a window nobody has.
//
// TMUX is cleared for the same reason the window is handed in. A run built
// under a live tmux asks that tmux about its pane, so the law would read
// the developer's own multiplexer and answer differently on a machine that
// has one — and it would run somebody else's tmux to do it.
func TestTheDoctorDoorSaysTheWindowItWasHanded(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	probe := func() (int, term.Geom, term.WidthFrom) {
		return 120, term.Geom{CellW: 9, CellH: 20}, term.FromTTY
	}
	var b strings.Builder
	r := newRun(t.Context(), rungAuto, "/tmp/deltas.jsonl", &inForce{th: &theme.Theme{}})
	if code := r.runDoctor(t.Context(), &b, probe); code != 0 {
		t.Fatalf("-doctor exited %d", code)
	}
	for _, want := range []string{
		"terminal: xterm-kitty",
		"columns: 120 (from tty)",
		"cell: 9x20 px",
		"placeholders: yes",
		"tmux: no",
		"tee: /tmp/deltas.jsonl",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("-doctor did not say %q:\n%s", want, b.String())
		}
	}
}

// -doctor is what a hook process would decide from, said out loud: eleven
// facts, one per line, in the order the ladder decides them. Under a pipe
// there is no window to ask — the columns are the number nobody chose and
// the cell size is unknown — and a terminal that says nothing gets braille,
// which every font carries.
//
// The window is handed in rather than probed: `go test` runs under the go
// command, and the go command's own stdin is a terminal on the machine this
// was written on and a pipe on CI, so a law that probed would read a
// different window in each place.
func TestDoctorSaysWhatAHookWouldSee(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("TMUX", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	var b strings.Builder
	newRun(t.Context(), rungAuto, "", &inForce{}).doctor(t.Context(), &b, 100, term.FromDefault)
	said := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	want := []string{
		"drawer: ",
		"terminal: dumb",
		"tmux: no",
		"columns: 100 (from default)",
		"cell: unknown: the terminal did not say",
		"placeholders: no",
		"rasteriser: ",
		"rung: braille (asked: auto)",
		"theme: Claude Code's dark",
		"state: ",
		"tee: none",
	}
	if len(said) != len(want) {
		t.Fatalf("-doctor said %d lines, want %d:\n%s", len(said), len(want), b.String())
	}
	for i, w := range want {
		if !strings.HasPrefix(said[i], w) {
			t.Errorf("line %d is %q, want it to open %q", i+1, said[i], w)
		}
	}
	if !strings.HasSuffix(said[9], " (writable)") {
		t.Errorf("a state directory this law just made is not writable: %q", said[9])
	}
}

// The theme line says which theme actually came out, and a -theme file that
// would not read says so there rather than taking the whole door down: the
// reader who ran -doctor is the reader whose theme is wrong. TMUX is
// cleared here too: the run this builds is a run in no multiplexer, and a
// law that read the developer's would fork his tmux to find out.
func TestDoctorSaysWhichThemeCameOut(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	cases := []struct {
		name string
		th   *inForce
		want string
	}{
		{"a file that read", &inForce{th: &theme.Theme{}, file: "/themes/night.dot"},
			"theme: file /themes/night.dot"},
		{"a file that would not", &inForce{file: "/themes/gone.dot", readErr: errors.New("open /themes/gone.dot: no such file or directory")},
			"theme: file /themes/gone.dot (would not read: open /themes/gone.dot: no such file or directory)"},
	}
	for _, c := range cases {
		var b strings.Builder
		newRun(t.Context(), rungAuto, "", c.th).doctor(t.Context(), &b, 100, term.FromDefault)
		line := ""
		for _, l := range strings.Split(b.String(), "\n") {
			if strings.HasPrefix(l, "theme: ") {
				line = l
			}
		}
		if line != c.want {
			t.Errorf("%s: theme line is %q, want %q", c.name, line, c.want)
		}
	}
}

// countingTmux puts a tmux on the PATH that writes down every command it is
// given and answers nothing. The file it writes is the count.
func countingTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "asked")
	sh := "#!/bin/sh\nshift 2\necho \"$1\" >>" + log + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
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

// -version and -show-theme need neither the terminal nor the multiplexer:
// one prints a linker variable and the other prints a theme. Building the
// run asks tmux which terminal is behind this pane, and that is a fork and
// a socket round-trip in front of a door whose whole job is to print a
// string — and on a tmux that has stopped answering, two seconds of it.
// Both answer before the run exists.
//
// The boundary is the point, so a door that does need the terminal stands
// beside them: -context reads the rung it is describing, so it builds the
// run and asks.
func TestTheDoorsThatNeedNoTerminalRunNoTmux(t *testing.T) {
	for _, c := range []struct {
		door string
		runs int
	}{
		{"-version", 0},
		{"-show-theme", 0},
		{"-context", 1},
	} {
		log := countingTmux(t)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "/nowhere,1,0")
		t.Setenv("TMUX_PANE", "%0")
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DRAWER_THEME", "")
		t.Setenv("DRAWER_TEE", "")
		t.Setenv("DRAWER_RENDER", "")
		if code := Main([]string{c.door}); code != 0 {
			t.Errorf("%s exited %d", c.door, code)
		}
		if got := asked(t, log); len(got) != c.runs {
			t.Errorf("%s ran tmux %v; want %d times", c.door, got, c.runs)
		}
	}
}
