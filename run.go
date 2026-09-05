// run.go — what one run of the binary knows: the rung asked for, and what
// the terminal answered when asked.
//
// Built once, in main, and filled out again in runHook when the payload
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

package main

type run struct {
	rung rung   // -render / DRAWER_RENDER; rungAuto reads the terminal
	geom pxGeom // a cell in pixels, where the terminal reported one
	term string // TERM, for the auto rung
	sess string // Claude Code's session id: the ledger's key
	tee  string // -hooktee / DRAWER_TEE

	theme *inForce // the theme, derived when the first picture asks
}

// newRun is the run main hands down. TERM is read here and not at probe
// time, because it is available on every door and the window is not: a
// -deltas replay never asks the window how big it is, and it still gets to
// know what terminal it is replaying for.
func newRun(r rung, tee string, th *theme) run {
	return run{rung: r, term: termName(), tee: tee, theme: &inForce{th: th}}
}

// probe asks the window how big it is. The columns come back; the cell's
// pixel size lands in the run. Where there is no terminal the geometry is
// zero and every rung below pixels still draws.
func (r *run) probe() int { cols, g := termSize(); r.geom = g; return cols }

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
