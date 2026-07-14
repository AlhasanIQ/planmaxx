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
		var exactMatches []Resolved
		for offset := 0; ; {
			index := strings.Index(base[offset:], h.Expected)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(h.Expected)
			startLine, startChar := PositionAt(base, start)
			endLine, endChar := PositionAt(base, end)
			candidate := Resolved{Hunk: h, StartOffset: start, EndOffset: end, StartLine: startLine, StartChar: startChar, EndLine: endLine, EndChar: endChar}
			exactMatches = append(exactMatches, candidate)
			if contextMatches(base, start, end, h) {
				matches = append(matches, candidate)
			}
			offset = start + 1
			if offset >= len(base) {
				break
			}
		}
		if len(matches) == 1 {
			out = append(out, matches[0])
			continue
		}
		// Exact source text is sufficient when it occurs once. This safely
		// recovers from a model miscounting or slightly miscopying optional
		// surrounding context, without using fragile line-number hints.
		if len(matches) == 0 && len(exactMatches) == 1 {
			out = append(out, exactMatches[0])
			continue
		}
		return nil, errors.New("patch hunk is missing or ambiguous in the base revision")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartOffset < out[j].StartOffset })
	for i := range out {
		if out[i].Hunk.Target == "lines" && out[i].Hunk.Content == "" {
			lowerBound := 0
			if i > 0 {
				lowerBound = out[i-1].EndOffset
			}
			out[i].StartOffset, out[i].EndOffset = fullLineDeletionOffsets(base, out[i].StartOffset, out[i].EndOffset, lowerBound)
		}
		if i > 0 && out[i].StartOffset < out[i-1].EndOffset {
			return nil, errors.New("patch hunks overlap")
		}
	}
	trimTerminalDeletionDelimiter(base, out)
	return out, nil
}

// Adjacent full-line deletions that reach EOF otherwise leave the delimiter
// before the deleted block behind. Move that delimiter into the first hunk in
// the contiguous group, preserving the single-last-line deletion behavior.
func trimTerminalDeletionDelimiter(base string, resolved []Resolved) {
	if len(resolved) == 0 {
		return
	}
	last := len(resolved) - 1
	if !isFullLineDeletion(resolved[last]) || resolved[last].EndOffset != len(base) {
		return
	}
	first := last
	for first > 0 && isFullLineDeletion(resolved[first-1]) && resolved[first-1].EndOffset == resolved[first].StartOffset {
		first--
	}
	// A single trailing deletion already chose either the leading or trailing
	// delimiter in fullLineDeletionOffsets. This correction is only for an
	// adjacent group where the last hunk could not claim its leading delimiter.
	if first == last || strings.HasSuffix(resolved[last].Hunk.Expected, "\n") ||
		!strings.HasSuffix(base[:resolved[last].EndOffset], resolved[last].Hunk.Expected) {
		return
	}
	start := resolved[first].StartOffset
	delimiterStart := start
	if start >= 2 && base[start-2:start] == "\r\n" {
		delimiterStart = start - 2
	} else if start > 0 && base[start-1] == '\n' {
		delimiterStart = start - 1
	}
	if delimiterStart == start || (first > 0 && delimiterStart < resolved[first-1].EndOffset) {
		return
	}
	resolved[first].StartOffset = delimiterStart
}

func isFullLineDeletion(resolved Resolved) bool {
	return resolved.Hunk.Target == "lines" && resolved.Hunk.Content == ""
}

func fullLineDeletionOffsets(base string, start, end, lowerBound int) (int, int) {
	startsAtLineBoundary := start == 0 || base[start-1] == '\n'
	lineDelimiterEnd := end
	switch {
	case strings.HasPrefix(base[end:], "\r\n"):
		lineDelimiterEnd = end + 2
	case end < len(base) && base[end] == '\n':
		lineDelimiterEnd = end + 1
	}
	endsAtLineBoundary := end == len(base) || lineDelimiterEnd > end
	if !startsAtLineBoundary || !endsAtLineBoundary {
		return start, end
	}
	if lineDelimiterEnd > end {
		return start, lineDelimiterEnd
	}
	if start >= 2 && base[start-2:start] == "\r\n" && start-2 >= lowerBound {
		return start - 2, end
	}
	if start > 0 && base[start-1] == '\n' && start-1 >= lowerBound {
		return start - 1, end
	}
	return start, end
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
