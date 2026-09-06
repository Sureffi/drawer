// glyph.go — the vocabulary, and the one selector that reads it.
//
// A cell does not know its glyph until the drawing is finished. Two
// strokes meeting are a corner, a tee or a crossing depending on nothing
// but what else arrives, so a line is never written as a glyph: it is
// recorded as an arm — one side of one cell, in the style it leaves in —
// and the glyph is looked up from the four arms at the end. Writing
// glyphs as you go and merging them cannot work, which is why every bend
// used to come out as ┼.
//
// The table below is the whole vocabulary, written so a reader can check
// it against the Unicode block without decoding a number: four
// characters, N E S W, "." for no arm, "l" light, ":" dashed, "h" heavy,
// "=" double.
//
// Unicode gives every light-and-heavy combination of four arms its own
// glyph, so that half of the vocabulary never falls back. The other two
// halves are thinner — double has no per-arm mixing, dashed has no
// junctions at all — and a cell that cannot say its arms exactly says
// them in the heaviest style present that it can draw. The arms are all
// still there, one weight up; a reader walks the same graph.

package cells

import "strings"

// A Style is how a line is drawn. Light is the zero value because it is
// what a line is when nobody said otherwise.
type Style uint8

const (
	Light  Style = iota // ─ │
	Dashed              // ┄ ┆
	Heavy               // ━ ┃
	Double              // ═ ║
)

// weight orders the styles for the fall: dashed is light with gaps, so
// it weighs the same, and where the two meet the solid line wins.
func (s Style) weight() int {
	switch s {
	case Heavy:
		return 2
	case Double:
		return 3
	}
	return 1
}

// A Dir is one side of a cell, in the order a Mask packs them.
type Dir uint8

const (
	North Dir = iota
	East
	South
	West
)

// Opposite is the side a line arriving from d leaves by.
func (d Dir) Opposite() Dir { return (d + 2) % 4 }

// Step is the cell one move in d: dx, dy, with y down the drawing.
func (d Dir) Step() (int, int) { return dsx[d], dsy[d] }

var dsx = [4]int{0, 1, 0, -1}
var dsy = [4]int{-1, 0, 1, 0}

// A Mask is one cell's four arms — North, East, South, West — four bits
// each: zero for no arm, otherwise the Style plus one. The top bit asks
// for a rounded corner, which is a property of the box the cell belongs
// to rather than of any line, and so has to ride along with the arms.
type Mask uint32

const roundBit Mask = 1 << 16

// With returns the mask with an arm added on side d. An arm already
// there is replaced: a cell has one line per side and no more.
func (m Mask) With(d Dir, s Style) Mask {
	return m&^(0xf<<(4*d)) | Mask(s+1)<<(4*d)
}

// Has reports whether a line leaves this cell on side d.
func (m Mask) Has(d Dir) bool { return m>>(4*d)&0xf != 0 }

// Arm is the style of side d's arm, and whether there is one.
func (m Mask) Arm(d Dir) (Style, bool) {
	v := m >> (4 * d) & 0xf
	return Style(v - 1), v != 0
}

// Round asks for ╭ ╮ ╰ ╯ where the arms make a light corner. Nothing
// else in the block is rounded, so a heavier corner and every junction
// ignore it — a box's wall stays a wall, whatever joins it.
func (m Mask) Round() Mask { return m | roundBit }

// Rounded reports whether this cell asked for rounded corners.
func (m Mask) Rounded() bool { return m&roundBit != 0 }

// Horiz and Vert are the axis questions a router asks: is a lane
// through this cell already spoken for.
func (m Mask) Horiz() bool { return m.Has(East) || m.Has(West) }
func (m Mask) Vert() bool  { return m.Has(North) || m.Has(South) }

// Arms drops the rounding and leaves the lines.
func (m Mask) Arms() Mask { return m &^ roundBit }

// vocabulary — every glyph this rung draws, by the arms it makes.
// N E S W; "." none, "l" light, ":" dashed, "h" heavy, "=" double.
//
// A cell with one arm reads as the whole line it continues rather than
// as a half-line: a run that starts nowhere should still look like a
// line, and ╴ ╵ ╶ ╷ read as damage.
var vocabulary = map[string]rune{
	// ---- light and heavy: the complete half ----
	// stubs
	"l...": '│', "h...": '┃', "..l.": '│', "..h.": '┃',
	".l..": '─', ".h..": '━', "...l": '─', "...h": '━',
	// lines
	".l.l": '─', ".h.h": '━', ".l.h": '╾', ".h.l": '╼',
	"l.l.": '│', "h.h.": '┃', "l.h.": '╽', "h.l.": '╿',
	// corners: down and right
	".ll.": '┌', ".hl.": '┍', ".lh.": '┎', ".hh.": '┏',
	// corners: down and left
	"..ll": '┐', "..lh": '┑', "..hl": '┒', "..hh": '┓',
	// corners: up and right
	"ll..": '└', "lh..": '┕', "hl..": '┖', "hh..": '┗',
	// corners: up and left
	"l..l": '┘', "l..h": '┙', "h..l": '┚', "h..h": '┛',
	// tee: vertical and right
	"lll.": '├', "lhl.": '┝', "hll.": '┞', "llh.": '┟',
	"hlh.": '┠', "hhl.": '┡', "lhh.": '┢', "hhh.": '┣',
	// tee: vertical and left
	"l.ll": '┤', "l.lh": '┥', "h.ll": '┦', "l.hl": '┧',
	"h.hl": '┨', "h.lh": '┩', "l.hh": '┪', "h.hh": '┫',
	// tee: down and horizontal
	".lll": '┬', ".llh": '┭', ".hll": '┮', ".hlh": '┯',
	".lhl": '┰', ".lhh": '┱', ".hhl": '┲', ".hhh": '┳',
	// tee: up and horizontal
	"ll.l": '┴', "ll.h": '┵', "lh.l": '┶', "lh.h": '┷',
	"hl.l": '┸', "hl.h": '┹', "hh.l": '┺', "hh.h": '┻',
	// crossing
	"llll": '┼', "lllh": '┽', "lhll": '┾', "lhlh": '┿',
	"hlll": '╀', "llhl": '╁', "hlhl": '╂', "hllh": '╃',
	"hhll": '╄', "llhh": '╅', "lhhl": '╆', "hhlh": '╇',
	"lhhh": '╈', "hlhh": '╉', "hhhl": '╊', "hhhh": '╋',

	// ---- double: whole axes only ----
	// stubs and lines
	"=...": '║', "..=.": '║', ".=..": '═', "...=": '═',
	".=.=": '═', "=.=.": '║',
	// corners
	".=l.": '╒', ".l=.": '╓', ".==.": '╔',
	"..l=": '╕', "..=l": '╖', "..==": '╗',
	"l=..": '╘', "=l..": '╙', "==..": '╚',
	"l..=": '╛', "=..l": '╜', "=..=": '╝',
	// tees
	"l=l.": '╞', "=l=.": '╟', "===.": '╠',
	"l.l=": '╡', "=.=l": '╢', "=.==": '╣',
	".=l=": '╤', ".l=l": '╥', ".===": '╦',
	"l=.=": '╧', "=l.l": '╨', "==.=": '╩',
	// crossing
	"l=l=": '╪', "=l=l": '╫', "====": '╬',

	// ---- dashed: lines only ----
	":...": '┆', "..:.": '┆', ".:..": '┄', "...:": '┄',
	".:.:": '┄', ":.:.": '┆',
}

// rounded is the light corner's other voice. ╭ is always a box and ┌ is
// always a bend, and the reader never pays to tell a wall from a turn.
var rounded = map[rune]rune{'┌': '╭', '┐': '╮', '└': '╰', '┘': '╯'}

// lut is the vocabulary compiled to masks, built once.
var lut = func() map[Mask]rune {
	m := make(map[Mask]rune, len(vocabulary))
	for k, r := range vocabulary {
		if mk, ok := maskOf(k); ok {
			m[mk] = r
		}
	}
	return m
}()

// maskOf reads one table key. A key this does not understand is not a
// mask, and the law in cells_test.go is what says so out loud.
func maskOf(k string) (Mask, bool) {
	if len(k) != 4 {
		return 0, false
	}
	var m Mask
	for i := 0; i < 4; i++ {
		var s Style
		switch k[i] {
		case '.':
			continue
		case 'l':
			s = Light
		case ':':
			s = Dashed
		case 'h':
			s = Heavy
		case '=':
			s = Double
		default:
			return 0, false
		}
		m = m.With(Dir(i), s)
	}
	return m, true
}

// Glyph is the selector: any mask, one rune. An exact glyph wins; where
// the block has none — a double arm meeting a light one across an axis,
// a dashed arm at a junction, heavy meeting double — every arm falls to
// the heaviest style present, which the block always draws.
func Glyph(m Mask) rune {
	arms := m.Arms()
	if arms == 0 {
		return ' '
	}
	r, ok := lut[arms]
	if !ok {
		r = lut[arms.promote(arms.heaviest())]
	}
	if m.Rounded() {
		if q, ok := rounded[r]; ok {
			return q
		}
	}
	return r
}

// heaviest is the style the cell falls to: the weightiest arm on it,
// and among equal weights the solid line rather than the dashed one.
func (m Mask) heaviest() Style {
	best := Light
	for d := North; d <= West; d++ {
		if s, ok := m.Arm(d); ok && s.weight() > best.weight() {
			best = s
		}
	}
	return best
}

// promote redraws every arm the cell has in one style.
func (m Mask) promote(s Style) Mask {
	var out Mask
	for d := North; d <= West; d++ {
		if _, ok := m.Arm(d); ok {
			out = out.With(d, s)
		}
	}
	return out
}

// Arrow is the head that points d. These four are font-drawn in every
// monospace face this rung has been put in front of, and a head is the
// one mark in the drawing that has to be unmistakable.
func Arrow(d Dir) rune { return arrows[d] }

var arrows = [4]rune{'▲', '▶', '▼', '◀'}

// Alphabet is every rune a box drawing spends on a line, a corner, a
// junction, a crossing, a border or an arrowhead — this rung's own and
// the ones graph-easy draws with, because both are read by the same
// reader. A label made of these is a label a reader takes for a drawing:
// the words have to be told apart from the picture by their runes alone,
// and there is nothing else to tell them apart by.
const Alphabet = "─━═╌╍┄┅┈┉╴╶╸╺▬▀▄−·⋯∼-~=" +
	"│┃║╎╏┆┇┊┋╵╷╹╻⋮≀▮▌▐∥|:!'\"" +
	"┌┏╔╭┎┍╒╓┐┓╗╮┒┑╕╖└┗╚╰┖┕╘╙┘┛╝╯┚┙╛╜" +
	"├┣╠┝┞┟┠┡┢┤┫╣┥┦┧┨┩┪┬┳╦┭┮┯┰┱┲┴┻╩┵┶┷┸┹┺" +
	"┼╋╬╳⧓┽┾┿╀╁╂╃╄╅╆╇╈╉╊" +
	"▶▷▸▹►▻◀◁◂◃◄◅▲△▴▵▼▽▾▿" +
	"><^v∧∨" +
	"+#█."

// InAlphabet reports whether a rune is one of those.
func InAlphabet(r rune) bool { return strings.ContainsRune(Alphabet, r) }

// heads is the four ascii arrowheads and the side each joins its line on.
// They are ordinary letters and punctuation as well, and a reader takes
// one for a head exactly when there is a line on that side — so a label
// with a `v` in it must not be set under a line, and one with a `^` must
// not be set over one.
var heads = map[rune]Dir{'>': West, '<': East, '^': South, '\u2227': South, 'v': North, '\u2228': North}
