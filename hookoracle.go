package main

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
//     output is either the same bytes or the drawn block for the same
//     source.
//  3. The block is well formed: a bare fence that fits its width with a
//     drawing in it, or the notice that says why not with the source
//     under it. checkDrawn is that law.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// runDeltas replays a recorded delta stream through the transducer and
// checks what a reader would have seen against the text CC handed us.
//
//	drawer -deltas testdata/deltas-split-a.jsonl -size 100x40
func (r run) runDeltas(path string, w int) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-deltas:", err)
		return 1
	}
	defer f.Close()

	emit := func(src string, indent int) []string { return r.drawBlock(src, w-indent) }
	var in, shown strings.Builder
	var st state
	// A recording is in the order the hook processes wrote it, which is the
	// order Claude Code started them and not the order of the deltas —
	// testdata/deltas-race.jsonl has the second before the first. The hook
	// takes its turn by index, so the transducer sees a message's deltas in
	// index order, and the replay does the same.
	var deltas []hookIn
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
		deltas = append(deltas, p)
	}
	first := map[string]int{}
	for i, p := range deltas {
		if _, ok := first[p.MessageID]; !ok {
			first[p.MessageID] = i
		}
	}
	sort.SliceStable(deltas, func(i, j int) bool {
		a, b := deltas[i], deltas[j]
		if a.MessageID != b.MessageID {
			return first[a.MessageID] < first[b.MessageID]
		}
		return a.Index < b.Index
	})
	for _, p := range deltas {
		in.WriteString(p.Delta)
		shown.WriteString(stream(p.Delta, p.Final, &st, emit))
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
		f, _ := openerOf(strings.SplitN(sf, "\n", 2)[0])
		if err := checkDrawn(of, fenceBody(sf, f), w); err != nil {
			fmt.Fprintf(os.Stderr, "-deltas: %s: fence %d: %v\n", path, i+1, err)
			return 1
		}
		drawn++
	}
	fmt.Printf("%d fences, %d replaced, %d returned whole\n",
		len(srcFences), drawn, len(srcFences)-drawn)
	return 0
}

// splitFences pulls every fence out of text — as the transducer reads
// one, whatever it is labelled, so a fence quoted inside another is the
// content it is — and returns them in order alongside the text that
// remains, each block replaced by one NUL so the prose comparison stays
// positional.
func splitFences(text string) ([]string, string) {
	var fences []string
	var prose strings.Builder
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		f, ok := openerOf(lines[i])
		end := -1
		if ok {
			for j := i + 1; j < len(lines); j++ {
				if f.closes(lines[j]) {
					end = j
					break
				}
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

// checkDrawn is law 3: the fence is bare, every row fits the width it was
// drawn for, and something was actually drawn — a stroke, a placeholder,
// or the notice that says why not, with the source under it.
func checkDrawn(block, src string, w int) error {
	lines := strings.Split(block, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != fenceTick ||
		strings.TrimSpace(lines[len(lines)-1]) != fenceTick {
		return fmt.Errorf("drawn block is not a bare fence")
	}
	body := lines[1 : len(lines)-1]
	drawn := false
	for _, row := range body {
		plain := stripSGR(row)
		if n := textCells(plain); n > w {
			return fmt.Errorf("row is %d cells in %d columns: %q", n, w, plain)
		}
		if strings.ContainsAny(plain, "─│╭╮╰╯▶◀▲▼") || strings.ContainsRune(plain, placeholderRune) ||
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

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
