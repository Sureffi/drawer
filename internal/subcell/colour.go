// colour.go — the pen graphviz drew in, carried to the terminal.
//
// Everything in the json this rung reads is already coloured: graphviz
// writes `{"op":"c","color":"#rrggbb"}` before every stroke and
// `{"op":"C",…}` before every fill, so the author's `color=`,
// `fontcolor=` and `fillcolor=` arrive with the geometry and cost nothing
// to read. The rung threw them away; this file keeps them.
//
// Two rules make the keeping safe.
//
// Black is not a colour. `#000000` is what graphviz writes when nobody
// asked for anything, so it stays the default pen and keeps the dim the
// rung has always drawn strokes in. That is what makes an uncoloured
// graph byte-identical to the drawing before colour existed — the
// property is the point, not a happy accident, and subcell_test holds it.
//
// A coloured stroke is painted, not dimmed. SGR 2 over a set foreground
// is the terminal's own business and several of them answer it by
// dropping the colour; structure only has to recede where nobody said
// what colour it is.
//
// The wire, measured on the rig 2026-09-06 on Claude Code 2.1.261, three
// ways, byte captures each time. A 24-bit foreground crosses CC's display
// wire exactly outside a tmux pane; inside one, the same escape arrives
// snapped to the xterm-256 cube. tmux is not the one snapping it — a raw
// printf into that same pane arrives exact, and pipe-pane shows that what
// CC writes into the pane is already 38;5. Nor is the terminal's name what
// decides: TERM=tmux-256color with no tmux around it still arrives exact,
// so what the renderer reads is $TMUX, being in a pane. ESC[39m arrived
// in every one of those conditions.
//
// So 24-bit is emitted here, and where the wire quantises, the quantiser
// is downstream of this file: the same picture one step coarser, which is
// still that stroke in that colour. The pixels rung's image id has no such
// room and rides in a 256-colour foreground on purpose — pixel/cut.go
// carries this reading beside the older one and says why.

package subcell

import (
	"strconv"
	"strings"
)

// A pen is 0 for "whatever the terminal was already using", or
// 0x01rrggbb. The high bit is what lets black be a colour nobody asked
// for and 0x000001 still be a colour somebody did.
const penSet uint32 = 0x01000000

// parseColor reads one graphviz pen colour: `#rrggbb`, or `#rrggbbaa`
// where the graph asked for transparency. Anything else — a name the
// json never writes, an empty string — is the default pen.
func parseColor(s string) uint32 {
	if len(s) < 7 || s[0] != '#' {
		return 0
	}
	v, err := strconv.ParseUint(s[1:7], 16, 32)
	if err != nil {
		return 0
	}
	// Nearly transparent is nothing anybody meant to see; graphviz writes
	// `#ffffff00` for an invisible fill.
	if len(s) >= 9 {
		a, err := strconv.ParseUint(s[7:9], 16, 32)
		if err != nil || a < 0x20 {
			return 0
		}
	}
	if v == 0 { // graphviz's own black: the default pen, undeclared
		return 0
	}
	return penSet | uint32(v)
}

// wire writes one row's escapes: it holds what the terminal is currently
// set to, so a run of cells in one colour costs one SGR and a row with no
// colour in it costs none at all.
type wire struct {
	b   *strings.Builder
	col uint32 // the foreground the terminal is on
	dim bool   // SGR 2 in force
}

func (w *wire) colour(c uint32) {
	if c == w.col {
		return
	}
	if c == 0 {
		w.b.WriteString("\x1b[39m")
	} else {
		w.b.WriteString("\x1b[38;2;")
		w.b.WriteString(strconv.Itoa(int(c >> 16 & 0xff)))
		w.b.WriteByte(';')
		w.b.WriteString(strconv.Itoa(int(c >> 8 & 0xff)))
		w.b.WriteByte(';')
		w.b.WriteString(strconv.Itoa(int(c & 0xff)))
		w.b.WriteByte('m')
	}
	w.col = c
}

func (w *wire) faint(on bool) {
	if on == w.dim {
		return
	}
	if on {
		w.b.WriteString("\x1b[2m")
	} else {
		w.b.WriteString("\x1b[22m")
	}
	w.dim = on
}

// major is the colour a cell is drawn in: the one most of its dots were
// laid in, first-laid winning a tie. A cell is eight dots, so counting
// them is a scan and not a map — and the alternative, one SGR per
// sub-pixel, is not a thing a cell can carry.
func major(dots []uint32) uint32 {
	best, bn := uint32(0), 0
	for _, c := range dots {
		n := 0
		for _, d := range dots {
			if d == c {
				n++
			}
		}
		if n > bn { // strictly: a later colour never unseats an equal one
			best, bn = c, n
		}
	}
	return best
}
