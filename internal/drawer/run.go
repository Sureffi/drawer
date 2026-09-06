// run.go — what one run of the binary knows: the rung asked for, and what
// the terminal answered when asked.
//
// Built once, in Main, and filled out again in runHook when the payload
// names its session, then carried down by value. Nothing below reads a
// package global and nothing below can write one, which is why a law can
// stand a run up with a literal instead of saving a variable and putting
// it back.
//
// The rung was a string, compared in thirteen places across three files
// with no compiler behind any of them: a typo in one was a silent fall to
// braille. It is five constants now, and the zero value is auto — which is
// what -render already defaulted to, so a run nobody filled in reads the
// terminal.

package drawer

import (
	"context"

	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
)

type run struct {
	rung rung      // -render / DRAWER_RENDER; rungAuto reads the terminal
	geom term.Geom // a cell in pixels, where the terminal reported one
	term string    // TERM, for the auto rung
	sess string    // Claude Code's session id: the ledger's key
	tee  string    // -hooktee / DRAWER_TEE

	mux *pixel.Tmux // the multiplexer a picture has to get past; nil where there is none

	theme *inForce // the theme, derived when the first picture asks
}

// newRun is the run main hands down. TERM is read here and not at probe
// time, because it is available on every door and the window is not: a
// -deltas replay never asks the window how big it is, and it still gets to
// know what terminal it is replaying for. The multiplexer is read in the
// same breath and for the same reason — it is an environment fact, and
// under tmux it is also half of what the terminal's name is. The theme
// arrives whole, because main is where -theme was read and where it was
// found unreadable.
//
// This is the one place the name is asked for, and it is asked once: under
// tmux the answer costs a fork and a socket round-trip, and every rung
// below reads the name out of the run instead. The context is why the ask
// is here and not earlier — the deadline has to exist before somebody
// else's program is run against it.
func newRun(ctx context.Context, r rung, tee string, th *inForce) run {
	return run{rung: r, term: term.Name(ctx), mux: pixel.Multiplexer(), tee: tee, theme: th}
}

// probe asks the window how big it is. The columns come back, and where
// they came from; the cell's pixel size lands in the run. Where there is no
// terminal the geometry is zero and every rung below pixels still draws.
func (r *run) probe() (int, term.WidthFrom) {
	cols, g, from := term.Size()
	r.geom = g
	return cols, from
}

// rung is which drawing this run makes. The zero value is auto, which
// reads the terminal; the four others name themselves.
type rung uint8

const (
	rungAuto rung = iota
	rungCells
	rungBraille
	rungOctants
	rungPixels
)

func (r rung) String() string {
	switch r {
	case rungCells:
		return "cells"
	case rungBraille:
		return "braille"
	case rungOctants:
		return "octants"
	case rungPixels:
		return "pixels"
	}
	return "auto"
}

// parseRung reads -render / DRAWER_RENDER. Anything else is auto, which is
// what a string nobody recognised already meant.
func parseRung(s string) rung {
	switch s {
	case "cells":
		return rungCells
	case "braille":
		return rungBraille
	case "octants":
		return rungOctants
	case "pixels":
		return rungPixels
	}
	return rungAuto
}
