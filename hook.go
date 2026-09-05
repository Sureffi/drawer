package main

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
// the key set (transcript_path, cwd, prompt_id, turn_id, hook_event_name)
// is measured and deliberately unread — a hook that binds to more of the
// payload than it needs is a hook that breaks on more of CC's changes than
// it has to. The session id keys the ledger of pictures drawn.
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

// hookSize is the one genuinely awkward part of standing here. A command
// hook has no controlling terminal: fds 0/1/2 are pipes and /dev/tty
// fails. But the parent process is `claude`, which does have one, so the
// window size is readable through it — /proc on Linux, the device ps names
// on macOS — and falls to COLUMNS, then 100, rather than quietly drawing at
// 80. winsize is TIOCGWINSZ's answer: rows, columns, and the window's pixel
// size, which kitty fills in and most terminals leave at zero.
type winsize struct{ rows, cols, x, y uint16 }

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// hookGeom is what the window said about itself: cells, and the pixel
// size of a cell where the terminal reports one (kitty does; most leave
// it zero, which reads as no pixels).
var hookGeom pxGeom

func hookSize() (width int) {
	cols := 0
	if f, err := os.Open(parentTTY()); err == nil {
		var ws winsize
		if ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) == nil && ws.cols > 0 {
			cols = int(ws.cols)
			if ws.x > 0 && ws.y > 0 && ws.rows > 0 {
				hookGeom = pxGeom{CellW: int(ws.x) / cols, CellH: int(ws.y) / int(ws.rows)}
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
	// CC's own left margin for a message body.
	const inset = 6
	if cols > inset {
		return cols - inset
	}
	return cols
}

// tee is where a live turn is written down, one payload per line, in
// exactly the shape -deltas reads. One armed session therefore produces a
// fixture rather than a log. It is -hooktee on the command line, or
// DRAWER_TEE in the environment: a variable set for claude — the shell's,
// or settings.json's `env` — reaches a hook (measured on 2.1.261; an
// earlier measurement said the environment was scrubbed, and on 2.1.257
// it was not either way that mattered here).
var tee string

func teePayload(raw []byte) {
	if tee == "" {
		return
	}
	f, err := os.OpenFile(tee, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(bytes.TrimRight(raw, "\n"), '\n'))
	f.Close()
}

// runHook is the whole of the -hook entry point: one payload in, one
// replacement out, through drawBlock. Any failure prints an empty object,
// which CC reads as "display the original".
func runHook() int {
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
	width := hookSize()
	// The first delta of a message is the first thing the hook hears after
	// a theme switch; pixelledger.go says why, and what is repainted.
	if in.Index == 0 && pickRung() == "pixels" {
		repaintPictures(hookSession, parentTTYOut(), probeRaster())
	}
	st, done := takeTurn(in.MessageID, in.Index, turnPatience)
	defer done()
	before := st
	text := stream(in.Delta, in.Final, &st, func(src string, indent int) []string {
		return drawBlock(src, width-indent)
	})
	st.Next = max(st.Next, in.Index+1)
	saveState(in.MessageID, st, in.Final)
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

// sessionContext is what a model should know at the start of a session, for a
// SessionStart hook to hand it: that a ```dot fence in a reply is drawn in
// place, and what this terminal's rung can draw, which is what a graph
// should be written for. Without it a model that has never heard of the
// hook writes mermaid, or boxes out of hyphens, and neither is a picture.
func sessionContext() string {
	hookSize()
	var can string
	switch pickRung() {
	case "pixels":
		can = "as graphviz's own picture, so everything dot draws, draws"
	case "octants", "braille":
		can = "in strokes: clusters, node shapes, multi-line labels and dashed edges draw; record and HTML labels print their markup, and node colours are not painted"
	default:
		can = "in box-drawing characters: boxes with one-line labels and routed edges; clusters and node shapes do not draw"
	}
	return "drawer: a ```dot fence in a reply is drawn in place, " + can +
		". For any diagram, write graphviz DOT in a ```dot fence, not mermaid and not ASCII art. " +
		"Up to about a dozen nodes it draws as written; past that it goes top-down and tall.\n"
}
