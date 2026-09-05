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
//	drawer -version            # the release this binary was built from
package main

import (
	"os"

	"github.com/sureffi/drawer/internal/drawer"
)

func main() { os.Exit(drawer.Main(os.Args[1:])) }
