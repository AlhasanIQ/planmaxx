// Package patches resolves model-supplied patch hunks against immutable base
// content. Hints are never authoritative: every hunk must uniquely match its
// surrounding content before the complete patch set is applied atomically.
package patches

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
)

// Hunk describes a replacement in immutable base content. Before and After
// are optional exact adjacent context; Expected is the source text to replace.
// Target and numeric hints document model intent only and do not control where
// the server applies the change.
type Hunk struct {
	Before, Expected, After, Content string
	Target                           string
	StartHint, EndHint               int
}

// Resolved is one uniquely located hunk. Byte offsets are private patch
// machinery; line/UTF-16 positions let review metadata keep browser anchors.
type Resolved struct {
	Hunk                   Hunk
	StartOffset, EndOffset int
	StartLine, StartChar   int
	EndLine, EndChar       int
}

func Resolve(base string, hunks []Hunk) ([]Resolved, error) {
	if len(hunks) == 0 {
		return nil, errors.New("patch has no hunks")
	}
	out := make([]Resolved, 0, len(hunks))
	for _, h := range hunks {
		if h.Expected == "" {
			return nil, errors.New("patch hunk requires expected source text")
		}
		var matches []Resolved
		for offset := 0; ; {
			index := strings.Index(base[offset:], h.Expected)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(h.Expected)
			if contextMatches(base, start, end, h) {
				startLine, startChar := PositionAt(base, start)
				endLine, endChar := PositionAt(base, end)
				matches = append(matches, Resolved{Hunk: h, StartOffset: start, EndOffset: end, StartLine: startLine, StartChar: startChar, EndLine: endLine, EndChar: endChar})
			}
			offset = start + 1
			if offset >= len(base) {
				break
			}
		}
		if len(matches) != 1 {
			return nil, errors.New("patch hunk is missing or ambiguous in the base revision")
		}
		out = append(out, matches[0])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartOffset < out[j].StartOffset })
	for i := 1; i < len(out); i++ {
		if out[i].StartOffset < out[i-1].EndOffset {
			return nil, errors.New("patch hunks overlap")
		}
	}
	return out, nil
}

func Apply(base string, resolved []Resolved) string {
	for i := len(resolved) - 1; i >= 0; i-- {
		r := resolved[i]
		base = base[:r.StartOffset] + r.Hunk.Content + base[r.EndOffset:]
	}
	return base
}

func contextMatches(base string, start, end int, h Hunk) bool {
	return beforeMatches(base, start, h.Before) && afterMatches(base, end, h.After)
}

func beforeMatches(base string, start int, before string) bool {
	if before == "" {
		return true
	}
	if start >= len(before) && base[start-len(before):start] == before {
		return true
	}
	// Full-line hunks commonly express neighbouring lines without their newline
	// delimiter. Accept that representation only at a true line boundary.
	if start > 0 && base[start-1] == '\n' {
		lineStart := strings.LastIndex(base[:start-1], "\n") + 1
		return base[lineStart:start-1] == before
	}
	return false
}

func afterMatches(base string, end int, after string) bool {
	if after == "" {
		return true
	}
	if len(base)-end >= len(after) && base[end:end+len(after)] == after {
		return true
	}
	if end < len(base) && base[end] == '\n' {
		lineEnd := strings.Index(base[end+1:], "\n")
		if lineEnd < 0 {
			return base[end+1:] == after
		}
		return base[end+1:end+1+lineEnd] == after
	}
	return false
}

// PositionAt converts a byte offset in UTF-8 text to the 1-based line and
// zero-based UTF-16 column used by browser/editor selections.
func PositionAt(text string, offset int) (line, character int) {
	line = 1
	lineStart := 0
	for index := 0; index < offset; index++ {
		if text[index] == '\n' {
			line++
			lineStart = index + 1
		}
	}
	return line, utf16Length(text[lineStart:offset])
}

func utf16Length(text string) int {
	return len(utf16.Encode([]rune(text)))
}
