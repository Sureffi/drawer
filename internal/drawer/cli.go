// cli.go — the offline doors. Every one of them works with no account, no
// pty and no live session, which is deliberate: this feature cost a lot of
// live calls to debug before they existed.

package drawer

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sureffi/drawer/internal/cells"
	"github.com/sureffi/drawer/internal/layout"
	"github.com/sureffi/drawer/internal/pixel"
	"github.com/sureffi/drawer/internal/subcell"
	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

// runDotDump draws a DOT source at a given cell size and prints it — the
// fast loop for judging how a diagram actually reads, without a live
// session in the way.
//
//	drawer -dot graph.dot -size 100x14 -render braille
func (r run) runDotDump(ctx context.Context, path string, w, h int) int {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-dot:", err)
		return 1
	}
	var rows []string
	if r.rung == rungBraille || r.rung == rungOctants {
		rows = subcell.Draw(ctx, string(b), w, r.rung == rungOctants)
		if rows == nil {
			fmt.Fprintf(os.Stderr, "-dot: will not fit in %d columns as strokes (source would be left alone)\n", w)
			return 1
		}
	}
	l, _, ok := layout.Fit(ctx, string(b), w, 0)
	if ok && rows == nil {
		rows = cells.Draw(l, w, h)
	}
	if rows == nil {
		// the layout already worked the answer out; reporting only "will
		// not fit" makes the caller hand-search for a size the tool knows
		if l == nil {
			l, _ = layout.DOT(ctx, string(b), "")
		}
		if dw, dh := layout.Footprint(l); dw > 0 && dh > 0 {
			fmt.Fprintf(os.Stderr, "-dot: needs %dx%d, given %dx%d (source would be left alone)\n",
				dw, dh, w, h)
		} else {
			fmt.Fprintf(os.Stderr, "-dot: will not lay out (source would be left alone)\n")
		}
		return 1
	}
	for _, row := range rows {
		fmt.Println(row)
	}
	return 0
}

// runPNG draws a DOT file the way the hook would for a terminal `width`
// cells wide with cells of `geom` pixels, and writes the picture to a file:
// a theme, or a graph, looked at without a session. Nonzero when there is
// no picture, with the reason on stderr.
func (r run) runPNG(ctx context.Context, dotPath, pngPath string, width int, geom term.Geom) int {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	ras := pixel.Find()
	if ras == nil {
		fmt.Fprintln(os.Stderr, "drawer: no rasteriser on the PATH (rsvg-convert or magick)")
		return 1
	}
	png, p, err := pixel.Cut(ctx, r.theme.get(ctx), ras, string(src), width, geom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "drawer:", err)
		return 1
	}
	how := ""
	if p.Rankdir != "" {
		how = ", laid out top-down to fit"
	}
	fmt.Printf("%s: %d×%d cells%s\n", pngPath, p.Cols, p.Rows, how)
	return 0
}

// runVersion is the -version door: the release this binary was built from,
// on the one line a script greps and a stranger reads. It writes to a
// writer for the same reason the doctor does — a law reads what it wrote,
// with nothing on the terminal in between.
func runVersion(w io.Writer) int {
	fmt.Fprintln(w, "drawer", version)
	return 0
}

// muxLine is the doctor's tmux fact: whether there is one, and whether
// this pane lets a graphics escape past. It is the difference between a
// picture that is missing and a picture that was dropped, which is the one
// question a reader under tmux actually has. Read only — the doctor asks
// what is, and the hook is what turns it on.
//
// Where the value in force is not already through, the pane's own value is
// read too, because that is the answer the hook would act on: a pane that
// set itself anything at all is one drawer will not write to, and a line
// saying it would is a line that is not true.
//
// Every string on this line that came from outside goes through
// term.Printable first, the reason the tee's note does: the option's value
// is whatever somebody set it to, and an error from a failed exec carries
// the PATH entry it was looking in. Measured on the rig: a directory named
// with an OSC title and a clear-screen put two ESC and a live BEL onto the
// terminal of the reader who ran -doctor to find out what was wrong.
func (r run) muxLine(ctx context.Context) string {
	if r.mux == nil {
		return "no"
	}
	if r.mux.Pane == "" {
		// Nothing is asked and nothing is written: a pane nobody named is a
		// pane this binary will not set an option on, so the answer is the
		// same one the hook would reach without running tmux at all.
		return "yes, and TMUX_PANE names no pane: nothing can be asked or set here" +
			" — no picture can cross this wire"
	}
	pane := "pane " + r.mux.Pane
	was, err := r.mux.Passthrough(ctx)
	switch {
	case err != nil:
		// Not "unknown": a tmux that cannot be asked is a tmux that cannot
		// be told either, and a picture down this wire is a picture gone.
		return "yes: " + pane + ", allow-passthrough could not be asked about: " +
			term.Printable(err.Error()) + " — no picture can cross this wire"
	case was == "on" || was == "all":
		return "yes: " + pane + ", allow-passthrough " + was
	}
	own, err := r.mux.PaneOption(ctx)
	switch {
	case err != nil:
		return "yes: " + pane + ", allow-passthrough " + pixel.Quoted(was) +
			", and this pane's own value could not be read: " + term.Printable(err.Error()) +
			" — no picture can cross this wire"
	case own != "":
		return "yes: " + pane + ", allow-passthrough " + own + ", set on the pane itself" +
			": drawer leaves that alone and draws glyphs"
	default:
		return "yes: " + pane + ", allow-passthrough " + pixel.Quoted(was) +
			": drawer sets it on for this pane, and this pane only, when it draws a picture"
	}
}

// runDoctor is the -doctor door: what this binary sees, one fact per line.
// Every answer is read out of the same run a hook is built from, so what it
// prints is what a hook process would have decided — which is the point.
// Nearly every question this tool gets asked is "why that rung", and the
// ten facts around that line are the ones the answer is made of.
//
// The window arrives as a call rather than being asked for inside: term.Size
// in the binary, and in a law a window nobody has, because go test runs
// under the go command and the go command's stdin is a terminal at home and
// a pipe on CI.
func (r run) runDoctor(ctx context.Context, w io.Writer, probe func() (int, term.Geom, term.WidthFrom)) int {
	cols, g, from := probe()
	r.geom = g
	r.doctor(ctx, w, cols, from)
	return 0
}

// doctor writes the eleven facts. The window is handed in rather than asked
// for here: asking is the door's job, and a law can then stand this binary
// in a terminal that is not there. The theme is the one exception: it is
// derived here, the way the first picture of a session derives it, because
// a line that only read the flags would name a theme this run may never
// get.
func (r run) doctor(ctx context.Context, w io.Writer, cols int, from term.WidthFrom) {
	name := r.term
	if name == "" {
		name = "unknown"
	}
	placeholders := "no"
	if pixel.Placeholders(r.term) {
		placeholders = "yes"
	}
	fmt.Fprintln(w, "drawer:", version)
	fmt.Fprintln(w, "terminal:", name)
	fmt.Fprintln(w, "tmux:", r.muxLine(ctx))
	fmt.Fprintf(w, "columns: %d (from %s)\n", cols, from)
	if r.geom.OK() {
		fmt.Fprintf(w, "cell: %dx%d px\n", r.geom.CellW, r.geom.CellH)
	} else {
		fmt.Fprintln(w, "cell: unknown: the terminal did not say")
	}
	fmt.Fprintln(w, "placeholders:", placeholders)
	if ras := pixel.Find(); ras != nil {
		fmt.Fprintf(w, "rasteriser: %s at %s\n", ras.Name, ras.Path)
	} else {
		fmt.Fprintln(w, "rasteriser: none: rsvg-convert or magick on the PATH would enable pixels")
	}
	fmt.Fprintf(w, "rung: %s (asked: %s)\n", r.pickRung(), r.rung)
	r.theme.get(ctx) // the answer is in the run afterwards, not in the value
	switch {
	case r.theme.file != "" && r.theme.readErr != nil:
		fmt.Fprintf(w, "theme: file %s (would not read: %s)\n", term.Printable(r.theme.file), term.Printable(firstLine(r.theme.readErr.Error())))
	case r.theme.file != "":
		fmt.Fprintln(w, "theme: file", term.Printable(r.theme.file))
	case r.theme.deriveErr != nil:
		fmt.Fprintf(w, "theme: graphviz's defaults (Claude Code's theme would not derive: %s)\n",
			term.Printable(firstLine(r.theme.deriveErr.Error())))
	default:
		fmt.Fprintln(w, "theme: Claude Code's", theme.ClaudeName())
	}
	dir := stateDir()
	writable := "writable"
	if f, err := os.CreateTemp(dir, "doctor-*"); err != nil {
		// Answered by writing one: the directory existing and the mode bits
		// reading right are both things that have been true of a directory
		// the hook could not use.
		writable = "NOT writable"
	} else {
		f.Close()
		os.Remove(f.Name())
	}
	fmt.Fprintf(w, "state: %s (%s)\n", dir, writable)
	tee := r.tee
	if tee == "" {
		tee = "none"
	}
	fmt.Fprintln(w, "tee:", tee)
}
