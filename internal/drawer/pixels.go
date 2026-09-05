// pixels.go — real pixels from inside the hook.
//
// A program that owns the terminal's output can stream a picture's bytes
// through what it writes. A hook has no such stream: what it returns is text, and
// CC's display wire strips a graphics escape out of that text without a
// word — measured, the transmission simply was not there on the other
// side. The placeholder cells, on the other hand, ride through untouched:
// U+10EEEE with its diacritics and a 256-colour foreground came out byte
// for byte and counted as one column each.

package drawer

import (
	"context"

	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/term"
)

// drawPixels is the pixels rung: the rows that show a picture, or nil.
func (r run) drawPixels(ctx context.Context, src string, width int) []string {
	ras := pixel.Probe()
	if ras == nil || !r.geom.OK() {
		return nil
	}
	png, p, err := pixel.Cut(ctx, r.theme.get(ctx), ras, src, width, r.geom)
	if err != nil {
		return nil
	}
	id := pixel.ImageID(p.Src, p.Cols, p.Rows)
	if !pixel.Send(term.TTYOut(), png, id, p.Cols, p.Rows) {
		return nil
	}
	r.recordPicture(ctx, p)
	return pixel.PlaceholderRows(id, p.Cols, p.Rows)
}
