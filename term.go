// term.go — the parent's terminal: how big it is, what it is called, and
// where its output goes.
//
// The one genuinely awkward part of standing here. A command hook has no
// controlling terminal: fds 0/1/2 are pipes and /dev/tty fails. But the
// parent process is `claude`, which does have one, so the window size is
// readable through it — /proc on Linux, the device ps names on macOS — and
// falls to COLUMNS, then 100, rather than quietly drawing at 80. winsize
// is TIOCGWINSZ's answer: rows, columns, and the window's pixel size,
// which kitty fills in and most terminals leave at zero.
//
// Read-only, and it knows nothing about what it is being measured for.

package main

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

type winsize struct{ rows, cols, x, y uint16 }

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// pxGeom is how big a cell is in pixels, measured from the terminal itself:
// TIOCGWINSZ carries the window's pixel size beside its cell size, so the
// answer is already there and does not have to be asked for with an escape
// and waited on. Zero means the terminal did not answer — an ordinary thing
// for a terminal to do — and reads here as no pixels.
type pxGeom struct{ CellW, CellH int }

func (g pxGeom) ok() bool { return g.CellW > 0 && g.CellH > 0 }

// termSize is what the window says about itself: its columns, and the
// pixel size of a cell where the terminal reports one.
func termSize() (cols int, g pxGeom) {
	if f, err := os.Open(parentTTY()); err == nil {
		var ws winsize
		if ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) == nil && ws.cols > 0 {
			cols = int(ws.cols)
			if ws.x > 0 && ws.y > 0 && ws.rows > 0 {
				g = pxGeom{CellW: int(ws.x) / cols, CellH: int(ws.y) / int(ws.rows)}
			}
		}
		f.Close()
	}
	if cols == 0 {
		if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 0 {
			cols = v
		}
	}
	if cols == 0 {
		cols = 100
	}
	return cols, g
}

// termName names the terminal. TERM survives CC's scrub of a hook's
// environment (measured); when it does not, the parent's environment is
// readable where there is a /proc and says the same thing.
func termName() string {
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return parentEnv("TERM")
}
