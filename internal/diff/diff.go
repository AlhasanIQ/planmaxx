package diff

import (
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	KindContext = "context"
	KindRemove  = "remove"
	KindAdd     = "add"
)

const maxRefinedReplacementCells = 250_000

type Line struct {
	Kind   string `json:"kind"`
	Before int    `json:"before,omitempty"`
	After  int    `json:"after,omitempty"`
	Text   string `json:"text"`
}

func Lines(before string, after string) []Line {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	if before == after {
		return appendEqual(nil, beforeLines, 0, len(beforeLines), 0)
	}

	// DiffMatchPatch reduces each unique line to one rune before comparing. It
	// avoids SequenceMatcher's pathological slowdowns on long Markdown plans
	// with repeated bullets or blank lines. The deadline keeps the UI responsive
	// even for a wholesale rewrite; its fallback remains a correct replacement.
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 75 * time.Millisecond
	beforeRunes, afterRunes, lineTable := dmp.DiffLinesToRunes(before, after)
	diffs := dmp.DiffMainRunes(beforeRunes, afterRunes, false)

	beforeIndex, afterIndex := 0, 0
	out := make([]Line, 0, len(beforeLines)+len(afterLines))
	for index := 0; index < len(diffs); {
		if diffs[index].Type == diffmatchpatch.DiffEqual {
			for _, text := range diffLineTexts(diffs[index], lineTable) {
				out = append(out, Line{Kind: KindContext, Before: beforeIndex + 1, After: afterIndex + 1, Text: text})
				beforeIndex++
				afterIndex++
			}
			index++
			continue
		}

		var removed, added []string
		for index < len(diffs) && diffs[index].Type != diffmatchpatch.DiffEqual {
			switch diffs[index].Type {
			case diffmatchpatch.DiffDelete:
				removed = append(removed, diffLineTexts(diffs[index], lineTable)...)
			case diffmatchpatch.DiffInsert:
				added = append(added, diffLineTexts(diffs[index], lineTable)...)
			}
			index++
		}
		out = appendReplacement(out, removed, added, beforeIndex, afterIndex)
		beforeIndex += len(removed)
		afterIndex += len(added)
	}

	// DiffMatchPatch treats the newline as part of the preceding physical line;
	// PlanMaxx deliberately retains the final empty source row for stable line
	// numbers and anchors.
	return appendRemaining(out, beforeLines, afterLines, beforeIndex, afterIndex)
}

func diffLineTexts(diff diffmatchpatch.Diff, lineTable []string) []string {
	lines := make([]string, 0, len(diff.Text))
	for _, lineRune := range diff.Text {
		lines = append(lines, strings.TrimSuffix(lineTable[int(lineRune)], "\n"))
	}
	return lines
}

func appendReplacement(out []Line, before, after []string, beforeBase, afterBase int) []Line {
	if len(before)*len(after) > maxRefinedReplacementCells {
		for index, text := range before {
			out = append(out, Line{Kind: KindRemove, Before: beforeBase + index + 1, Text: text})
		}
		for index, text := range after {
			out = append(out, Line{Kind: KindAdd, After: afterBase + index + 1, Text: text})
		}
		return out
	}

	matcher := difflib.NewMatcher(before, after)
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			for index := op.I1; index < op.I2; index++ {
				out = append(out, Line{Kind: KindContext, Before: beforeBase + index + 1, After: afterBase + op.J1 + (index - op.I1) + 1, Text: before[index]})
			}
		case 'd':
			for index := op.I1; index < op.I2; index++ {
				out = append(out, Line{Kind: KindRemove, Before: beforeBase + index + 1, Text: before[index]})
			}
		case 'i':
			for index := op.J1; index < op.J2; index++ {
				out = append(out, Line{Kind: KindAdd, After: afterBase + index + 1, Text: after[index]})
			}
		case 'r':
			for index := op.I1; index < op.I2; index++ {
				out = append(out, Line{Kind: KindRemove, Before: beforeBase + index + 1, Text: before[index]})
			}
			for index := op.J1; index < op.J2; index++ {
				out = append(out, Line{Kind: KindAdd, After: afterBase + index + 1, Text: after[index]})
			}
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

func appendRemaining(out []Line, before, after []string, beforeIndex, afterIndex int) []Line {
	for beforeIndex < len(before) && afterIndex < len(after) && before[beforeIndex] == after[afterIndex] {
		out = append(out, Line{Kind: KindContext, Before: beforeIndex + 1, After: afterIndex + 1, Text: before[beforeIndex]})
		beforeIndex++
		afterIndex++
	}
	for beforeIndex < len(before) {
		out = append(out, Line{Kind: KindRemove, Before: beforeIndex + 1, Text: before[beforeIndex]})
		beforeIndex++
	}
	for afterIndex < len(after) {
		out = append(out, Line{Kind: KindAdd, After: afterIndex + 1, Text: after[afterIndex]})
		afterIndex++
	}
	return out
}
