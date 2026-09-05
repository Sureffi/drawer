package drawer

// The oracle for the hook wire. Not a golden file: the laws below are
// checked against the input the fixture carries, so a fixture cannot bless
// whatever the code happens to do today.
//
// Three laws, and they are the ones that would actually hurt:
//
//  1. Prose is not ours. Strip every fence from the input and from what
//     was displayed, and the remainder must be byte-identical. That
//     catches a lost delta, a doubled one, reordering, and any damage to
//     a sentence that merely mentions a fence.
//  2. A fence is replaced or returned, never eaten. Each fence in the
//     input maps in order to exactly one fence in the output, and that
//     output is either the same bytes or the mode's block for the same
//     source.
//  3. The block is well formed. What that means is the mode's to say —
//     Draw wants a bare fence that fits its width with a drawing in it;
//     a reserve wants its caption, its source and its pad rows — so the
//     mode carries the check beside the emit.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RunDeltas replays a recorded delta stream through the transducer in the
// given mode and checks what a reader would have seen against the text CC
// handed us.
//
//	drawer -deltas fixtures/deltas-split-a.jsonl -size 100x40
func RunDeltas(path string, w, rows int, m Mode) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-deltas:", err)
		return 1
	}
	defer f.Close()

	emit := func(src string) []string { return m.Emit(src, w, rows) }
	var in, shown strings.Builder
	var st State
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		line++
		if raw == "" {
			continue
		}
		var p hookIn
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			fmt.Fprintf(os.Stderr, "-deltas: %s:%d: %v\n", path, line, err)
			return 1
		}
		in.WriteString(p.Delta)
		shown.WriteString(Stream(p.Delta, p.Final, &st, emit))
	}
	if st.InFence {
		fmt.Fprintf(os.Stderr, "-deltas: %s: stream ended with a fence still held — "+
			"its text was suppressed and never given back\n", path)
		return 1
	}

	srcFences, srcProse := splitFences(in.String())
	outFences, outProse := splitFences(shown.String())

	if srcProse != outProse {
		fmt.Fprintf(os.Stderr, "-deltas: %s: prose outside fences was altered\n", path)
		fmt.Fprintf(os.Stderr, "  handed to us: %q\n", clip(srcProse))
		fmt.Fprintf(os.Stderr, "  displayed:    %q\n", clip(outProse))
		return 1
	}
	if len(srcFences) != len(outFences) {
		fmt.Fprintf(os.Stderr, "-deltas: %s: %d fences in, %d out\n",
			path, len(srcFences), len(outFences))
		return 1
	}

	drawn := 0
	for i, sf := range srcFences {
		of := outFences[i]
		if sf == of {
			continue // returned untouched: the honest failure
		}
		if err := m.Check(of, fenceBody(sf), w, rows); err != nil {
			fmt.Fprintf(os.Stderr, "-deltas: %s: fence %d: %v\n", path, i+1, err)
			return 1
		}
		drawn++
	}
	fmt.Printf("%d fences, %d replaced, %d returned whole\n",
		len(srcFences), drawn, len(srcFences)-drawn)
	return 0
}

// splitFences pulls every ``` block out of text and returns them in order
// alongside the text that remains, each block replaced by one NUL so the
// prose comparison stays positional.
func splitFences(text string) ([]string, string) {
	var fences []string
	var prose strings.Builder
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if t != FenceOpen && t != FenceTick {
			prose.WriteString(lines[i])
			prose.WriteString("\n")
			continue
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimRight(lines[j], " \t") == FenceTick {
				end = j
				break
			}
		}
		if end < 0 {
			prose.WriteString(lines[i])
			prose.WriteString("\n")
			continue
		}
		fences = append(fences, strings.Join(lines[i:end+1], "\n"))
		prose.WriteString("\x00\n")
		i = end
	}
	return fences, prose.String()
}

// checkDrawn is law 3 for Draw: the fence is bare, every row fits the
// width it was drawn for, and something was actually drawn — a stroke, a
// placeholder, or the notice that says why not, with the source under it.
// Rows are not a ceiling here; a drawing scrolls.
func checkDrawn(block, src string, w, _ int) error {
	lines := strings.Split(block, "\n")
	if len(lines) < 3 || strings.TrimRight(lines[0], " \t") != FenceTick ||
		strings.TrimRight(lines[len(lines)-1], " \t") != FenceTick {
		return fmt.Errorf("drawn block is not a bare fence")
	}
	body := lines[1 : len(lines)-1]
	drawn := false
	for _, row := range body {
		plain := stripSGR(row)
		if n := textCells(plain); n > w {
			return fmt.Errorf("row is %d cells in %d columns: %q", n, w, plain)
		}
		if strings.ContainsAny(plain, "─│╭╮╰╯▶◀▲▼") || strings.ContainsRune(plain, PlaceholderRune) ||
			strings.Contains(plain, "no diagram") || hasSubcellInk(plain) {
			drawn = true
		}
	}
	if !drawn {
		return fmt.Errorf("fence was replaced and nothing was drawn in its place")
	}
	if strings.Contains(block, "no diagram") && !strings.Contains(block, strings.TrimSpace(strings.Split(src, "\n")[0])) {
		return fmt.Errorf("a notice without the source it is about")
	}
	return nil
}

// hasSubcellInk reports a braille or octant stroke in a row.
func hasSubcellInk(s string) bool {
	for _, r := range s {
		if (r > 0x2800 && r <= 0x28FF) || (r >= 0x1CD00 && r <= 0x1CDE5) || (r >= 0x1CEA0 && r <= 0x1CEAF) {
			return true
		}
	}
	return false
}

// stripSGR drops colour escapes so a row can be measured in cells.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
