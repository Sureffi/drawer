// theme_test.go — laws for the theme: Claude Code's, resolved as Claude Code
// resolves it, and a theme file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTheme makes a theme the theme for one test.
func withTheme(t *testing.T, src string) {
	old := currentTheme()
	th, err := parseTheme(src)
	if err != nil {
		t.Fatal(err)
	}
	setTheme(th)
	t.Cleanup(func() { setTheme(old) })
}

// A theme is DOT declarations, and Claude Code's theme is one: with no
// Claude Code settings to read it is the stock dark palette on the picture,
// and what it declares for each kind comes back as the values it wrote.
func TestThemeIsDOTDeclarations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	src := claudeThemeDOT()
	th, err := parseTheme(src)
	if err != nil {
		t.Fatal(err)
	}
	if th.Graph["bgcolor"] != "transparent" || th.Node["fillcolor"] != "#373737" || th.Node["color"] != "#d77757" || th.Edge["fontcolor"] != "#4eba65" {
		t.Errorf("Claude Code's dark theme read back wrong: %+v", th)
	}
	if th.Source != src {
		t.Error("a theme does not keep the DOT it was read from")
	}
	if th.face() != "monospace" {
		t.Errorf("face is %q with no fontname declared", th.face())
	}
	if _, err := parseTheme("node ["); err == nil {
		t.Error("an unclosed declaration parsed")
	}
}

// The theme in force is the one Claude Code was told to use, resolved as
// Claude Code resolves it: the `theme` setting, in settings.json first and
// the older ~/.claude.json where that has none; a stock name is its
// palette, an ansi variant its base's, a custom name the base its file
// declares with the overrides laid over; auto and anything unreadable are
// dark. An override is a colour in a form a picture can use, or nothing.
func TestThemeFollowsClaudeCodesTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(func() { setTheme(nil) })
	if err := os.MkdirAll(filepath.Join(home, ".claude", "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	put := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, rel)
		if body == "" {
			os.Remove(path)
			return
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// what the picture is drawn in: node fill, stroke, text; edge label.
	drawn := func() string {
		t.Helper()
		setTheme(nil)
		th := currentTheme()
		return th.Node["fillcolor"] + " " + th.Node["color"] + " " + th.Node["fontcolor"] + " " + th.Edge["fontcolor"]
	}
	dark := "#373737 #d77757 #ffffff #4eba65"
	light := "#f0f0f0 #d77757 #000000 #2c7a39"
	for _, c := range []struct{ name, settings, older, custom, want string }{
		{"nothing readable", "", "", "", dark},
		{"dark", `{"theme":"dark"}`, "", "", dark},
		{"light", `{"theme":"light"}`, "", "", light},
		{"light-daltonized", `{"theme":"light-daltonized"}`, "", "", "#dcdcdc #ff9933 #000000 #006699"},
		{"dark-ansi draws as dark", `{"theme":"dark-ansi"}`, "", "", dark},
		{"auto is dark", `{"theme":"auto"}`, "", "", dark},
		{"the older file alone", "", `{"theme":"light"}`, "", light},
		{"settings.json over the older file", `{"theme":"dark"}`, `{"theme":"light"}`, "", dark},
		{"custom on a light base, overrides in every form", `{"theme":"custom:x"}`, "",
			`{"name":"x","base":"light","overrides":{"claude":"#123456","text":"rgb(1, 2, 3)","success":"ansi:green","userMessageBackground":"#abc"}}`,
			"#aabbcc #123456 #010203 #2c7a39"},
		{"custom on an ansi base", `{"theme":"custom:x"}`, "", `{"name":"x","base":"dark-ansi"}`, dark},
		{"custom with no file", `{"theme":"custom:x"}`, "", "", dark},
		{"not JSON", `{"theme":`, "", "", dark},
		{"not a string", `{"theme":7}`, "", "", dark},
	} {
		put(".claude/settings.json", c.settings)
		put(".claude.json", c.older)
		put(".claude/themes/x.json", c.custom)
		if got := drawn(); got != c.want {
			t.Errorf("%s: drew %s, want %s", c.name, got, c.want)
		}
	}
}

// A theme file reaches the picture: its fill is the nodes' fill, its edge
// colour the edges', its fontname the face the SVG is set in — and what it
// does not declare is graphviz's default, not the built-in theme's.
func TestThemeFileReachesThePicture(t *testing.T) {
	withTheme(t, `node [fillcolor="#7aa2f71f", fontname="JetBrains Mono"]
	              edge [color=red]`)
	svg, err := renderThemedSVG("digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// graphviz writes an RGBA fill as a colour and an opacity.
	a := svgGroup(svg, "a")
	if !strings.Contains(a, `fill="#7aa2f7" fill-opacity="0.12`) {
		t.Errorf("the theme's fill did not reach the node:\n%s", a)
	}
	if strings.Contains(a, "#d77757") {
		t.Errorf("Claude Code's stroke leaked through a theme that declares its own:\n%s", a)
	}
	if e := svgGroup(svg, "a&#45;&gt;b"); !strings.Contains(e, `stroke="red"`) {
		t.Errorf("the theme's edge colour did not reach the edge:\n%s", e)
	}
	if !strings.Contains(string(svg), `font-family="JetBrains Mono"`) || strings.Contains(string(svg), "Courier") {
		t.Error("the picture is not set in the theme's face")
	}
}

// The model's paint still wins over a theme file, as it does over the
// built-in one.
func TestThemeFileYieldsToTheModel(t *testing.T) {
	withTheme(t, `node [fillcolor="#000000", color="#111111"]`)
	svg, err := renderThemedSVG("digraph { a [color=red]; a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := svgGroup(svg, "a"); !strings.Contains(a, `stroke="red"`) {
		t.Errorf("the theme overwrote the model:\n%s", a)
	}
	if b := svgGroup(svg, "b"); !strings.Contains(b, `stroke="#111111"`) {
		t.Errorf("the theme did not reach an unpainted node:\n%s", b)
	}
}
