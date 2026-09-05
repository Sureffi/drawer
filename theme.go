// theme.go — the look of the pixel rung, written in DOT.
//
// A theme is the defaults a graph would declare for itself — `graph [...]`,
// `node [...]`, `edge [...]` — declared once for every graph the hook draws.
// It is DOT because the thing being themed is DOT: there is no second
// language to learn, graphviz's own parser reads it, and a value means in a
// theme exactly what it means in a graph.
//
// A theme applies where the model left an attribute unset, and nowhere
// else. `graph [...]` is the root and every cluster alike; an attribute that
// means nothing on one of them is ignored there, as graphviz already ignores
// it. Three attributes are read as rules rather than values, and pixel.go
// says how: fontname names the face the picture is set in, fontsize yields
// to the cell when the theme has none, and a node's fill is a rule.
//
// The built-in theme is a theme file like any other, the one below.
// `drawer -install -theme FILE` puts another on the hook line. A file the
// hook cannot read at run time is the built-in one — the picture draws;
// `drawer -theme FILE -dot ...` says what is wrong with it.

package drawer

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// DefaultTheme is the built-in look: tokyonight, kept from the first
// reference render, standing on the terminal's own ground.
const DefaultTheme = `graph [bgcolor=transparent, pad=0.15, color="#565f89", fontcolor="#a9b1d6", style="rounded,dashed", penwidth=1]
node  [shape=box, style=rounded, fillcolor="#24283b", color="#7aa2f7", fontcolor="#c0caf5", penwidth=1.4]
edge  [color="#7aa2f7", fontcolor="#9ece6a", penwidth=1.2]
`

// Theme is a parsed theme: for each kind of object, what it declares.
type Theme struct {
	Graph, Node, Edge map[string]string
}

// Face is the font the picture is set in: the theme's fontname, from
// whichever kind declares one, or the terminal's generic monospace.
func (t *Theme) Face() string {
	for _, m := range []map[string]string{t.Node, t.Graph, t.Edge} {
		if f := m["fontname"]; f != "" {
			return f
		}
	}
	return "monospace"
}

// cgraph's object kinds, as agattr and agnxtattr take them.
const (
	agGraph = 0
	agNode  = 1
	agEdge  = 2
)

// ParseTheme reads a theme from DOT declarations. The declarations are
// parsed inside a graph of their own, and what that graph declares as its
// defaults is the theme.
func ParseTheme(src string) (*Theme, error) {
	graphvizMu.Lock()
	defer graphvizMu.Unlock()
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	graph, err := graphviz.ParseBytes([]byte("digraph {\n" + src + "\n}\n"))
	if err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, errors.New("no declarations")
	}
	defer graph.Close()
	th := &Theme{Graph: map[string]string{}, Node: map[string]string{}, Edge: map[string]string{}}
	for kind, m := range map[int]map[string]string{agGraph: th.Graph, agNode: th.Node, agEdge: th.Edge} {
		var sym *cgraph.Symbol
		for {
			if sym, err = graph.NextAttr(kind, sym); err != nil {
				return nil, err
			}
			if sym == nil {
				break
			}
			m[sym.Name()] = sym.DefaultValue()
		}
	}
	return th, nil
}

// LoadTheme makes a theme file the theme, or says what is wrong with it and
// changes nothing.
func LoadTheme(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	th, err := ParseTheme(string(src))
	if err != nil {
		return err
	}
	setTheme(th)
	return nil
}

var (
	themeMu sync.Mutex
	theme   *Theme
)

func setTheme(t *Theme) {
	themeMu.Lock()
	defer themeMu.Unlock()
	theme = t
}

// currentTheme is the theme in force: what LoadTheme set, else the
// built-in one. Not to be called with graphvizMu held — parsing takes it.
func currentTheme() *Theme {
	themeMu.Lock()
	defer themeMu.Unlock()
	if theme == nil {
		th, err := ParseTheme(DefaultTheme)
		if err != nil {
			panic("drawer: the built-in theme does not parse: " + err.Error())
		}
		theme = th
	}
	return theme
}
