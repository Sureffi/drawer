// theme_test.go — laws for a theme file: what it reaches, and what it
// yields to.

package pixel

import (
	"strings"
	"testing"

	"github.com/sureffi/drawer/internal/theme"
)

// mustTheme is the theme a law draws in: the declarations it names, read
// back. Claude Code's own is mustTheme(t, theme.ClaudeDOT()).
func mustTheme(t *testing.T, src string) *theme.Theme {
	t.Helper()
	th, err := theme.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// A theme file reaches the picture: its fill is the nodes' fill, its edge
// colour the edges', its fontname the face the SVG is set in — and what it
// does not declare is graphviz's default, not the built-in theme's.
func TestThemeFileReachesThePicture(t *testing.T) {
	th := mustTheme(t, `node [fillcolor="#7aa2f71f", fontname="JetBrains Mono"]
	                    edge [color=red]`)
	svg, err := RenderThemedSVG(th, "digraph { a -> b }", 0, "")
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
	th := mustTheme(t, `node [fillcolor="#000000", color="#111111"]`)
	svg, err := RenderThemedSVG(th, "digraph { a [color=red]; a -> b }", 0, "")
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
