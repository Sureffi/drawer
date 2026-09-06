// tmux.go — the shape a graphics escape has to have to get past tmux.
//
// tmux reads everything an application writes and forwards only what it
// understands; a kitty graphics APC is not on that list and is dropped
// without a word. The one way through is tmux's own passthrough — the
// escape wrapped in a DCS tmux; … ST, with every ESC inside it doubled,
// which tmux unwraps and writes to the terminal verbatim. Measured on the
// rig 2026-09-06: bare, the 109-byte APC reaches the pane and the picture
// never appears; wrapped, the same escape is 120 bytes and the graph draws
// in its placeholder rows, streaming and settled alike, on ghostty and
// kitty both.
//
// Only the escape is wrapped. The placeholder cells are ordinary text and
// go through CC's display wire as they always did; a cursor move or a colour
// inside the wrap would be tmux's screen being written behind tmux's back.
//
// The fact that there is a tmux at all is term's — this file is only the
// bytes, and the one command that lets them past.

package pixel

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/sureffi/drawer/internal/term"
)

// Tmux is the multiplexer between a picture and the screen: which server,
// and which pane the parent is in. A nil *Tmux is "there is no
// multiplexer", and every method here reads that as nothing to do, so a
// caller has one path whether or not tmux is in the way.
type Tmux struct{ Socket, Pane string }

// Multiplexer is the tmux this process is under, or nil.
func Multiplexer() *Tmux {
	if !term.Tmux() {
		return nil
	}
	return &Tmux{Socket: term.TmuxSocket(), Pane: term.TmuxPane()}
}

// wrap puts an escape inside tmux's passthrough. Doubling ESC is not
// decoration: tmux ends the passthrough at the first ESC it finds, so a
// single one would truncate the picture to its own first byte.
func (t *Tmux) wrap(esc string) string {
	if t == nil {
		return esc
	}
	return "\x1bPtmux;" + strings.ReplaceAll(esc, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// tmuxTimeout bounds a call to somebody else's tmux. It answers in
// milliseconds or it is not answering; a hook that waited on it would be
// holding up CC's own display.
const tmuxTimeout = 2 * time.Second

// Passthrough is whether this pane lets a passthrough through: "on", "off",
// or empty where tmux would not answer. -A asks for the value in force
// rather than the one set on the pane, because an option nobody set on the
// pane reads as empty while the server's own answer is the one that counts.
func (t *Tmux) Passthrough() string {
	if t == nil {
		return ""
	}
	return strings.TrimSpace(t.run("show", "-p", "-t", t.Pane, "-A", "-v", "allow-passthrough"))
}

// Allow turns the pane's passthrough on where it is not on already,
// because tmux drops a passthrough nobody allowed and the picture is then
// simply gone — no error, no cells missing, nothing to see. Two answers:
// what it found and did, for the tee, and whether an escape can get
// through afterwards. False is the fail-open signal the rung above needs:
// a wire this one knows is closed is a wire to draw glyphs down instead of
// leaving a reader eight blank rows.
//
// Pane-scoped, on purpose and without an option to do otherwise: this is
// somebody else's tmux and drawer is a guest in it. The change reaches the
// pane claude is running in and no other, it is never written to a file,
// the server's own setting is left where it was, and it dies with the pane.
// An option somebody already set on — on the server, or on this pane — is
// read as on and nothing is written at all: -A asks for the value in force.
//
// The note is empty where it was already on and there was nothing to do,
// because a note about nothing is noise.
func (t *Tmux) Allow() (string, bool) {
	if t == nil {
		return "", true
	}
	pane := " for pane " + t.Pane
	switch was := t.Passthrough(); was {
	case "on":
		return "", true
	case "":
		// tmux would not say. Ask for it anyway rather than deciding from
		// an answer nobody gave.
		if out := t.allow(); out != "" {
			return "tmux: allow-passthrough could not be read or set" + pane + ": " + out, false
		}
		return "tmux: allow-passthrough could not be read" + pane + "; set on for it anyway", true
	default:
		if out := t.allow(); out != "" {
			return "tmux: allow-passthrough is " + was + pane + " and would not be set: " + out, false
		}
		return "tmux: allow-passthrough was " + was + "; set on" + pane +
			" (this pane only, until it closes)", true
	}
}

// allow is the one write this package makes to anything but a tty: what
// tmux said about it, and nothing where it worked.
func (t *Tmux) allow() string {
	return strings.TrimSpace(t.run("set", "-p", "-t", t.Pane, "allow-passthrough", "on"))
}

// run is one tmux command against this server. The socket is addressed
// directly rather than through TMUX, so the answer is about this server
// whatever environment the command inherits. Output and error read the
// same: what tmux said, or nothing at all.
func (t *Tmux) run(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", append([]string{"-S", t.Socket}, args...)...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return string(out)
}
