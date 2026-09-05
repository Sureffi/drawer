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

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-graphviz/cgraph"
)

// drawPixels is the pixels rung: the rows that show a picture, or nil.
func drawPixels(src string, width int) []string {
	r := ProbeRaster()
	if r == nil || !hookGeom.OK() {
		return nil
	}
	png, p, err := pixelCut(r, src, width, hookGeom)
	if err != nil {
		return nil
	}
	id := hookImageID(p.Src, p.Cols, p.Rows)
	if !transmitFile(parentTTYOut(), png, id, p.Cols, p.Rows) {
		return nil
	}
	recordPicture(hookSession, p)
	return placeholderRows(id, p.Cols, p.Rows)
}

// pixelCut is the picture for a block of cells: the cut — its columns up
// to the width, the rows that follow, and the orientation it was laid out
// in — and the pixels rasterised for exactly that block. The picture is
// cut to whole columns so kitty scales nothing on the axis that has to
// line up with text.
//
// Two layouts, as the glyph rungs try: as written, and top-down, because
// rows scroll where columns run out. As written wins outright when it
// fits at the cell's own type, and is the only layout made. Otherwise
// each is squeezed into the width and the one that keeps more of its
// type is the picture, as written on a tie. Measured, a 38-node graph
// written left-to-right at 169 columns keeps 0.54 of its type in 18
// rows; top-down it keeps 0.97 in 56. An error is a picture that will
// not fit either way — too narrow to be anything, or taller than
// drawMaxRows.
func pixelCut(r *Raster, src string, width int, geom PxGeom) ([]byte, picture, error) {
	if r == nil || !geom.OK() {
		return nil, picture{}, errors.New("no rasteriser or no cell size")
	}
	if width > len(RowColumnDiacritics) {
		width = len(RowColumnDiacritics)
	}
	var svg []byte
	cols, zoom := 0, 0.0
	rd := cgraph.RankDir("")
	var last error
	for _, try := range orientations(src) {
		s, err := renderThemedSVG(src, pxFontPt(geom.CellW), try)
		if err != nil {
			return nil, picture{}, err
		}
		ptW, _, err := svgSize(s)
		if err != nil {
			return nil, picture{}, err
		}
		c := min(int(math.Ceil(ptW*pxPerPt/float64(geom.CellW))), width)
		z, _, err := pixelZoom(s, c, geom)
		if err != nil {
			last = err
			continue
		}
		if svg == nil || z > zoom {
			svg, cols, zoom, rd = s, c, z, try
		}
		if try == "" && z >= 1 {
			break
		}
	}
	if svg == nil {
		return nil, picture{}, last
	}
	png, rows, err := pixelFit(r, svg, cols, geom)
	if err != nil {
		return nil, picture{}, err
	}
	return png, picture{Src: src, Cols: cols, Rows: rows, Geom: geom, Rankdir: rd}, nil
}

// pixelZoom is the cut's arithmetic: the zoom that puts a laid-out
// picture's width on exactly `cols` columns, and the rows that follow. An
// error is a block that will not do — too narrow to be anything, or
// taller than drawMaxRows.
func pixelZoom(svg []byte, cols int, geom PxGeom) (float64, int, error) {
	if cols < 4 {
		return 0, 0, errors.New("too narrow to draw")
	}
	ptW, ptH, err := svgSize(svg)
	if err != nil {
		return 0, 0, err
	}
	pxW, pxH := ptW*pxPerPt, ptH*pxPerPt
	zoom := float64(cols*geom.CellW) / pxW
	rows := int(math.Ceil(pxH * zoom / float64(geom.CellH)))
	if rows < 1 || rows > drawMaxRows || rows > len(RowColumnDiacritics) {
		return 0, 0, fmt.Errorf("%d rows; the ceiling is %d", rows, drawMaxRows)
	}
	if zoom > rasterMaxZoom {
		zoom = rasterMaxZoom
	}
	return zoom, rows, nil
}

// pixelFit rasterises a laid-out picture into a block `cols` wide, at the
// zoom pixelZoom chose: the pixels, and the rows they stand on.
func pixelFit(r *Raster, svg []byte, cols int, geom PxGeom) ([]byte, int, error) {
	zoom, rows, err := pixelZoom(svg, cols, geom)
	if err != nil {
		return nil, 0, err
	}
	png, err := r.run(svg, zoom)
	if err != nil {
		return nil, 0, err
	}
	return png, rows, nil
}

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
	// Append, which a tty does anyway and a file standing in for one in a
	// test does not.
	out, err := os.OpenFile(tty, os.O_WRONLY|os.O_APPEND, 0)
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
