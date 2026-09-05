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

package main

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

// ClaudeThemeDOT is Claude Code's theme as a theme: its palette on the
// picture. A node is drawn as the user's own message is — that background
// for the fill, the text colour for the label — with claude, the accent,
// for every stroke; an edge label is in success, a cluster's outline in
// inactive and its caption in subtle.
func ClaudeThemeDOT() string {
	p := claudePalette()
	return "graph [bgcolor=transparent, pad=0.15, color=\"" + p["inactive"] + "\", fontcolor=\"" + p["subtle"] + "\", style=\"rounded,dashed\", penwidth=1]\n" +
		"node  [shape=box, style=rounded, fillcolor=\"" + p["userMessageBackground"] + "\", color=\"" + p["claude"] + "\", fontcolor=\"" + p["text"] + "\", penwidth=1.4]\n" +
		"edge  [color=\"" + p["claude"] + "\", fontcolor=\"" + p["success"] + "\", penwidth=1.2]\n"
}

// claudePalette resolves the theme Claude Code was told to use, the way
// Claude Code resolves it. The `theme` of ~/.claude/settings.json, or of
// the older ~/.claude.json where the newer file has none, is a stock name
// or custom:NAME, and ~/.claude/themes/NAME.json says which stock palette
// a custom theme stands on and which colours it overrides. An override is
// taken as #rrggbb, #rgb or rgb(r,g,b); one in any other form — a terminal
// palette slot, say — is the base's colour. Anything the hook cannot read
// is dark, auto included: auto is the ground Claude Code found by asking
// the terminal, which a hook cannot ask.
func claudePalette() map[string]string {
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
	base, ok := claudePalettes[strings.TrimSuffix(name, "-ansi")]
	if !ok {
		base = claudePalettes["dark"]
	}
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

// ThemeSource is the theme in force as DOT: the file LoadTheme read, else
// Claude Code's theme as ClaudeThemeDOT writes it.
func ThemeSource() string { return currentTheme().Source }

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
	th := &Theme{Graph: map[string]string{}, Node: map[string]string{}, Edge: map[string]string{}, Source: src}
	err := door("digraph {\n"+src+"\n}\n", func(_ context.Context, _ *graphviz.Graphviz, graph *cgraph.Graph) error {
		for kind, m := range map[int]map[string]string{agGraph: th.Graph, agNode: th.Node, agEdge: th.Edge} {
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

var theme *Theme

func setTheme(t *Theme) { theme = t }

// currentTheme is the theme in force: what LoadTheme set, else Claude
// Code's.
func currentTheme() *Theme {
	if theme == nil {
		th, err := ParseTheme(ClaudeThemeDOT())
		if err != nil {
			panic("drawer: the derived theme does not parse: " + err.Error())
		}
		theme = th
	}
	return theme
}
