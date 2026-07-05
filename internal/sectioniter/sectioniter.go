package sectioniter

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

type ParsedResponse struct {
	Summary     string
	Replacement string
}

func ParseResponse(raw string) (ParsedResponse, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	open := -1
	fenceLength := 0
	for i, line := range lines {
		if n, ok := parseMarkdownFenceOpen(line); ok {
			open = i
			fenceLength = n
			break
		}
	}
	if open < 0 {
		return ParsedResponse{}, errors.New("section iteration response is missing a markdown replacement fence")
	}

	close := -1
	for i := len(lines) - 1; i > open; i-- {
		if isMarkdownFenceClose(lines[i], fenceLength) {
			close = i
			break
		}
	}
	if close < 0 {
		return ParsedResponse{}, errors.New("section iteration response is missing a markdown replacement fence")
	}
	if strings.TrimSpace(strings.Join(lines[close+1:], "\n")) != "" {
		return ParsedResponse{}, errors.New("section iteration response has content after the markdown replacement fence")
	}

	replacement := strings.Trim(strings.Join(lines[open+1:close], "\n"), "\n")
	if strings.TrimSpace(replacement) == "" {
		return ParsedResponse{}, errors.New("section iteration response has an empty markdown replacement")
	}
	summary := parseSummary(strings.Join(lines[:open], "\n"))
	return ParsedResponse{
		Summary:     summary,
		Replacement: replacement,
	}, nil
}

func parseMarkdownFenceOpen(line string) (int, bool) {
	trimmed := strings.TrimSpace(line)
	count := countLeadingBackticks(trimmed)
	if count < 3 {
		return 0, false
	}
	info := strings.TrimSpace(trimmed[count:])
	if info == "" || strings.EqualFold(info, "markdown") || strings.EqualFold(info, "md") {
		return count, true
	}
	return 0, false
}

func isMarkdownFenceClose(line string, openLength int) bool {
	trimmed := strings.TrimSpace(line)
	count := countLeadingBackticks(trimmed)
	return count >= openLength && strings.TrimSpace(trimmed[count:]) == ""
}

func countLeadingBackticks(s string) int {
	count := 0
	for count < len(s) && s[count] == '`' {
		count++
	}
	return count
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
		if end < start {
			return "", errors.New("anchor end character is before start character")
		}
		return line[start:end], nil
	}

	selected := make([]string, 0, anchor.EndLine-anchor.StartLine+1)
	startLine := lines[anchor.StartLine-1]
	start, err := utf16OffsetToByteIndex(startLine, anchor.StartChar)
	if err != nil {
		return "", err
	}
	selected = append(selected, startLine[start:])
	for line := anchor.StartLine; line < anchor.EndLine-1; line++ {
		selected = append(selected, lines[line])
	}
	endLine := lines[anchor.EndLine-1]
	end, err := utf16OffsetToByteIndex(endLine, anchor.EndChar)
	if err != nil {
		return "", err
	}
	selected = append(selected, endLine[:end])
	return strings.Join(selected, "\n"), nil
}

func ReplaceSection(plan string, anchor session.Anchor, replacement string) (string, error) {
	lines := strings.Split(plan, "\n")
	if err := validateAnchor(lines, anchor); err != nil {
		return "", err
	}
	if !hasCharacterRange(anchor) {
		out := make([]string, 0, len(lines)-(anchor.EndLine-anchor.StartLine+1)+strings.Count(replacement, "\n")+1)
		out = append(out, lines[:anchor.StartLine-1]...)
		out = append(out, strings.Split(replacement, "\n")...)
		out = append(out, lines[anchor.EndLine:]...)
		return strings.Join(out, "\n"), nil
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
		if end < start {
			return "", errors.New("anchor end character is before start character")
		}
		lines[anchor.StartLine-1] = line[:start] + replacement + line[end:]
		return strings.Join(lines, "\n"), nil
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
	replacementLines := strings.Split(replacement, "\n")
	if len(replacementLines) == 1 {
		replacementLines[0] = startLine[:start] + replacementLines[0] + endLine[end:]
	} else {
		replacementLines[0] = startLine[:start] + replacementLines[0]
		last := len(replacementLines) - 1
		replacementLines[last] = replacementLines[last] + endLine[end:]
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:anchor.StartLine-1]...)
	out = append(out, replacementLines...)
	out = append(out, lines[anchor.EndLine:]...)
	return strings.Join(out, "\n"), nil
}

func parseSummary(prefix string) string {
	for _, line := range strings.Split(prefix, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "summary:") {
			return strings.TrimSpace(strings.TrimPrefix(line, line[:len("Summary:")]))
		}
	}
	return ""
}

func validateAnchor(lines []string, anchor session.Anchor) error {
	if anchor.StartLine <= 0 || anchor.EndLine <= 0 {
		return errors.New("anchor lines must be positive")
	}
	if anchor.EndLine < anchor.StartLine {
		return errors.New("anchor end line must be after start line")
	}
	if anchor.StartLine > len(lines) || anchor.EndLine > len(lines) {
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

func utf16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func utf16OffsetToByteIndex(s string, offset int) (int, error) {
	if offset < 0 {
		return 0, errors.New("utf16 offset must be non-negative")
	}
	seen := 0
	for i, r := range s {
		if seen == offset {
			return i, nil
		}
		seen += len(utf16.Encode([]rune{r}))
		if seen > offset {
			return 0, errors.New("utf16 offset splits a surrogate pair")
		}
	}
	if seen == offset {
		return len(s), nil
	}
	return 0, errors.New("utf16 offset is outside string")
}
