// themehold.go — the theme this run draws in.
//
// Deriving Claude Code's theme reads its settings and opens graphviz, so it
// is not done until a picture actually asks: most deltas are prose and draw
// nothing, and a hook is one process per delta. -theme fills this in up
// front instead, because a file that will not parse is an answer everywhere
// but the hook.
//
// One process per delta and one goroutine in it, so the memo is a nil check
// and not a sync.Once.

package main

import "github.com/sureffi/drawer/internal/theme"

type inForce struct{ th *theme.Theme }

func (f *inForce) get() *theme.Theme {
	if f.th == nil {
		th, err := theme.Claude()
		if err != nil {
			panic("drawer: the derived theme does not parse: " + err.Error())
		}
		f.th = th
	}
	return f.th
}
