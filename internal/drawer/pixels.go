// pixels.go — real pixels from inside the hook.
//
// A program that owns the terminal's output can stream a picture's bytes
// through what it writes. A hook has no such stream: what it returns is text, and
// CC's display wire strips a graphics escape out of that text without a
// word — measured, the transmission simply was not there on the other
// side. The placeholder cells, on the other hand, ride through untouched:
// U+10EEEE with its diacritics and a 256-colour foreground came out byte
// for byte and counted as one column each.
//
// So the two halves of a picture travel by two different roads: the cells
// home through CC, the pixels down the parent's own tty — and, under tmux,
// through tmux's passthrough, which is the only door that wire has.

package drawer

import (
	"context"

	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
)

// drawPixels is the pixels rung: the rows that show a picture, or nil.
//
// The terminal has to draw kitty's placeholder cells, whoever asked for
// this rung. Every other rung hands back text that any terminal renders as
// text; this one writes an escape down the parent's own tty and answers
// with U+10EEEE, which is a picture in kitty and ghostty and a tofu box
// everywhere else — measured, wezterm, konsole and alacritty each printed
// 576 of them. So the gate is here as well as in pickRung: a rung asked for
// by name is still that rung, and this is the rung failing open the way
// every other stage of it does.
func (r run) drawPixels(ctx context.Context, src string, width int) []string {
	if !pixel.Placeholders(r.term) {
		return nil
	}
	png, p, err := pixel.Cut(ctx, r.theme.get(ctx), src, width, r.geom)
	if err != nil {
		return nil
	}
	id := pixel.ImageID(p.Src, p.Cols, p.Rows)
	// tmux drops a passthrough nobody allowed, and a dropped picture looks
	// exactly like no picture: the cells arrive and stay empty. Ask for it
	// before the escape goes out, say so where a note can be read, and fail
	// open to the glyphs where the answer is no — eight blank rows are worse
	// than a drawing in strokes.
	note, through := r.mux.Allow(ctx)
	r.teeNote(note)
	if !through {
		return nil
	}
	if !pixel.Send(ctx, term.TTYOut(), png, id, p.Cols, p.Rows, r.mux) {
		return nil
	}
	r.recordPicture(ctx, p)
	return pixel.PlaceholderRows(id, p.Cols, p.Rows)
}
