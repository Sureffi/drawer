// grid.go — the terminal's cell grid: what a row of text costs in cells,
// and the ceiling a drawing draws inside.
//
// The floor every rung stands on. A box width, a label's length and the
// offset that centres one inside the other all have to agree, and they
// agree by being the same measurement; a row carrying colour has to lose
// its escapes before it can be measured at all. Nothing here knows what a
// graph is, and everything above it does.

package grid

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// Cells is the only ruler in this file. Box widths, label lengths and the
// offsets that centre one inside the other all have to agree, and they agree
// by being the same measurement rather than three that usually match.
func Cells(s string) int { return runewidth.StringWidth(s) }

// StripSGR drops colour escapes so a row can be measured in cells.
func StripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TrimBlank drops the blank rows a centred drawing carries above and
// below itself: the layout centres in the rows it was given, and in a
// fence those rows are only air.
func TrimBlank(rows []string) []string {
	lo, hi := 0, len(rows)
	for lo < hi && strings.TrimSpace(rows[lo]) == "" {
		lo++
	}
	for hi > lo && strings.TrimSpace(rows[hi-1]) == "" {
		hi--
	}
	return rows[lo:hi]
}

// MaxRows bounds a drawing's height. Rows scroll, so there is no ceiling
// from the window — but a 40-node chain flipped top-down is 250 rows of
// wall, and past this a reader is better served by the source.
const MaxRows = 120

// MaxCombBytes bounds the combining marks a cell keeps: decoration is lost
// past it, never a cell without a size.
const MaxCombBytes = 16

// Shadow is the second cell of a wide glyph. A canvas cell is one column,
// but a glyph is not: without a marker for the column the glyph already
// spent, `rows` emitted the rune AND a space and every row carrying a wide
// label came out one column wider than its own box for each one — a
// drawing that measured right and rendered crooked.
const Shadow rune = -1
