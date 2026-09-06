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
// it. Three attributes are read as rules rather than values, and
// internal/pixel says how: fontname names a font file the picture is set in
// and nothing else, fontsize yields to the cell when the theme has none,
// and a node's fill is a rule.
//
// The theme in force is Claude Code's own, unless DRAWER_THEME names a
// file. Claude Code's theme is a palette of named colours — claude, text,
// subtle, inactive, success, userMessageBackground and some sixty more —
// and six of them are the picture: what the transcript is drawn in, the
// drawing is drawn in, and a theme switch reaches it. The four stock
// palettes are carried here, read from the binary; a custom theme is its
// base with the overrides its file declares laid over. `drawer -show-theme`
// prints the result as DOT, which is where a theme file starts. A file the
// hook cannot read at run time is Claude Code's theme — the picture draws;
// `drawer -theme FILE -dot ...` says what is wrong with it.

package theme

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/sureffi/drawer/internal/layout"
)

// claudePalettes is Claude Code's stock palettes, the six keys the mapping
// reads, as read from the 2.1.257 binary. The two ansi themes name
// terminal palette slots a picture cannot use, and draw with their base's
// colours.
var claudePalettes = map[string]map[string]string{
	"dark":             {"claude": "#d77757", "text": "#ffffff", "subtle": "#505050", "inactive": "#999999", "success": "#4eba65", "userMessageBackground": "#373737"},
	"light":            {"claude": "#d77757", "text": "#000000", "subtle": "#afafaf", "inactive": "#666666", "success": "#2c7a39", "userMessageBackground": "#f0f0f0"},
	"dark-daltonized":  {"claude": "#ff9933", "text": "#ffffff", "subtle": "#505050", "inactive": "#999999", "success": "#3399ff", "userMessageBackground": "#373737"},
	"light-daltonized": {"claude": "#ff9933", "text": "#000000", "subtle": "#afafaf", "inactive": "#666666", "success": "#006699", "userMessageBackground": "#dcdcdc"},
}

// ClaudeDOT is Claude Code's theme as a theme: its palette on the
// picture. A node is drawn as the user's own message is — that background
// for the fill, the text colour for the label — with claude, the accent,
// for every stroke; an edge label is in success, a cluster's outline in
// inactive and its caption in subtle.
func ClaudeDOT() string {
	p := claudePalette()
	return "graph [bgcolor=transparent, pad=0.15, color=\"" + p["inactive"] + "\", fontcolor=\"" + p["subtle"] + "\", style=\"rounded,dashed\", penwidth=1]\n" +
		"node  [shape=box, style=rounded, fillcolor=\"" + p["userMessageBackground"] + "\", color=\"" + p["claude"] + "\", fontcolor=\"" + p["text"] + "\", penwidth=1.4]\n" +
		"edge  [color=\"" + p["claude"] + "\", fontcolor=\"" + p["success"] + "\", penwidth=1.2]\n"
}

// claudeSetting resolves the theme Claude Code was told to use, the way
// Claude Code resolves it: the stock palette it stands on, and the colours
// a custom theme lays over that. The `theme` of ~/.claude/settings.json, or
// of the older ~/.claude.json where the newer file has none, is a stock
// name or custom:NAME, and ~/.claude/themes/NAME.json says which stock
// palette a custom theme stands on and which colours it overrides. Anything
// the hook cannot read is dark, auto included: auto is the ground Claude
// Code found by asking the terminal, which a hook cannot ask.
func claudeSetting() (string, map[string]any) {
	name := ""
	var overrides map[string]any
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".claude.json")} {
			if name = jsonString(p, "theme"); name != "" {
				break
			}
		}
		if slug, ok := strings.CutPrefix(name, "custom:"); ok {
			var custom struct {
				Base      string         `json:"base"`
				Overrides map[string]any `json:"overrides"`
			}
			if raw, err := os.ReadFile(filepath.Join(home, ".claude", "themes", slug+".json")); err == nil {
				json.Unmarshal(raw, &custom)
			}
			name, overrides = custom.Base, custom.Overrides
		}
	}
	name = strings.TrimSuffix(name, "-ansi")
	if _, ok := claudePalettes[name]; !ok {
		name = "dark"
	}
	return name, overrides
}

// ClaudeName is the stock palette Claude Code's theme resolves to, which is
// the one the picture is painted from. `drawer -doctor` says it, because a
// picture in colours the reader did not expect is asking this question.
func ClaudeName() string { name, _ := claudeSetting(); return name }

// claudePalette is that palette with the overrides laid over it. An
// override is taken as #rrggbb, #rgb or rgb(r,g,b); one in any other form —
// a terminal palette slot, say — is the base's colour.
func claudePalette() map[string]string {
	name, overrides := claudeSetting()
	base := claudePalettes[name]
	pal := make(map[string]string, len(base))
	for k, v := range base {
		pal[k] = v
		if s, _ := overrides[k].(string); s != "" {
			if hex := hexColour(s); hex != "" {
				pal[k] = hex
			}
		}
	}
	return pal
}

var (
	rgbRe = regexp.MustCompile(`^rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)$`)
	hexRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)
)

// hexColour is a colour as #rrggbb, or "" for a form a picture cannot use.
func hexColour(s string) string {
	s = strings.TrimSpace(s)
	if m := rgbRe.FindStringSubmatch(s); m != nil {
		var c [3]int
		for i := range c {
			c[i], _ = strconv.Atoi(m[i+1])
			if c[i] > 255 {
				return ""
			}
		}
		return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
	}
	if !hexRe.MatchString(s) {
		return ""
	}
	s = strings.ToLower(s)
	if len(s) == 4 {
		return "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	return s
}

// jsonString is one string-valued top-level key of a JSON file, or "" for
// anything else: no file, not JSON, no key, not a string.
func jsonString(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var s string
	json.Unmarshal(doc[key], &s)
	return s
}

// Theme is a parsed theme: for each kind of object, what it declares, and
// the DOT it was read from.
type Theme struct {
	Graph, Node, Edge map[string]string
	Source            string
}

// Face is what the theme asked the picture to be set in: the fontname of
// whichever kind declares one, or "monospace", which names no file and so
// leaves the picture in the Go Mono the binary carries.
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
	KindGraph = 0
	KindNode  = 1
	KindEdge  = 2
)

// Parse reads a theme from DOT declarations. The declarations are parsed
// inside a graph of their own, and what that graph declares as its defaults
// is the theme.
func Parse(ctx context.Context, src string) (*Theme, error) {
	th := &Theme{Graph: map[string]string{}, Node: map[string]string{}, Edge: map[string]string{}, Source: src}
	err := layout.Door(ctx, "digraph {\n"+src+"\n}\n", func(_ context.Context, _ *graphviz.Graphviz, graph *cgraph.Graph) error {
		for kind, m := range map[int]map[string]string{KindGraph: th.Graph, KindNode: th.Node, KindEdge: th.Edge} {
			var sym *cgraph.Symbol
			for {
				var err error
				if sym, err = graph.NextAttr(kind, sym); err != nil {
					return err
				}
				if sym == nil {
					break
				}
				m[sym.Name()] = sym.DefaultValue()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return th, nil
}

// Load reads a theme file, or says what is wrong with it.
func Load(ctx context.Context, path string) (*Theme, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(ctx, string(src))
}

// Claude is Claude Code's own theme, derived from its settings and read
// back as a theme.
func Claude(ctx context.Context) (*Theme, error) { return Parse(ctx, ClaudeDOT()) }
