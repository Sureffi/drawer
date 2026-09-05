// cli_test.go — laws for the offline doors.

package drawer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
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
func TestTheDoctorDoorSaysTheWindowItWasHanded(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	probe := func() (int, term.Geom, term.WidthFrom) {
		return 120, term.Geom{CellW: 9, CellH: 20}, term.FromTTY
	}
	var b strings.Builder
	r := newRun(rungAuto, "/tmp/deltas.jsonl", &inForce{th: &theme.Theme{}})
	if code := r.runDoctor(t.Context(), &b, probe); code != 0 {
		t.Fatalf("-doctor exited %d", code)
	}
	for _, want := range []string{
		"terminal: xterm-kitty",
		"columns: 120 (from tty)",
		"cell: 9x20 px",
		"kitty: yes",
		"tee: /tmp/deltas.jsonl",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("-doctor did not say %q:\n%s", want, b.String())
		}
	}
}

// -doctor is what a hook process would decide from, said out loud: ten
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
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	var b strings.Builder
	newRun(rungAuto, "", &inForce{}).doctor(t.Context(), &b, 100, term.FromDefault)
	said := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	want := []string{
		"drawer: ",
		"terminal: dumb",
		"columns: 100 (from default)",
		"cell: unknown: the terminal did not say",
		"kitty: no",
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
	if !strings.HasSuffix(said[8], " (writable)") {
		t.Errorf("a state directory this law just made is not writable: %q", said[8])
	}
}

// The theme line says which theme actually came out, and a -theme file that
// would not read says so there rather than taking the whole door down: the
// reader who ran -doctor is the reader whose theme is wrong.
func TestDoctorSaysWhichThemeCameOut(t *testing.T) {
	t.Setenv("TERM", "dumb")
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
		newRun(rungAuto, "", c.th).doctor(t.Context(), &b, 100, term.FromDefault)
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
