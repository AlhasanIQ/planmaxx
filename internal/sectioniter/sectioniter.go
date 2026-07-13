package sectioniter

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/AlhasanIQ/planmaxx/internal/patches"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

// ParsedResponse is the sole, content-anchored section-iteration response.
// Positional replacement protocols were never released and are intentionally
// not retained as a compatibility path.
type ParsedResponse struct {
	RevisionID string
	Summary    string
	Hunks      []patches.Hunk
}

func SectionForAnchor(plan string, anchor session.Anchor) (string, error) {
	lines := strings.Split(plan, "\n")
	if err := validateAnchor(lines, anchor); err != nil {
		return "", err
	}
	if !hasCharacterRange(anchor) {
		return strings.Join(lines[anchor.StartLine-1:anchor.EndLine], "\n"), nil
	}
	if anchor.StartLine == anchor.EndLine {
		line := lines[anchor.StartLine-1]
		start, err := utf16OffsetToByteIndex(line, anchor.StartChar)
		if err != nil {
			return "", err
		}
		end, err := utf16OffsetToByteIndex(line, anchor.EndChar)
		if err != nil {
			return "", err
		}
		return line[start:end], nil
	}
	startLine := lines[anchor.StartLine-1]
	start, err := utf16OffsetToByteIndex(startLine, anchor.StartChar)
	if err != nil {
		return "", err
	}
	endLine := lines[anchor.EndLine-1]
	end, err := utf16OffsetToByteIndex(endLine, anchor.EndChar)
	if err != nil {
		return "", err
	}
	selected := []string{startLine[start:]}
	selected = append(selected, lines[anchor.StartLine:anchor.EndLine-1]...)
	selected = append(selected, endLine[:end])
	return strings.Join(selected, "\n"), nil
}

func validateAnchor(lines []string, anchor session.Anchor) error {
	if anchor.StartLine <= 0 || anchor.EndLine < anchor.StartLine || anchor.EndLine > len(lines) {
		return fmt.Errorf("anchor is outside plan")
	}
	if !hasCharacterRange(anchor) {
		return nil
	}
	if anchor.StartChar < 0 || anchor.EndChar < 0 {
		return errors.New("anchor character offsets must be non-negative")
	}
	startLength := utf16Length(lines[anchor.StartLine-1])
	endLength := utf16Length(lines[anchor.EndLine-1])
	if anchor.StartChar > startLength || anchor.EndChar > endLength {
		return errors.New("anchor character offset is outside plan")
	}
	if anchor.StartLine == anchor.EndLine && anchor.EndChar < anchor.StartChar {
		return errors.New("anchor end character is before start character")
	}
	return nil
}

func hasCharacterRange(anchor session.Anchor) bool {
	return anchor.StartChar != 0 || anchor.EndChar != 0
}

func utf16Length(s string) int { return len(utf16.Encode([]rune(s))) }

func utf16OffsetToByteIndex(s string, offset int) (int, error) {
	if offset < 0 {
		return 0, errors.New("utf16 offset must be non-negative")
	}
	seen := 0
	for index, char := range s {
		if seen == offset {
			return index, nil
		}
		if char > 0xffff {
			seen += 2
		} else {
			seen++
		}
		if seen > offset {
			return 0, errors.New("utf16 offset splits a surrogate pair")
		}
	}
	if seen == offset {
		return len(s), nil
	}
	return 0, errors.New("utf16 offset is outside string")
}
