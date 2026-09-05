// ledger_test.go — laws for the ledger: which pictures a theme switch
// sends to the terminal again, and as what.

package drawer

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/svgtest"
	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

// mustTheme is the theme a law draws in: the declarations it names, read
// back.
func mustTheme(t *testing.T, src string) *theme.Theme {
	t.Helper()
	th, err := theme.Parse(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// The pictures a session drew are sent to the terminal again, under their
// own ids, when the theme in force is not the one they stand in — each
// once, however often it was drawn — and not otherwise.
func TestPixelLedgerRepaintsUnderTheOldIDs(t *testing.T) {
	r := pixel.Find()
	if r == nil {
		t.Skip("no rasteriser on the PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir()) // the pictures kitty is not here to collect
	// the terminal is a file here: transmitFile opens it, it does not make it
	tty := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(tty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	geom := term.Geom{CellW: 10, CellH: 24}
	a := pixel.Picture{Src: "digraph { a -> b }", Cols: 20, Rows: 3, Geom: geom}
	b := pixel.Picture{Src: "digraph { c -> d }", Cols: 20, Rows: 3, Geom: geom}
	s1 := run{sess: "s1", theme: &inForce{}}
	s1.recordPicture(t.Context(), a)
	s1.recordPicture(t.Context(), b)
	s1.recordPicture(t.Context(), a)
	if n := s1.repaintPictures(t.Context(), tty, r); n != 0 {
		t.Errorf("repainted %d pictures under the theme they were drawn in", n)
	}
	s1.theme = &inForce{th: mustTheme(t, `node [color=red]`)}
	if n := s1.repaintPictures(t.Context(), tty, r); n != 2 {
		t.Fatalf("repainted %d pictures, want 2", n)
	}
	out, err := os.ReadFile(tty)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []pixel.Picture{a, b} {
		id := ",i=" + strconv.Itoa(int(pixel.ImageID(p.Src, p.Cols, p.Rows))) + ","
		if n := strings.Count(string(out), id); n != 1 {
			t.Errorf("%q was sent %d times under its id, want once", p.Src, n)
		}
	}
	if n := s1.repaintPictures(t.Context(), tty, r); n != 0 {
		t.Errorf("repainted %d pictures with nothing changed", n)
	}
	if n := (run{sess: "s2", theme: s1.theme}).repaintPictures(t.Context(), tty, r); n != 0 {
		t.Errorf("repainted %d pictures for a session that drew none", n)
	}
}

// A picture drawn top-down is repainted top-down. The ledger carries the
// orientation with the cut, so what kitty gets under the old id is the
// picture that was there, in the new colours, and not the same source laid
// out the other way and squeezed onto the old columns.
func TestPixelLedgerRepaintsAsLaidOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DRAWER_STATE", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	tty := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(tty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var got []byte
	r := &pixel.Raster{Name: "stub", Run: func(_ context.Context, svg []byte, _ float64) ([]byte, error) {
		got = svg
		return []byte("png"), nil
	}}
	geom := term.Geom{CellW: 10, CellH: 24}
	s1 := run{sess: "s1", theme: &inForce{}}
	s1.recordPicture(t.Context(), pixel.Picture{Src: "digraph { rankdir=LR; a -> b }", Cols: 12, Rows: 7, Geom: geom, Rankdir: cgraph.TBRank})
	s1.theme = &inForce{th: mustTheme(t, `node [color=red]`)}
	if n := s1.repaintPictures(t.Context(), tty, r); n != 1 {
		t.Fatalf("repainted %d pictures, want 1", n)
	}
	ya, yb := svgtest.TextY(svgtest.Group(got, "a")), svgtest.TextY(svgtest.Group(got, "b"))
	if ya == 0 || yb == 0 {
		t.Fatalf("missing a node: a=%v b=%v", ya, yb)
	}
	if ya == yb {
		t.Errorf("repainted left-to-right, a and b both at y=%v; the picture was drawn top-down", ya)
	}
}
