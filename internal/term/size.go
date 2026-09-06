// size.go — the parent's terminal: how big it is, what it is called, and
// where its output goes.
//
// The one genuinely awkward part of standing here. A command hook has no
// controlling terminal: fds 0/1/2 are pipes and /dev/tty fails. But the
// parent process is `claude`, which does have one, so the window size is
// readable through it — /proc on Linux, the device ps names on macOS — and
// falls to COLUMNS, then 100, rather than quietly drawing at 80.
// TIOCGWINSZ's answer is rows, columns and the window's pixel size, which
// kitty fills in and most terminals leave at zero. x/sys/unix makes the
// call: the struct layout and the syscall number are then its business on
// every platform this ships to, and not a thing written here for a machine
// nobody in this fleet runs.
//
// Read-only, and it knows nothing about what it is being measured for.

package term

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// Geom is how big a cell is in pixels, measured from the terminal itself:
// TIOCGWINSZ carries the window's pixel size beside its cell size, so the
// answer is already there and does not have to be asked for with an escape
// and waited on. Zero means the terminal did not answer — an ordinary thing
// for a terminal to do — and reads here as no pixels.
type Geom struct{ CellW, CellH int }

// OK says the terminal answered with a cell size. Zero on either axis is
// no answer, and every rung above the glyphs reads that as no pixels.
func (g Geom) OK() bool { return g.CellW > 0 && g.CellH > 0 }

// WidthFrom says which of the three answers the column count is. A drawing
// too wide for the window is nearly always a window nobody found, and the
// difference between the terminal's own number and the one nobody chose is
// the first thing worth knowing about it.
type WidthFrom uint8

const (
	FromDefault WidthFrom = iota // nothing answered, so 100
	FromTTY                      // the window itself, through TIOCGWINSZ
	FromCOLUMNS                  // the variable, where there was no window
)

// String names the answer the way -doctor prints it: tty, COLUMNS, or
// default.
func (f WidthFrom) String() string {
	switch f {
	case FromTTY:
		return "tty"
	case FromCOLUMNS:
		return "COLUMNS"
	}
	return "default"
}

// Size is what the window says about itself: its columns, the pixel size of
// a cell where the terminal reports one, and which of the three answers the
// columns are.
func Size() (cols int, g Geom, from WidthFrom) {
	if f, err := os.Open(parentTTY()); err == nil {
		if ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ); err == nil && ws.Col > 0 {
			cols, from = int(ws.Col), FromTTY
			if ws.Xpixel > 0 && ws.Ypixel > 0 && ws.Row > 0 {
				g = Geom{CellW: int(ws.Xpixel) / cols, CellH: int(ws.Ypixel) / int(ws.Row)}
			}
		}
		f.Close()
	}
	if cols == 0 {
		if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 0 {
			cols, from = v, FromCOLUMNS
		}
	}
	if cols == 0 {
		cols, from = 100, FromDefault
	}
	return cols, g, from
}

// Name names the terminal. TERM survives CC's scrub of a hook's environment
// (measured); when it does not, the parent's environment is readable where
// there is a /proc and says the same thing.
//
// Under tmux TERM is tmux's own and names no terminal at all, so the answer
// is the terminal behind it, read from the mark that terminal left in the
// environment — mux.go says which marks, how they were measured, and what
// they cannot answer. The name that comes back is that terminal's own TERM,
// so everything downstream reads one kind of name and no rung has to know
// there was a multiplexer.
func Name() string {
	if Tmux() {
		if n := behindTmux(); n != "" {
			return n
		}
	}
	return env("TERM")
}
