// pixelhook.go — real pixels from inside the hook.
//
// A program that owns the terminal's output can stream a picture's bytes
// through what it writes. A hook has no such stream: what it returns is text, and
// CC's display wire strips a graphics escape out of that text without a
// word — measured, the transmission simply was not there on the other
// side. The placeholder cells, on the other hand, ride through untouched:
// U+10EEEE with its diacritics and a 256-colour foreground came out byte
// for byte and counted as one column each.
//
// So the picture goes round CC rather than through it. The hook writes
// the PNG to a temp file and hands the terminal one short escape naming
// that file, straight down the parent's own tty via /proc. kitty reads the
// file, deletes it, and holds the image under the id the cells will name.
// One write of a hundred-odd bytes is atomic on a tty, so the bytes cannot
// land inside a frame CC is mid-way through writing — which is the hazard
// pushing fifty kilobytes down the same wire would have had.
//
// Where any of that cannot happen — no kitty, no cell size in pixels, no
// rasteriser, a tty that is not there — the answer is nil and the rung
// below draws. Nothing here is a dependency.

package drawer

import (
	"encoding/base64"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// drawPixels is the pixels rung: the rows that show a picture, or nil.
func drawPixels(src string, width int) []string {
	r := ProbeRaster("auto")
	if r == nil || !hookGeom.OK() {
		return nil
	}
	svg, err := renderThemedSVG(src, pxFontPt(hookGeom.CellW))
	if err != nil {
		return nil
	}
	ptW, ptH, err := svgSize(svg)
	if err != nil {
		return nil
	}
	pxW, pxH := ptW*pxPerPt, ptH*pxPerPt
	cols := int(math.Ceil(pxW / float64(hookGeom.CellW)))
	if cols > width {
		cols = width
	}
	if cols > len(RowColumnDiacritics) {
		cols = len(RowColumnDiacritics)
	}
	if cols < 4 {
		return nil
	}
	// The picture is cut to exactly the columns it will occupy, so kitty
	// scales nothing on the axis that has to line up with text.
	zoom := float64(cols*hookGeom.CellW) / pxW
	rows := int(math.Ceil(pxH * zoom / float64(hookGeom.CellH)))
	if rows < 1 || rows > drawMaxRows || rows > len(RowColumnDiacritics) {
		return nil
	}
	if zoom > rasterMaxZoom {
		zoom = rasterMaxZoom
	}
	png, err := r.run(svg, zoom)
	if err != nil {
		return nil
	}
	id := hookImageID(src, cols, rows)
	if !transmitFile(parentTTYOut(), png, id, cols, rows) {
		return nil
	}
	return placeholderRows(id, cols, rows)
}

// parentTTYOut is the terminal on the parent's stdout, where the picture
// goes. Its stdin is where the size was read; both are the same tty in an
// interactive session and neither exists in an oracle.
func parentTTYOut() string { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/1" }

// hookImageID names a picture by hashing its cut: derived, never minted. The low byte rides in the placeholder's 256-colour
// foreground and the high byte in a third diacritic, because the display
// wire quantises a truecolor foreground and would have mangled a 24-bit id
// — measured: 38;2;253;151;31 came back as 38;5;215. Zero is "no image" in
// the low byte, so it is skipped.
func hookImageID(src string, cols, rows int) uint32 {
	h := fnv1a32(strconv.Itoa(cols) + "x" + strconv.Itoa(rows) + "\x00" + src)
	lo := 1 + h%255
	hi := (h >> 8) & 0xff
	return hi<<24 | lo
}

// transmitFile hands the terminal the picture through a temp file it will
// delete itself. The name has to carry tty-graphics-protocol and the file
// has to sit in a temp dir kitty knows, or it refuses on purpose.
func transmitFile(tty string, png []byte, id uint32, cols, rows int) bool {
	sweepPictures()
	f, err := os.CreateTemp("", "tty-graphics-protocol-graph-*.png")
	if err != nil {
		return false
	}
	path := f.Name()
	if _, err := f.Write(png); err != nil {
		f.Close()
		os.Remove(path)
		return false
	}
	f.Close()
	out, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		os.Remove(path)
		return false
	}
	defer out.Close()
	cmd := "\x1b_Ga=T,U=1,q=2,f=100,t=t,i=" + strconv.Itoa(int(id)) +
		",c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + ";" +
		base64.StdEncoding.EncodeToString([]byte(path)) + "\x1b\\"
	if _, err := out.WriteString(cmd); err != nil {
		os.Remove(path)
		return false
	}
	return true
}

// sweepPictures drops pictures the terminal never collected — a tty that
// was not kitty after all, or one that had gone away. kitty deletes what it
// reads within the moment; anything older than a minute is nobody's.
func sweepPictures() {
	old, _ := filepath.Glob(filepath.Join(os.TempDir(), "tty-graphics-protocol-graph-*.png"))
	for _, p := range old {
		if info, err := os.Stat(p); err == nil && time.Since(info.ModTime()) > time.Minute {
			os.Remove(p)
		}
	}
}

// placeholderRows is the block of cells that shows the picture: every
// cell names its image by colour and its row and column by diacritic, so
// the block survives being re-wrapped, copied, or scrolled as text. The
// column mark is written on every cell rather than inferred from the one
// before it, because CC re-wraps what a hook returns and a cell that lost
// its neighbour would otherwise lose its place too.
func placeholderRows(id uint32, cols, rows int) []string {
	lo, hi := id&0xff, (id>>24)&0xff
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString("\x1b[38;5;" + strconv.Itoa(int(lo)) + "m")
		for c := 0; c < cols; c++ {
			b.WriteRune(PlaceholderRune)
			b.WriteRune(RowColumnDiacritics[r])
			b.WriteRune(RowColumnDiacritics[c])
			if hi > 0 {
				b.WriteRune(RowColumnDiacritics[hi])
			}
		}
		b.WriteString("\x1b[39m")
		out = append(out, b.String())
	}
	return out
}
