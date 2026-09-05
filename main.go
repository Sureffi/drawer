// drawer — a ```dot fence in Claude Code becomes a drawing, in place.
//
// One binary, nothing else running. Claude Code hands a MessageDisplay hook
// each piece of an assistant message before it is laid out and takes back
// a replacement; this hook finds a DOT fence, lays it out with graphviz
// (compiled in — no `dot` on the PATH), and hands back the picture as text
// CC renders verbatim. Where the terminal is kitty it hands back a real
// image instead. The plugin in this repo is how it is installed; the two
// hook entry points are what the plugin's script execs.
//
//	drawer -hook               # what CC runs: payload on stdin, JSON out
//	drawer -context            # what a model should know, for a SessionStart hook to hand it
//	drawer -show-theme         # the theme in force, as DOT: a theme file starts here
//	drawer -dot FILE -size WxH # draw a file offline, at a size
//	drawer -dot FILE -png OUT  # the pixels rung's picture, to a file
//	drawer -deltas FILE        # replay a recorded turn; nonzero if damaged
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sureffi/drawer/internal/term"
)

func main() {
	// Three flags default from the environment, because that is how a
	// setting reaches a hook: DRAWER_RENDER, DRAWER_THEME and DRAWER_TEE,
	// set for claude in settings.json's `env` or the shell.
	hook := flag.Bool("hook", false, "act as a CC MessageDisplay hook: payload on stdin, replacement on stdout")
	contextLine := flag.Bool("context", false, "print what a model should know about this hook, as a SessionStart hook hands it, and exit")
	render := flag.String("render", envOr("DRAWER_RENDER", "auto"), "how a graph is drawn: auto, pixels, octants, braille or cells (DRAWER_RENDER)")
	hooktee := flag.String("hooktee", os.Getenv("DRAWER_TEE"), "as -hook: append every payload here, one JSON object per line, a fixture for -deltas (DRAWER_TEE)")
	dotDump := flag.String("dot", "", "draw a DOT file and print it")
	deltaDump := flag.String("deltas", "", "replay a recorded MessageDisplay delta stream through the hook; nonzero if it damaged the message")
	size := flag.String("size", "100x40", "screen size for -dot, WxH; -deltas reads the width")
	pngOut := flag.String("png", "", "with -dot: write the pixels rung's picture here, as the hook would draw it")
	cell := flag.String("cell", "10x24", "with -png: a terminal cell in pixels, WxH")
	themePath := flag.String("theme", os.Getenv("DRAWER_THEME"), "a theme file: DOT graph/node/edge defaults for the pixels rung (DRAWER_THEME; default: Claude Code's own theme)")
	showTheme := flag.Bool("show-theme", false, "print the theme in force as DOT and exit: Claude Code's, or the file given with -theme")
	flag.Parse()

	// A theme the hook cannot read is the built-in one: the picture draws,
	// and the session opens. Everywhere else — -dot, -png, -deltas — a bad
	// theme is an answer.
	var th *theme
	if *themePath != "" {
		loaded, err := loadTheme(*themePath)
		if err != nil && !*hook && !*contextLine {
			fmt.Fprintln(os.Stderr, "drawer: theme:", err)
			os.Exit(1)
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
		os.Exit(r.runPNG(*dotDump, *pngOut, w, term.Geom{CellW: cw, CellH: ch}))
	case *dotDump != "":
		os.Exit(r.runDotDump(*dotDump, w, h))
	case *deltaDump != "":
		os.Exit(r.runDeltas(*deltaDump, w))
	case *hook:
		os.Exit(r.runHook())
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// envOr is an environment variable, or the default where it is unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
