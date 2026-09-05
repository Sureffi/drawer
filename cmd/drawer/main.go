// drawer — a ```dot fence in Claude Code becomes a drawing, in place.
//
// One binary, one line of settings, nothing else running. Claude Code hands
// a MessageDisplay hook each piece of an assistant message before it is
// laid out and takes back a replacement; this hook finds a DOT fence,
// lays it out with graphviz (compiled in — no `dot` on the PATH), and
// hands back the picture as text CC renders verbatim. Where the terminal is
// kitty it hands back a real image instead.
//
//	drawer -install            # write the hook into ~/.claude/settings.json
//	drawer -uninstall          # take it out again
//	drawer -hook               # what CC runs: payload on stdin, JSON out
//	drawer -dot FILE -size WxH # draw a file offline, at a size
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
	hook := flag.Bool("hook", false, "act as a CC MessageDisplay hook: payload on stdin, replacement on stdout")
	render := flag.String("render", "auto", "how a graph is drawn: auto, pixels, octants, braille or cells")
	install := flag.Bool("install", false, "write the MessageDisplay hook into ~/.claude/settings.json and exit")
	uninstall := flag.Bool("uninstall", false, "remove the hook from ~/.claude/settings.json and exit")
	hooktee := flag.String("hooktee", "", "as -hook: append every payload here, one JSON object per line (a fixture for -deltas)")
	dotDump := flag.String("dot", "", "draw a DOT file and print it")
	deltaDump := flag.String("deltas", "", "replay a recorded MessageDisplay delta stream through the hook; nonzero if it damaged the message")
	size := flag.String("size", "100x40", "screen size for -dot and -deltas, WxH")
	flag.Parse()

	drawer.Rung = *render
	w, h := drawer.ParseSize(*size, 100, 40)

	switch {
	case *install || *uninstall:
		if err := installHook(*uninstall); err != nil {
			fmt.Fprintln(os.Stderr, "drawer:", err)
			os.Exit(1)
		}
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

// installHook writes one entry into the hooks of ~/.claude/settings.json —
// or takes ours out — and leaves every other key exactly as it found it. A
// previous copy of ours is replaced, so re-running after a rebuild or a
// move is the same as installing once. The file is backed up beside itself
// first, because this is somebody's configuration and the write reorders
// keys: json in Go comes back sorted.
func installHook(remove bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
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
	ours := func(e any) bool {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.HasSuffix(cmd, "/drawer -hook") || strings.HasSuffix(cmd, "/drawer -hook ") {
				return true
			}
		}
		return false
	}
	kept := make([]any, 0, len(list)+1)
	had := false
	for _, e := range list {
		if ours(e) {
			had = true
			continue
		}
		kept = append(kept, e)
	}
	command := self + " -hook"
	if !remove {
		kept = append(kept, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command}},
		})
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
	switch {
	case remove && had:
		fmt.Println("removed the MessageDisplay hook from", path)
	case remove:
		fmt.Println("nothing of ours in", path)
	default:
		fmt.Println("installed:", path)
		fmt.Println("  MessageDisplay ->", command)
		fmt.Println("claude picks it up live (measured on 2.1.257): a ```dot fence in a reply is drawn in place")
	}
	return nil
}
