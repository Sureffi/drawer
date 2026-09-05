package drawer

// The native way in. CC ships a MessageDisplay hook that hands us the
// text it is about to draw and takes back a replacement, so a ```dot
// fence can become a drawing without anything being rewritten upstream
// of CC at all.
//
// A hook stands upstream of CC's layout and downstream of the model, which
// is the one place where the source is known and the text is still free:
// nothing can be invented downstream of the layout, so whatever replaces a
// fence has to be made here.
//
// Everything here is fail-open: whatever we cannot draw is handed back as
// the exact bytes we were given, and any error at all leaves the delta
// alone. A hook that fails must be
// invisible, because the thing it is failing at is CC's own display.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// hookIn is CC's payload. Only the fields we use are named; the rest of
// the key set (session_id, transcript_path, cwd, prompt_id, turn_id,
// hook_event_name) is measured and deliberately unread — a hook that
// binds to more of the payload than it needs is a hook that breaks on
// more of CC's changes than it has to.
type hookIn struct {
	Delta     string `json:"delta"`
	Final     bool   `json:"final"`
	MessageID string `json:"message_id"`
	Index     int    `json:"index"`
	SessionID string `json:"session_id"`
}

// The output shape is nested and the nesting is load-bearing: a flat
// {"displayContent": ...} is accepted, parsed, and silently ignored —
// no error on screen, no entry in the hook log, the original simply
// displays. The binary carries the check as
// `hookSpecificOutput is missing required field "hookEventName"`.
type hookOut struct {
	Specific hookSpecific `json:"hookSpecificOutput"`
}

type hookSpecific struct {
	HookEventName  string `json:"hookEventName"`
	DisplayContent string `json:"displayContent"`
}

// hookWidth is the one genuinely awkward part of standing here. A command
// hook has no controlling terminal: fds 0/1/2 are pipes and /dev/tty
// fails. But the parent process is `claude`, which does have one, so the
// window size is readable through /proc. Linux-only, and named as such
// rather than hidden behind a fallback that would quietly draw at 80.
// winsize is TIOCGWINSZ's answer: rows, columns, and the window's pixel
// size, which kitty fills in and most terminals leave at zero.
type winsize struct{ rows, cols, x, y uint16 }

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// parentTTY is the terminal the hook's parent is talking to, reached
// through /proc. Empty when there is none, which is every offline oracle.
func parentTTY() string { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/0" }

// hookGeom is what the window said about itself: cells, and the pixel
// size of a cell where the terminal reports one (kitty does; most leave
// it zero, which reads as no pixels).
var hookGeom PxGeom

func hookSize() (width, rows int) {
	cols, lines := 0, 0
	if f, err := os.Open(parentTTY()); err == nil {
		var ws winsize
		if ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) == nil && ws.cols > 0 {
			cols, lines = int(ws.cols), int(ws.rows)
			if ws.x > 0 && ws.y > 0 {
				hookGeom = PxGeom{CellW: int(ws.x) / cols, CellH: int(ws.y) / lines}
			}
		}
		f.Close()
	}
	if lines == 0 {
		lines = 24
	}
	// CC's own chrome at the foot of the window, measured at 100x30: the
	// hint line, two separators, the input row and two statusline rows,
	// plus the turn line and its blank while a reply is running. What is
	// left is the transcript; a mode that must not overrun it gets it
	// as rows.
	budget := lines - 8
	if budget < 4 {
		budget = 4
	}
	if cols == 0 {
		if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 0 {
			cols = v
		}
	}
	if cols == 0 {
		cols = 100
	}
	// CC's own left margin for a message body.
	const inset = 6
	if cols > inset {
		return cols - inset, budget
	}
	return cols, budget
}

// Mode is what is done with a complete fence, and how -deltas judges the
// result. Draw is the one this binary ships: it emits the drawing itself,
// correct for the width it was drawn at. A program embedding the package
// can pass its own — one that books rows for a drawing it will paint
// itself, say — and the hook wire and its oracle work the same for either.
//
// Emit is handed a complete source, the columns the text has, and the
// transcript's height in rows. Check is -deltas' third rule: given a
// block that replaced a fence and the source it was for, say what is
// wrong with it, or nothing.
type Mode struct {
	Emit  func(src string, width, rows int) []string
	Check func(block, src string, width, rows int) error
}

// Tee is where a live turn is written down, one payload per line, in
// exactly the shape -deltas reads. One armed session therefore produces a
// fixture rather than a log.
//
// It cannot be an environment variable. CC scrubs the environment before
// running a hook (measured: a variable set in the parent shell arrives
// empty), so the path has to ride the command line in settings.json.
var Tee string

func teePayload(raw []byte) {
	if Tee == "" {
		return
	}
	f, err := os.OpenFile(Tee, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(bytes.TrimRight(raw, "\n"), '\n'))
	f.Close()
}

// RunHook is the whole of the -hook entry point: one payload in, one
// replacement out, through the mode it is given. Any failure prints an
// empty object, which CC reads as "display the original".
func RunHook(m Mode) int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Print("{}")
		return 0
	}
	teePayload(raw)
	var in hookIn
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Print("{}")
		return 0
	}
	hookSession = in.SessionID
	st := loadState(in.MessageID)
	before := *&st
	width, rows := hookSize()
	// The first delta of a message is the first thing the hook hears after
	// a theme switch; pixelledger.go says why, and what is repainted.
	if in.Index == 0 && pickRung() == "pixels" {
		repaintPictures(hookSession, parentTTYOut(), ProbeRaster("auto"))
	}
	text := Stream(in.Delta, in.Final, &st, func(src string) []string {
		return m.Emit(src, width, rows)
	})
	saveState(in.MessageID, st)
	// Nothing held, nothing drawn, nothing suppressed: let CC display its
	// own delta rather than handing back a copy of it.
	if !before.InFence && !before.PendingClose && before.Buf == "" &&
		!st.InFence && !st.PendingClose && st.Buf == "" && text == in.Delta {
		fmt.Print("{}")
		return 0
	}
	json.NewEncoder(os.Stdout).Encode(hookOut{Specific: hookSpecific{
		HookEventName:  "MessageDisplay",
		DisplayContent: text,
	}})
	return 0
}

// hookTerm names the terminal. TERM survives CC's scrub of a hook's
// environment (measured); when it does not, the parent's environment is
// readable through /proc and says the same thing.
func hookTerm() string {
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(os.Getppid()) + "/environ"); err == nil {
		for _, kv := range strings.Split(string(b), "\x00") {
			if strings.HasPrefix(kv, "TERM=") {
				return kv[5:]
			}
		}
	}
	return ""
}
