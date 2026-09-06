// theme_test.go — laws for a theme file: what it reaches, and what it
// yields to.

package pixel

import (
	"testing"

	"github.com/sureffi/drawer/internal/theme"
)

// mustTheme is the theme a law draws in: the declarations it names, read
// back. Claude Code's own is mustTheme(t, theme.ClaudeDOT()).
func mustTheme(t *testing.T, src string) *theme.Theme {
	t.Helper()
	th, err := theme.Parse(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// A theme file reaches the picture: its fill is the nodes' fill and its
// edge colour the edges' — and what it does not declare is graphviz's
// default, not the built-in theme's. Its fontname is the one declaration
// that does not reach the picture unless it names a font file: the layout
// is measured in Courier whatever anybody says, and paint.go sets it in
// the face this binary carries.
func TestThemeFileReachesThePicture(t *testing.T) {
	th := mustTheme(t, `node [fillcolor="#7aa2f71f", fontname="JetBrains Mono"]
	                    edge [color=red]`)
	d, err := RenderThemed(t.Context(), th, "digraph { a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	a := d.Object("a")
	if a == nil {
		t.Fatal("the drawing has no node a")
	}
	if !inked(a.Draw, "C", "#7aa2f71f") {
		t.Errorf("the theme's fill did not reach the node:\n%+v", a.Draw)
	}
	if inked(a.Draw, "c", "#d77757") {
		t.Errorf("Claude Code's stroke leaked through a theme that declares its own:\n%+v", a.Draw)
	}
	if e := d.Between("a", "b"); e == nil || !inked(e.Draw, "c", "#ff0000") {
		t.Errorf("the theme's edge colour did not reach the edge:\n%+v", e)
	}
	if !hasFace(a.LDraw, pxLayoutFont) {
		t.Errorf("the layout was not measured in %s:\n%+v", pxLayoutFont, a.LDraw)
	}
	if f := fontFile(th.Face()); f != "" {
		t.Errorf("a family name was taken for a font file and resolved to %q", f)
	}
}

// The model's paint still wins over a theme file, as it does over the
// built-in one.
func TestThemeFileYieldsToTheModel(t *testing.T) {
	th := mustTheme(t, `node [fillcolor="#000000", color="#111111"]`)
	d, err := RenderThemed(t.Context(), th, "digraph { a [color=red]; a -> b }", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := d.Object("a"); a == nil || !inked(a.Draw, "c", "#ff0000") {
		t.Errorf("the theme overwrote the model:\n%+v", a)
	}
	if b := d.Object("b"); b == nil || !inked(b.Draw, "c", "#111111") {
		t.Errorf("the theme did not reach an unpainted node:\n%+v", b)
	}
}
