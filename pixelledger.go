// pixelledger.go — the pictures a session has drawn, painted again.
//
// A theme switch in Claude Code redraws the transcript in the new colours,
// and the pictures in it stay as they were: they are kitty's, held under
// the ids the placeholder cells name, and nothing asks for them again.
// Measured: the session that switched gets no hook event for its own
// settings write — every other session on the machine does — so the first
// thing this hook hears after a switch is the first delta of the next
// reply. That is where this runs. The theme in force is compared to the
// one the pictures stand in, and where it differs every picture of the
// session is laid out again in the new theme, at its old cut, and sent
// under its old id. kitty repaints the cells wherever they are, scrollback
// included (measured), and nothing is printed into the transcript.
//
// The ledger is one file per session, beside the fence state: the source
// and cut of every picture, and the theme they were last painted in. The id
// is not written down, being derived from source and cut; the cell
// geometry is, because the cells on screen are the size they were.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/goccy/go-graphviz/cgraph"
)

// picture is one drawn picture: enough to draw it again at the same cut.
// The orientation is part of the cut — empty as written, top-down where
// the hook flipped it to fit the width — because the rows on screen are
// the rows that layout gave, and a repaint has to lay it out the same way.
type picture struct {
	Src     string         `json:"src"`
	Cols    int            `json:"cols"`
	Rows    int            `json:"rows"`
	Geom    pxGeom         `json:"geom"`
	Rankdir cgraph.RankDir `json:"rankdir,omitempty"`
}

type ledger struct {
	Theme    string    `json:"theme"`
	Pictures []picture `json:"pictures"`
}

// hookSession is the session the hook is running in, from its payload.
var hookSession string

// ledgerMax bounds a session's ledger: the last pictures drawn, which are
// the ones on or near the screen.
const ledgerMax = 40

func ledgerPath(session string) string {
	return filepath.Join(stateDir(), "pictures", safeName(session)+".json")
}

// themeSig names the theme in force, for the ledger to compare.
func themeSig() string {
	return strconv.FormatUint(uint64(fnv1a32(currentTheme().Source)), 16)
}

// recordPicture writes a picture into its session's ledger, once, and
// marks the ledger with the theme it was drawn in.
func recordPicture(session string, p picture) {
	if session == "" {
		return
	}
	path := ledgerPath(session)
	os.MkdirAll(filepath.Dir(path), 0o700)
	sweepState(filepath.Dir(path), 7*24*time.Hour)
	var l ledger
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &l)
	}
	kept := l.Pictures[:0]
	for _, q := range l.Pictures {
		if q != p {
			kept = append(kept, q)
		}
	}
	l.Pictures = append(kept, p)
	if len(l.Pictures) > ledgerMax {
		l.Pictures = l.Pictures[len(l.Pictures)-ledgerMax:]
	}
	l.Theme = themeSig()
	if b, err := json.Marshal(l); err == nil {
		os.WriteFile(path, b, 0o600)
	}
}

// repaintPictures sends every picture in a session's ledger to the terminal
// again, in the theme in force, when that is not the theme they stand in.
// How many were sent; none when nothing changed, or nothing was drawn.
func repaintPictures(session, tty string, r *raster) int {
	if session == "" || r == nil {
		return 0
	}
	path := ledgerPath(session)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var l ledger
	if json.Unmarshal(b, &l) != nil || len(l.Pictures) == 0 {
		return 0
	}
	sig := themeSig()
	if l.Theme == sig {
		return 0
	}
	n := 0
	for _, p := range l.Pictures {
		svg, err := renderThemedSVG(p.Src, pxFontPt(p.Geom.CellW), p.Rankdir)
		if err != nil {
			continue
		}
		png, _, err := pixelFit(r, svg, p.Cols, p.Geom)
		if err != nil {
			continue
		}
		if transmitFile(tty, png, hookImageID(p.Src, p.Cols, p.Rows), p.Cols, p.Rows) {
			n++
		}
	}
	l.Theme = sig
	if b, err := json.Marshal(l); err == nil {
		os.WriteFile(path, b, 0o600)
	}
	return n
}
