package sectioniter

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/patches"
)

func ParseResponse(raw string) (ParsedResponse, error) {
	if strings.Contains(raw, `version="1"`) {
		return ParsedResponse{}, errors.New("section iteration protocol v1 is unsupported; return protocol v2")
	}
	var wire struct {
		XMLName      xml.Name `xml:"planmaxx_proposal"`
		Version      string   `xml:"version,attr"`
		Revision     string   `xml:"revision,attr"`
		Summary      string   `xml:"summary"`
		Replacements []struct {
			Target   string  `xml:"target,attr"`
			Start    string  `xml:"start_hint,attr"`
			End      string  `xml:"end_hint,attr"`
			Before   string  `xml:"before"`
			Expected string  `xml:"expected"`
			After    string  `xml:"after"`
			Content  *string `xml:"content"`
		} `xml:"replacement"`
	}
	decoder := xml.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&wire); err != nil {
		return ParsedResponse{}, fmt.Errorf("section iteration response is not valid XML: %w", err)
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return ParsedResponse{}, fmt.Errorf("section iteration response has invalid trailing content: %w", err)
		}
		return ParsedResponse{}, fmt.Errorf("section iteration response has content outside the XML document: %v", token)
	}
	if wire.XMLName.Local != "planmaxx_proposal" || wire.Version != "2" || wire.Revision == "" || strings.TrimSpace(wire.Summary) == "" || len(wire.Replacements) == 0 {
		return ParsedResponse{}, errors.New("protocol v2 requires proposal, revision, summary, and hunks")
	}
	result := ParsedResponse{RevisionID: wire.Revision, Summary: strings.TrimSpace(wire.Summary)}
	for _, h := range wire.Replacements {
		if h.Target != "lines" && h.Target != "selection" {
			return ParsedResponse{}, errors.New("protocol v2 hunk target must be selection or lines")
		}
		if h.Content == nil {
			return ParsedResponse{}, errors.New("protocol v2 hunk requires a content element")
		}
		if h.Expected == "" {
			return ParsedResponse{}, errors.New("protocol v2 hunk requires expected source text")
		}
		// Hints make the proposal easier to inspect, but the base-content match is
		// the authority. Ignore malformed hints instead of turning a recoverable
		// model counting error into a rejected otherwise-exact patch.
		start, _ := strconv.Atoi(h.Start)
		end, _ := strconv.Atoi(h.End)
		result.Hunks = append(result.Hunks, patches.Hunk{Target: h.Target, Before: h.Before, Expected: h.Expected, After: h.After, Content: *h.Content, StartHint: start, EndHint: end})
	}
	return result, nil
}
