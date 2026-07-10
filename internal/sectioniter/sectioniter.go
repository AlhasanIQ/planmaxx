package sectioniter

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/AlhasanIQ/planmaxx/internal/patches"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

type ParsedResponse struct {
	RevisionID  string
	Summary     string
	Replacement string
	Target      ReplacementTarget
	Hunks       []patches.Hunk
}

type ReplacementTarget struct {
	Kind      string
	StartLine int
	EndLine   int
}

func parseV1Response(raw string) (ParsedResponse, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	var result ParsedResponse
	seenRoot := false
	seenSummary := false
	seenReplacement := false
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !seenRoot || depth != 0 {
				return ParsedResponse{}, errors.New("section iteration response is incomplete XML")
			}
			break
		}
		if err != nil {
			return ParsedResponse{}, fmt.Errorf("section iteration response is not valid XML: %w", err)
		}
		switch value := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return ParsedResponse{}, errors.New("section iteration response has text outside its XML elements")
			}
		case xml.StartElement:
			if !seenRoot {
				if value.Name.Space != "" || value.Name.Local != "planmaxx_proposal" {
					return ParsedResponse{}, errors.New("section iteration response must use a planmaxx_proposal root element")
				}
				version, revision, err := requiredRootAttributes(value)
				if err != nil {
					return ParsedResponse{}, err
				}
				if version != "1" {
					return ParsedResponse{}, fmt.Errorf("unsupported section iteration protocol version %q", version)
				}
				result.RevisionID = revision
				seenRoot = true
				depth = 1
				continue
			}
			if depth != 1 {
				return ParsedResponse{}, errors.New("section iteration response contains nested XML elements")
			}
			if value.Name.Space != "" {
				return ParsedResponse{}, errors.New("section iteration response cannot use namespaced elements")
			}
			switch value.Name.Local {
			case "summary":
				if seenSummary {
					return ParsedResponse{}, errors.New("section iteration response has more than one summary")
				}
				if len(value.Attr) != 0 {
					return ParsedResponse{}, errors.New("summary cannot have attributes")
				}
				text, err := elementText(decoder, value.Name)
				if err != nil {
					return ParsedResponse{}, err
				}
				result.Summary = strings.TrimSpace(text)
				if result.Summary == "" {
					return ParsedResponse{}, errors.New("section iteration response has an empty summary")
				}
				seenSummary = true
			case "replacement":
				if seenReplacement {
					return ParsedResponse{}, errors.New("section iteration response has more than one replacement")
				}
				target, err := replacementTarget(value)
				if err != nil {
					return ParsedResponse{}, err
				}
				text, err := elementText(decoder, value.Name)
				if err != nil {
					return ParsedResponse{}, err
				}
				result.Replacement = strings.Trim(text, "\r\n")
				if strings.TrimSpace(result.Replacement) == "" {
					return ParsedResponse{}, errors.New("section iteration response has an empty replacement")
				}
				result.Target = target
				seenReplacement = true
			default:
				return ParsedResponse{}, fmt.Errorf("section iteration response contains unsupported element %q", value.Name.Local)
			}
		case xml.EndElement:
			if depth != 1 || value.Name.Local != "planmaxx_proposal" {
				return ParsedResponse{}, errors.New("section iteration response has an unexpected closing element")
			}
			depth = 0
		default:
			return ParsedResponse{}, errors.New("section iteration response contains unsupported XML content")
		}
	}
	if !seenSummary || !seenReplacement {
		return ParsedResponse{}, errors.New("section iteration response must contain one summary and one replacement")
	}
	return result, nil
}

func requiredRootAttributes(start xml.StartElement) (version string, revision string, err error) {
	for _, attr := range start.Attr {
		if attr.Name.Space != "" {
			return "", "", errors.New("proposal root cannot have namespaced attributes")
		}
		switch attr.Name.Local {
		case "version":
			version = attr.Value
		case "revision":
			revision = attr.Value
		default:
			return "", "", fmt.Errorf("proposal root has unsupported attribute %q", attr.Name.Local)
		}
	}
	if version == "" || revision == "" {
		return "", "", errors.New("proposal root requires version and revision attributes")
	}
	return version, revision, nil
}

func replacementTarget(start xml.StartElement) (ReplacementTarget, error) {
	var target ReplacementTarget
	var startRaw, endRaw string
	for _, attr := range start.Attr {
		if attr.Name.Space != "" {
			return ReplacementTarget{}, errors.New("replacement cannot have namespaced attributes")
		}
		switch attr.Name.Local {
		case "target":
			target.Kind = attr.Value
		case "start":
			startRaw = attr.Value
		case "end":
			endRaw = attr.Value
		default:
			return ReplacementTarget{}, fmt.Errorf("replacement has unsupported attribute %q", attr.Name.Local)
		}
	}
	switch target.Kind {
	case "selection":
		if startRaw != "" || endRaw != "" {
			return ReplacementTarget{}, errors.New("selection replacement cannot declare line numbers")
		}
	case "lines":
		if startRaw == "" || endRaw == "" {
			return ReplacementTarget{}, errors.New("line replacement requires start and end line numbers")
		}
		var err error
		if target.StartLine, err = strconv.Atoi(startRaw); err != nil || target.StartLine < 1 {
			return ReplacementTarget{}, errors.New("replacement start line must be a positive integer")
		}
		if target.EndLine, err = strconv.Atoi(endRaw); err != nil || target.EndLine < target.StartLine {
			return ReplacementTarget{}, errors.New("replacement end line must be at or after start line")
		}
	default:
		return ReplacementTarget{}, fmt.Errorf("unsupported replacement target %q", target.Kind)
	}
	return target, nil
}

func elementText(decoder *xml.Decoder, name xml.Name) (string, error) {
	var out bytes.Buffer
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", errors.New("section iteration response has an unfinished XML element")
		}
		switch value := token.(type) {
		case xml.CharData:
			out.Write([]byte(value))
		case xml.EndElement:
			if value.Name != name {
				return "", errors.New("section iteration response has unexpected nested XML")
			}
			return out.String(), nil
		default:
			return "", errors.New("section iteration response elements may contain text only")
		}
	}
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
