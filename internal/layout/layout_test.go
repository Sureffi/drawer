// layout_test.go — laws for the one door to graphviz.

package layout

import "testing"

// A fence the model opened and closed has no graph in it, and graphviz does
// not call that an error: ParseBytes answers (nil, nil), because nothing was
// wrong with what it was asked. Everything downstream dereferenced that nil,
// so one empty ```dot fence took the process with it. Every other law in the
// tree passed the whole time it was live, which is the argument for this one.
func TestEmptySourceIsRefusedNotFatal(t *testing.T) {
	for _, src := range []string{
		"",
		"   ",
		"\t\n \n",
		"\ufeff",
		"// just a comment\n",
		"/* nothing */",
		"digraph{}",
	} {
		if _, _, ok := Fit(src, 90, 0); ok {
			t.Errorf("laid out a source with no graph in it: %q", src)
		}
	}
}
