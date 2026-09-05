// drawer.go — the flags, and the one door each of them opens.
//
// Main is the whole of this package's surface: cmd/drawer hands it the
// arguments and makes its answer the exit status, so every door below can
// be reached from a law as readily as from a shell.
//
// Main itself has no law of its own: the flag set is named from os.Args[0]
// and exits on a bad flag, so -h prints what it always printed and a law
// that called it would take the test binary down with it. Every door below
// it stands on its own, and that is where the laws are.

package drawer

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

// layoutTimeout is how long one process may spend at graphviz's door. A
// hook is one process per delta and a delta is on screen in milliseconds,
// so ten seconds is not a budget anybody draws inside — it is the bound on
// a wasm that has stopped answering. A layout already under way runs to its
// end whatever this says: the context is spent at the door and nowhere
// deeper, so what the deadline buys is that the next door does not open —
// Fit's second orientation, Cut's, a repaint of a fence already drawn — and
// that is the difference between late and never. A door that refuses
// returns an error like any other, and the fence shows its source under a
// notice, which is the same fail-open path a typo takes.
const layoutTimeout = 10 * time.Second

// Main is the binary. The flag set is named for the binary and exits on a
// bad flag, exactly as flag.CommandLine does, so -h prints what it always
// printed.
func Main(args []string) int {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	// Three flags default from the environment, because that is how a
	// setting reaches a hook: DRAWER_RENDER, DRAWER_THEME and DRAWER_TEE,
	// set for claude in settings.json's `env` or the shell.
	hook := fs.Bool("hook", false, "act as a CC MessageDisplay hook: payload on stdin, replacement on stdout")
	contextLine := fs.Bool("context", false, "print what a model should know about this hook, as a SessionStart hook hands it, and exit")
	render := fs.String("render", envOr("DRAWER_RENDER", "auto"), "how a graph is drawn: auto, pixels, octants, braille or cells (DRAWER_RENDER)")
	hooktee := fs.String("hooktee", os.Getenv("DRAWER_TEE"), "as -hook: append every payload here, one JSON object per line, a fixture for -deltas (DRAWER_TEE)")
	dotDump := fs.String("dot", "", "draw a DOT file and print it")
	deltaDump := fs.String("deltas", "", "replay a recorded MessageDisplay delta stream through the hook; nonzero if it damaged the message")
	size := fs.String("size", "100x40", "screen size for -dot, WxH; -deltas reads the width")
	pngOut := fs.String("png", "", "with -dot: write the pixels rung's picture here, as the hook would draw it")
	cell := fs.String("cell", "10x24", "with -png: a terminal cell in pixels, WxH")
	themePath := fs.String("theme", os.Getenv("DRAWER_THEME"), "a theme file: DOT graph/node/edge defaults for the pixels rung (DRAWER_THEME; default: Claude Code's own theme)")
	showTheme := fs.Bool("show-theme", false, "print the theme in force as DOT and exit: Claude Code's, or the file given with -theme")
	showVersion := fs.Bool("version", false, "print the version this binary was built from and exit")
	doctor := fs.Bool("doctor", false, "print what this binary sees — terminal, window, rasteriser, rung, theme, state — and exit")
	fs.Parse(args)

	// One deadline, derived here and threaded down: everything below draws
	// through the one door, and the process has this long to be at it.
	ctx, cancel := context.WithTimeout(context.Background(), layoutTimeout)
	defer cancel()

	// A theme the hook cannot read is the built-in one: the picture draws,
	// and the session opens. -doctor stands with the hook there, because it
	// is asked precisely when something is wrong and its own theme line is
	// where the reason belongs — exiting first would leave the reader with
	// none of the other nine facts. Everywhere else — -dot, -png, -deltas —
	// a bad theme is an answer.
	var th *theme.Theme
	file, readErr := "", error(nil)
	if *themePath != "" {
		file = *themePath
		loaded, err := theme.Load(ctx, *themePath)
		if err != nil {
			if !*hook && !*contextLine && !*doctor {
				fmt.Fprintln(os.Stderr, "drawer: theme:", err)
				return 1
			}
			readErr = err
		}
		th = loaded // nil where it would not read: Claude Code's, as before
	}

	r := newRun(parseRung(*render), *hooktee, &inForce{th: th, file: file, readErr: readErr})
	w, h := parseSize(*size, 100, 40)

	switch {
	case *showVersion:
		return runVersion(os.Stdout)
	case *doctor:
		return r.runDoctor(ctx, os.Stdout, term.Size)
	case *showTheme:
		fmt.Print(r.theme.get(ctx).Source)
	case *contextLine:
		fmt.Print(r.sessionContext())
	case *dotDump != "" && *pngOut != "":
		cw, ch := parseSize(*cell, 10, 24)
		return r.runPNG(ctx, *dotDump, *pngOut, w, term.Geom{CellW: cw, CellH: ch})
	case *dotDump != "":
		return r.runDotDump(ctx, *dotDump, w, h)
	case *deltaDump != "":
		return r.runDeltas(ctx, *deltaDump, w)
	case *hook:
		return r.runHook(ctx)
	default:
		fs.Usage()
		return 2
	}
	return 0
}

// version is the release this binary was built from. scripts/release.sh
// writes it in at link time with -X, so a binary a stranger downloaded can
// say which one it is; a build out of a checkout says dev, which is what it
// is.
var version = "dev"

// envOr is an environment variable, or the default where it is unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseSize reads WxH, or hands back the default.
func parseSize(s string, dw, dh int) (int, int) {
	var w, h int
	if n, _ := fmt.Sscanf(s, "%dx%d", &w, &h); n == 2 && w > 0 && h > 0 {
		return w, h
	}
	return dw, dh
}
