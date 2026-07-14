package diff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
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
	beforeTokens, afterTokens, lines := lineTokens(beforeLines, afterLines)
	dmp := diffmatchpatch.New()
	// A review diff must be deterministic. Disabling the deadline prevents the
	// algorithm from returning a coarser result merely because a machine was busy.
	dmp.DiffTimeout = 0

	var out []Line
	beforeNumber, afterNumber := 1, 1
	for _, change := range dmp.DiffMainRunes(beforeTokens, afterTokens, false) {
		for _, token := range change.Text {
			line := lines[token]
			switch change.Type {
			case diffmatchpatch.DiffEqual:
				out = append(out, Line{Kind: KindContext, Before: beforeNumber, After: afterNumber, Text: line})
				beforeNumber++
				afterNumber++
			case diffmatchpatch.DiffDelete:
				out = append(out, Line{Kind: KindRemove, Before: beforeNumber, Text: line})
				beforeNumber++
			case diffmatchpatch.DiffInsert:
				out = append(out, Line{Kind: KindAdd, After: afterNumber, Text: line})
				afterNumber++
			}
		}
	}
	return out
}

// lineTokens interns complete logical lines into runes so the dependency runs
// its maintained Myers-style diff over lines, not characters. The surrounding
// wrapper remains PlanMaxx's stable row contract.
func lineTokens(before, after []string) ([]rune, []rune, map[rune]string) {
	byLine := make(map[string]rune, len(before)+len(after))
	byToken := make(map[rune]string, len(before)+len(after))
	next := rune(1)
	encode := func(source []string) []rune {
		out := make([]rune, 0, len(source))
		for _, line := range source {
			token, ok := byLine[line]
			if !ok {
				if next == 0xD800 {
					next = 0xE000
				}
				token = next
				next++
				byLine[line] = token
				byToken[token] = line
			}
			out = append(out, token)
		}
		return out
	}
	return encode(before), encode(after), byToken
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
