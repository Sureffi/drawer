// run.go — which drawing this run makes.
//
// The rung was a string, compared in thirteen places across three files
// with no compiler behind any of them: a typo in one was a silent fall to
// braille. It is five constants now, and the zero value is auto — which is
// what -render already defaulted to, so a run nobody filled in reads the
// terminal.

package main

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
