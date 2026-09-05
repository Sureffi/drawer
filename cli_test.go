// cli_test.go — laws for the offline doors.

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
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
	if code := (run{theme: &inForce{}}).runPNG(dot, png, 100, term.Geom{CellW: 10, CellH: 24}); code != 0 {
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
