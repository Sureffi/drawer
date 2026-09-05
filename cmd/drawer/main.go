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
//	drawer -uninstall          # take an older settings.json entry out
//	drawer -dot FILE -size WxH # draw a file offline, at a size
//	drawer -dot FILE -png OUT  # the pixels rung's picture, to a file
//	drawer -deltas FILE        # replay a recorded turn; nonzero if damaged
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sureffi/drawer"
)

func main() {
	// Three flags default from the environment, because that is how a
	// setting reaches a hook: DRAWER_RENDER, DRAWER_THEME and DRAWER_TEE,
	// set for claude in settings.json's `env` or the shell.
	hook := flag.Bool("hook", false, "act as a CC MessageDisplay hook: payload on stdin, replacement on stdout")
	contextLine := flag.Bool("context", false, "print what a model should know about this hook, as a SessionStart hook hands it, and exit")
	render := flag.String("render", envOr("DRAWER_RENDER", "auto"), "how a graph is drawn: auto, pixels, octants, braille or cells (DRAWER_RENDER)")
	uninstall := flag.Bool("uninstall", false, "remove an older MessageDisplay entry from ~/.claude/settings.json and exit")
	hooktee := flag.String("hooktee", os.Getenv("DRAWER_TEE"), "as -hook: append every payload here, one JSON object per line, a fixture for -deltas (DRAWER_TEE)")
	dotDump := flag.String("dot", "", "draw a DOT file and print it")
	deltaDump := flag.String("deltas", "", "replay a recorded MessageDisplay delta stream through the hook; nonzero if it damaged the message")
	size := flag.String("size", "100x40", "screen size for -dot and -deltas, WxH")
	pngOut := flag.String("png", "", "with -dot: write the pixels rung's picture here, as the hook would draw it")
	cell := flag.String("cell", "10x24", "with -png: a terminal cell in pixels, WxH")
	themePath := flag.String("theme", os.Getenv("DRAWER_THEME"), "a theme file: DOT graph/node/edge defaults for the pixels rung (DRAWER_THEME; default: Claude Code's own theme)")
	showTheme := flag.Bool("show-theme", false, "print the theme in force as DOT and exit: Claude Code's, or the file given with -theme")
	flag.Parse()

	// A theme the hook cannot read is the built-in one: the picture draws,
	// and the session opens. Everywhere else — -dot, -png, -deltas — a bad
	// theme is an answer.
	if *themePath != "" {
		if err := drawer.LoadTheme(*themePath); err != nil && !*hook && !*contextLine {
			fmt.Fprintln(os.Stderr, "drawer: theme:", err)
			os.Exit(1)
		}
	}

	drawer.Rung = *render
	w, h := drawer.ParseSize(*size, 100, 40)

	switch {
	case *showTheme:
		fmt.Print(drawer.ThemeSource())
	case *contextLine:
		fmt.Print(drawer.Context())
		if path, bin := settingsHook(); path != "" {
			fmt.Printf("drawer: %s also carries this hook as a settings entry, and two hooks on one reply corrupt each other. `%s -uninstall` takes the settings one out.\n", path, bin)
		}
	case *uninstall:
		if err := uninstallHook(); err != nil {
			fmt.Fprintln(os.Stderr, "drawer:", err)
			os.Exit(1)
		}
	case *dotDump != "" && *pngOut != "":
		cw, ch := drawer.ParseSize(*cell, 10, 24)
		os.Exit(drawer.RunPNG(*dotDump, *pngOut, w, drawer.PxGeom{CellW: cw, CellH: ch}))
	case *dotDump != "":
		os.Exit(drawer.RunDotDump(*dotDump, w, h))
	case *deltaDump != "":
		os.Exit(drawer.RunDeltas(*deltaDump, w, h, drawer.Draw))
	case *hook:
		drawer.Tee = *hooktee
		os.Exit(drawer.RunHook(drawer.Draw))
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

// uninstallHook takes our entry out of the hooks of ~/.claude/settings.json
// and leaves every other key exactly as it found it. Before the plugin,
// `-install` wrote that entry; the plugin's hook and a settings entry would
// both run on every delta and share the state files, so this is the exit.
// The file is backed up beside itself first, because this is somebody's
// configuration and the write reorders keys: json in Go comes back sorted.
func uninstallHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "settings.json")
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s: not JSON I will rewrite: %v", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	list, _ := hooks["MessageDisplay"].([]any)
	kept := make([]any, 0, len(list))
	had := false
	for _, e := range list {
		if _, ours := ourHook(e); ours {
			had = true
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(hooks, "MessageDisplay")
	} else {
		hooks["MessageDisplay"] = kept
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if raw != nil {
		bak := path + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(bak, raw, 0o600); err != nil {
			return err
		}
		fmt.Println("backup:", bak)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return err
	}
	if had {
		fmt.Println("removed the MessageDisplay hook from", path)
	} else {
		fmt.Println("nothing of ours in", path)
	}
	return nil
}

// ourHook reads a MessageDisplay hook entry as ours, if it is: matched on
// the command's shape, `…/drawer -hook`, so a copy installed from another
// build or another path is still ours. The binary's path comes back with it.
func ourHook(e any) (bin string, ok bool) {
	m, _ := e.(map[string]any)
	inner, _ := m["hooks"].([]any)
	for _, h := range inner {
		hm, _ := h.(map[string]any)
		cmd, _ := hm["command"].(string)
		if b, rest, found := strings.Cut(cmd, " -hook"); found && strings.HasSuffix(b, "/drawer") && (rest == "" || rest[0] == ' ') {
			return b, true
		}
	}
	return "", false
}

// settingsHook is the drawer hook in ~/.claude/settings.json, where one is
// installed: the settings path and the binary it names. The plugin's
// SessionStart says so, because a plugin hook and a settings hook would
// both run on every delta, and they share the state files, so each would
// read the other's half-finished work as its own.
func settingsHook() (path, bin string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	path = filepath.Join(home, ".claude", "settings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return "", ""
	}
	hooks, _ := doc["hooks"].(map[string]any)
	list, _ := hooks["MessageDisplay"].([]any)
	for _, e := range list {
		if b, ok := ourHook(e); ok {
			return path, b
		}
	}
	return "", ""
}
