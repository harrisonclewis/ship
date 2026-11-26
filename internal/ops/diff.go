package ops

import (
	"bytes"
	"fmt"
	"strings"
)

// UnifiedDiff renders a small, readable unified diff. It is used for app
// records and compose files, which are text and normally only a few KB.
func UnifiedDiff(oldName, newName string, old, new []byte) string {
	if bytes.Equal(old, new) {
		return ""
	}
	a := splitDiffLines(old)
	b := splitDiffLines(new)
	ops := diffOps(a, b)

	const context = 3
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", oldName, newName)
	for _, h := range diffHunks(ops, context) {
		oldStart, newStart := 0, 0
		for _, op := range ops[:h.start] {
			switch op.kind {
			case ' ', '-':
				oldStart++
			}
			switch op.kind {
			case ' ', '+':
				newStart++
			}
		}
		oldLen, newLen := 0, 0
		for _, op := range ops[h.start:h.end] {
			switch op.kind {
			case ' ', '-':
				oldLen++
			}
			switch op.kind {
			case ' ', '+':
				newLen++
			}
		}
		if oldLen > 0 {
			oldStart++
		}
		if newLen > 0 {
			newStart++
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldLen, newStart, newLen)
		for _, op := range ops[h.start:h.end] {
			writeDiffLine(&out, op.kind, op.line)
		}
	}
	return out.String()
}

type diffOp struct {
	kind byte
	line string
}

type diffHunk struct {
	start int
	end   int
}

// splitDiffLines keeps each line's terminator so a missing final newline is
// visible in the diff.
func splitDiffLines(b []byte) []string {
	var lines []string
	for len(b) > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			lines = append(lines, string(b[:i+1]))
			b = b[i+1:]
			continue
		}
		lines = append(lines, string(b))
		break
	}
	return lines
}

func diffOps(a, b []string) []diffOp {
	// A pathological pair of files should not allocate a huge table.
	if len(a)*len(b) > 4_000_000 {
		ops := make([]diffOp, 0, len(a)+len(b))
		for _, line := range a {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{'+', line})
		}
		return ops
	}

	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	ops := make([]diffOp, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

func diffHunks(ops []diffOp, context int) []diffHunk {
	var hunks []diffHunk
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		from := i - context
		if from < 0 {
			from = 0
		}
		to := i + context + 1
		if to > len(ops) {
			to = len(ops)
		}
		if len(hunks) > 0 && from <= hunks[len(hunks)-1].end {
			if to > hunks[len(hunks)-1].end {
				hunks[len(hunks)-1].end = to
			}
			continue
		}
		hunks = append(hunks, diffHunk{start: from, end: to})
	}
	return hunks
}

func writeDiffLine(out *strings.Builder, kind byte, line string) {
	out.WriteByte(kind)
	if strings.HasSuffix(line, "\n") {
		out.WriteString(line)
		return
	}
	out.WriteString(line)
	out.WriteString("\n\\ No newline at end of file\n")
}
