// cut.go — the picture goes round Claude Code, not through it.
//
// The hook writes the PNG to a temp file and hands the terminal one short
// escape naming that file, straight down the parent's own tty via /proc —
// wrapped in tmux's passthrough where a tmux is in the way, see tmux.go.
// The terminal reads the file, deletes it, and holds the image under the id
// the cells will name. One write of a hundred-odd bytes is atomic on a tty, so
// the bytes cannot land inside a frame CC is mid-way through writing —
// which is the hazard pushing fifty kilobytes down the same wire would
// have had.
//
// Where any of that cannot happen — a terminal that does not draw the
// placeholder cells, no cell size in pixels, a tty that is not there — the
// answer is nil and the rung below draws. Nothing here is a dependency.

package pixel

import (
	"context"
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
	"github.com/sureffi/drawer/internal/grid"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

// Picture is one drawn picture: enough to draw it again at the same cut.
// The orientation is part of the cut — empty as written, top-down where
// the hook flipped it to fit the width — because the rows on screen are
// the rows that layout gave, and a repaint has to lay it out the same way.
type Picture struct {
	Src     string         `json:"src"`
	Cols    int            `json:"cols"`
	Rows    int            `json:"rows"`
	Geom    term.Geom      `json:"geom"`
	Rankdir cgraph.RankDir `json:"rankdir,omitempty"`
}

// Cut is the picture for a block of cells: the cut — its columns up
// to the width, the rows that follow, and the orientation it was laid out
// in — and the pixels painted for exactly that block. The picture is
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
// grid.MaxRows.
func Cut(ctx context.Context, th *theme.Theme, src string, width int, geom term.Geom) ([]byte, Picture, error) {
	d, p, zoom, err := cut(ctx, th, src, width, geom)
	if err != nil {
		return nil, Picture{}, err
	}
	png, err := paintPNG(ctx, d, th.Face(), zoom)
	if err != nil {
		return nil, Picture{}, err
	}
	return png, p, nil
}

// cut is the choosing half of Cut: the layout it settled on, the block of
// cells it will stand in, and the zoom that puts it there. Nothing is
// painted, so what the cut chose can be read — by the caller above, and by
// a law — without decoding a picture to find out.
func cut(ctx context.Context, th *theme.Theme, src string, width int, geom term.Geom) (*layout.Drawing, Picture, float64, error) {
	if !geom.OK() {
		return nil, Picture{}, 0, errors.New("no cell size")
	}
	if width > len(rowColumnDiacritics) {
		width = len(rowColumnDiacritics)
	}
	var best *layout.Drawing
	cols, rows, zoom := 0, 0, 0.0
	rd := cgraph.RankDir("")
	var last error
	for _, try := range layout.Orientations(src) {
		d, err := RenderThemed(ctx, th, src, FontPt(geom.CellW), try)
		if err != nil {
			return nil, Picture{}, 0, err
		}
		c := min(int(math.Ceil(canvas(d).w()*pxPerPt/float64(geom.CellW))), width)
		z, r, err := pixelZoom(d, c, geom)
		if err != nil {
			last = err
			continue
		}
		if best == nil || z > zoom {
			best, cols, rows, zoom, rd = d, c, r, z, try
		}
		if try == "" && z >= 1 {
			break
		}
	}
	if best == nil {
		return nil, Picture{}, 0, last
	}
	return best, Picture{Src: src, Cols: cols, Rows: rows, Geom: geom, Rankdir: rd}, zoom, nil
}

// pixelZoom is the cut's arithmetic: the zoom that puts a laid-out
// picture's width on exactly `cols` columns, and the rows that follow. An
// error is a block that will not do — too narrow to be anything, or
// taller than grid.MaxRows.
func pixelZoom(d *layout.Drawing, cols int, geom term.Geom) (float64, int, error) {
	if cols < 4 {
		return 0, 0, errors.New("too narrow to draw")
	}
	b := canvas(d)
	pxW, pxH := b.w()*pxPerPt, b.h()*pxPerPt
	if pxW <= 0 || pxH <= 0 {
		return 0, 0, errors.New("the picture has no size")
	}
	zoom := float64(cols*geom.CellW) / pxW
	rows := int(math.Ceil(pxH * zoom / float64(geom.CellH)))
	if rows < 1 || rows > grid.MaxRows || rows > len(rowColumnDiacritics) {
		return 0, 0, fmt.Errorf("%d rows; the ceiling is %d", rows, grid.MaxRows)
	}
	if zoom > maxZoom {
		zoom = maxZoom
	}
	return zoom, rows, nil
}

// Fit paints a laid-out picture into a block `cols` wide, at the zoom
// pixelZoom chose: the pixels, and the rows they stand on. face is the
// theme's; paint.go says what it may name.
func Fit(ctx context.Context, d *layout.Drawing, face string, cols int, geom term.Geom) ([]byte, int, error) {
	zoom, rows, err := pixelZoom(d, cols, geom)
	if err != nil {
		return nil, 0, err
	}
	png, err := paintPNG(ctx, d, face, zoom)
	if err != nil {
		return nil, 0, err
	}
	return png, rows, nil
}

// ImageID names a picture by hashing its cut: derived, never minted. The
// low byte rides in the placeholder's 256-colour foreground and the high
// byte in a third diacritic, and it stays in that form.
//
// Two measurements of the same wire, both true on their day. On Claude
// Code 2.1.257 a truecolor foreground was quantised outright:
// 38;2;253;151;31 came back as 38;5;215, which would have mangled a 24-bit
// id. Measured again on the rig 2026-09-06, on 2.1.261 and three ways: a
// 24-bit foreground arrives exact outside a tmux pane and snapped to the
// xterm-256 cube inside one; tmux is not what snaps it, since a raw printf
// into that pane arrives exact and pipe-pane shows CC already writing 38;5
// into the pane; and the trigger is $TMUX rather than the terminal's name,
// since TERM=tmux-256color with no tmux around it arrives exact.
// The pen a drawing is coloured in stands on that second reading, because
// a line one step off is still that line. An id has no such room: one
// wrong step is
// a picture that never comes back. So the id keeps the 256-colour form,
// which is the one that crossed this wire on every version measured and in
// a pane besides. Zero is "no image" in the low byte, so it is skipped.
func ImageID(src string, cols, rows int) uint32 {
	h := fnv1a32(strconv.Itoa(cols) + "x" + strconv.Itoa(rows) + "\x00" + src)
	lo := 1 + h%255
	hi := (h >> 8) & 0xff
	return hi<<24 | lo
}

// ThemeSig names a theme, for the ledger to compare.
func ThemeSig(th *theme.Theme) string {
	return strconv.FormatUint(uint64(fnv1a32(th.Source)), 16)
}

// Send hands the terminal the picture through a temp file it will
// delete itself. The name has to carry tty-graphics-protocol and the file
// has to sit in a temp dir kitty knows, or it refuses on purpose.
//
// mux is the multiplexer between here and the screen, nil where there is
// none: under tmux the escape is wrapped in tmux's passthrough on the way
// out, which is the only shape that reaches the terminal at all. The cells
// are not wrapped and never come near this function — they are text, and
// they go home through CC's display wire.
//
// A picture whose deadline has already passed is not sent. The cells that
// would name it are not going out either, so the escape would leave an
// image in the terminal that nothing on screen ever points at.
func Send(ctx context.Context, tty string, png []byte, id uint32, cols, rows int, mux *Tmux) bool {
	if ctx.Err() != nil {
		return false
	}
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
	if _, err := out.WriteString(mux.wrap(cmd)); err != nil {
		os.Remove(path)
		return false
	}
	return true
}

// sweepPictures drops pictures the terminal never collected — a tty whose
// terminal draws no placeholder cells after all, or one that had gone away.
// A terminal that takes the file deletes it within the moment; anything
// older than a minute is nobody's.
func sweepPictures() {
	old, _ := filepath.Glob(filepath.Join(os.TempDir(), "tty-graphics-protocol-graph-*.png"))
	for _, p := range old {
		if info, err := os.Stat(p); err == nil && time.Since(info.ModTime()) > time.Minute {
			os.Remove(p)
		}
	}
}

// PlaceholderRows is the block of cells that shows the picture: every
// cell names its image by colour and its row and column by diacritic, so
// the block survives being re-wrapped, copied, or scrolled as text. The
// column mark is written on every cell rather than inferred from the one
// before it, because CC re-wraps what a hook returns and a cell that lost
// its neighbour would otherwise lose its place too.
func PlaceholderRows(id uint32, cols, rows int) []string {
	lo, hi := id&0xff, (id>>24)&0xff
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString("\x1b[38;5;" + strconv.Itoa(int(lo)) + "m")
		for c := 0; c < cols; c++ {
			b.WriteRune(PlaceholderRune)
			b.WriteRune(rowColumnDiacritics[r])
			b.WriteRune(rowColumnDiacritics[c])
			if hi > 0 {
				b.WriteRune(rowColumnDiacritics[hi])
			}
		}
		b.WriteString("\x1b[39m")
		out = append(out, b.String())
	}
	return out
}
