package diff

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

const (
	KindContext = "context"
	KindRemove  = "remove"
	KindAdd     = "add"
)

type Line struct {
	Kind   string `json:"kind"`
	Before int    `json:"before,omitempty"`
	After  int    `json:"after,omitempty"`
	Text   string `json:"text"`
}

func Lines(before string, after string) []Line {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	// Keep go-difflib behind this wrapper. Its SequenceMatcher opcodes match the
	// structured line model PlanMaxx needs, while newer patch-oriented packages
	// either emit unified patches or less readable short-line hunks.
	matcher := difflib.NewMatcher(beforeLines, afterLines)

	var out []Line
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			out = appendEqual(out, beforeLines, op.I1, op.I2, op.J1)
		case 'd':
			out = appendRemove(out, beforeLines, op.I1, op.I2)
		case 'i':
			out = appendAdd(out, afterLines, op.J1, op.J2)
		case 'r':
			out = appendRemove(out, beforeLines, op.I1, op.I2)
			out = appendAdd(out, afterLines, op.J1, op.J2)
		}
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func appendEqual(out []Line, lines []string, beforeStart int, beforeEnd int, afterStart int) []Line {
	for i := beforeStart; i < beforeEnd; i++ {
		out = append(out, Line{
			Kind:   KindContext,
			Before: i + 1,
			After:  afterStart + (i - beforeStart) + 1,
			Text:   lines[i],
		})
	}
	return out
}

func appendRemove(out []Line, lines []string, start int, end int) []Line {
	for i := start; i < end; i++ {
		out = append(out, Line{
			Kind:   KindRemove,
			Before: i + 1,
			Text:   lines[i],
		})
	}
	return out
}

func appendAdd(out []Line, lines []string, start int, end int) []Line {
	for i := start; i < end; i++ {
		out = append(out, Line{
			Kind:  KindAdd,
			After: i + 1,
			Text:  lines[i],
		})
	}
	return out
}
