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
	"flag"
	"fmt"
	"os"

	"github.com/sureffi/drawer/internal/term"
	"github.com/sureffi/drawer/internal/theme"
)

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
	fs.Parse(args)

	// A theme the hook cannot read is the built-in one: the picture draws,
	// and the session opens. Everywhere else — -dot, -png, -deltas — a bad
	// theme is an answer.
	var th *theme.Theme
	if *themePath != "" {
		loaded, err := theme.Load(*themePath)
		if err != nil && !*hook && !*contextLine {
			fmt.Fprintln(os.Stderr, "drawer: theme:", err)
			return 1
		}
		th = loaded // nil where it would not read: Claude Code's, as before
	}

	r := newRun(parseRung(*render), *hooktee, th)
	w, h := parseSize(*size, 100, 40)

	switch {
	case *showTheme:
		fmt.Print(r.theme.get().Source)
	case *contextLine:
		fmt.Print(r.sessionContext())
	case *dotDump != "" && *pngOut != "":
		cw, ch := parseSize(*cell, 10, 24)
		return r.runPNG(*dotDump, *pngOut, w, term.Geom{CellW: cw, CellH: ch})
	case *dotDump != "":
		return r.runDotDump(*dotDump, w, h)
	case *deltaDump != "":
		return r.runDeltas(*deltaDump, w)
	case *hook:
		return r.runHook()
	default:
		fs.Usage()
		return 2
	}
	return 0
}

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
