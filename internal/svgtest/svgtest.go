// svgtest.go — graphviz's SVG, as a law reads it.
//
// Two laws in two packages read the same picture: the pixels rung asserts
// on the SVG it writes, and the ledger asserts on the SVG a repaint writes.
// Both need the same two readers, and both carried their own byte-identical
// copy — two copies of an answer that has to agree. A package of readers is
// where Go puts one shared between laws, as net/http/httptest and
// testing/iotest are, and it is the only thing in this tree that exists for
// the laws rather than for the binary.

package svgtest

import (
	"regexp"
	"strings"

	"github.com/sureffi/drawer/internal/layout"
)

// Group is the SVG of one titled element: a node, an edge or a cluster.
func Group(svg []byte, title string) string {
	s := string(svg)
	i := strings.Index(s, "<title>"+title+"</title>")
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "</g>")
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

// textY reads the baseline off the first text of a group.
var textY = regexp.MustCompile(`<text [^>]*\by="(-?[0-9.]+)"`)

// TextY is the baseline of the first text in a group: where graphviz put
// the thing, up the page as it goes negative.
func TextY(group string) float64 {
	m := textY.FindStringSubmatch(group)
	if m == nil {
		return 0
	}
	return layout.Atof(m[1])
}
