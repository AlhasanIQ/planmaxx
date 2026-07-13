package sectioniter

import (
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestParseResponseExtractsSummaryAndReplacement(t *testing.T) {
	raw := proposalV1("rev-2", "lines", "## Current Section", "Clarified rollout order.", "## Replacement Section\n\n- Updated step.")

	got, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Clarified rollout order." {
		t.Fatalf("expected summary, got %q", got.Summary)
	}
	if got.Hunks[0].Content != "## Replacement Section\n\n- Updated step." {
		t.Fatalf("unexpected replacement %q", got.Hunks[0].Content)
	}
	if got.RevisionID != "rev-2" || len(got.Hunks) != 1 || got.Hunks[0].Target != "lines" {
		t.Fatalf("unexpected response metadata %+v", got)
	}
}

func TestParseResponsePreservesInnerMarkdownFences(t *testing.T) {
	raw := proposalV1("rev-2", "lines", "## Old", "Added CLI example.", "## Usage\n\n```bash\nplanmaxx review PLAN.md\n```\n\n- Keep verifying.")

	got, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Usage\n\n```bash\nplanmaxx review PLAN.md\n```\n\n- Keep verifying."
	if got.Hunks[0].Content != want {
		t.Fatalf("unexpected replacement\nwant: %q\ngot:  %q", want, got.Hunks[0].Content)
	}
}

func TestParseResponseRejectsMissingProtocolRoot(t *testing.T) {
	_, err := ParseResponse("Summary: Clarified rollout order.\n\nNo replacement XML.")
	if err == nil {
		t.Fatal("expected missing XML error")
	}
	if !strings.Contains(err.Error(), "not valid XML") {
		t.Fatalf("expected XML error, got %q", err.Error())
	}
}

func TestParseResponseRejectsMissingExpectedSource(t *testing.T) {
	_, err := ParseResponse(`<planmaxx_proposal version="1" revision="rev-2"><summary>No-op.</summary><replacement target="selection"><content>x</content></replacement></planmaxx_proposal>`)
	if err == nil {
		t.Fatal("expected empty replacement error")
	}
	if !strings.Contains(err.Error(), "expected source") {
		t.Fatalf("expected missing source error, got %q", err.Error())
	}
}

func TestParseResponseRejectsTrailingContentAfterReplacement(t *testing.T) {
	_, err := ParseResponse(proposalV1("rev-2", "selection", "old", "No-op.", "- New") + "\nextra")
	if err == nil {
		t.Fatal("expected trailing content error")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected trailing content error, got %q", err.Error())
	}
}

func TestParseResponseRejectsAmbiguousOrInvalidScope(t *testing.T) {
	cases := []string{
		`<planmaxx_proposal version="1" revision="rev-2"><summary>ok</summary><replacement target="selection">x</replacement></planmaxx_proposal>`,
		`<planmaxx_proposal version="1" revision="rev-2"><summary>ok</summary><replacement target="nearby"><expected>x</expected><content>y</content></replacement></planmaxx_proposal>`,
		`<planmaxx_proposal version="1" revision="rev-2"><summary>ok</summary><replacement target="selection"><expected>x</expected></replacement></planmaxx_proposal>`,
	}
	for _, raw := range cases {
		if _, err := ParseResponse(raw); err == nil {
			t.Fatalf("expected invalid response to fail: %s", raw)
		}
	}
}

func TestParseResponseUsesEscapedMultiHunks(t *testing.T) {
	raw := `<planmaxx_proposal version="1" revision="rev-2"><summary>Rename it.</summary><replacement target="lines" start_hint="99" end_hint="99"><before># Plan</before><expected>- Old &amp; Name</expected><after>- Keep</after><content>- New &amp; Name</content></replacement><replacement target="lines" start_hint="4" end_hint="4"><before>- Keep</before><expected>- Again</expected><after>- End</after><content>- Updated</content></replacement></planmaxx_proposal>`
	got, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hunks) != 2 || got.Hunks[0].Expected != "- Old & Name" || got.Hunks[0].Content != "- New & Name" {
		t.Fatalf("unexpected hunks %+v", got.Hunks)
	}
}

func TestParseResponseAcceptsCDATA(t *testing.T) {
	got, err := ParseResponse(`<planmaxx_proposal version="1" revision="rev-2"><summary>x</summary><replacement target="lines" start_hint="1" end_hint="1"><before>a</before><expected>b</expected><after>c</after><content><![CDATA[x]]></content></replacement></planmaxx_proposal>`)
	if err != nil || got.Hunks[0].Content != "x" {
		t.Fatalf("expected compatible CDATA handling, got %+v, %v", got, err)
	}
}

func TestParseResponseAllowsCharacterHunksAndMalformedHints(t *testing.T) {
	raw := `<planmaxx_proposal version="1" revision="rev-2"><summary>x</summary><replacement target="selection" start_hint="mistaken" end_hint="also-mistaken"><before>Fr</before><expected>om</expected><after> Zero</after><content>om</content></replacement></planmaxx_proposal>`
	got, err := ParseResponse(raw)
	if err != nil || len(got.Hunks) != 1 || got.Hunks[0].Target != "selection" || got.Hunks[0].StartHint != 0 {
		t.Fatalf("expected resilient selection parse, got %+v, %v", got, err)
	}
}

func TestParseResponseRequiresContentElementButAllowsDeletion(t *testing.T) {
	missing := `<planmaxx_proposal version="1" revision="rev-2"><summary>x</summary><replacement target="selection"><expected>x</expected></replacement></planmaxx_proposal>`
	if _, err := ParseResponse(missing); err == nil {
		t.Fatal("expected missing content element to fail")
	}
	deletion := `<planmaxx_proposal version="1" revision="rev-2"><summary>x</summary><replacement target="selection"><expected>x</expected><content></content></replacement></planmaxx_proposal>`
	got, err := ParseResponse(deletion)
	if err != nil || got.Hunks[0].Content != "" {
		t.Fatalf("expected empty deletion content, got %+v, %v", got, err)
	}
}

func proposalV1(revision, target, expected, summary, replacement string) string {
	return `<planmaxx_proposal version="1" revision="` + revision + `"><summary>` + summary + `</summary><replacement target="` + target + `"><expected>` + expected + `</expected><content>` + replacement + `</content></replacement></planmaxx_proposal>`
}

func TestSectionForAnchorReturnsLineRange(t *testing.T) {
	plan := "# Plan\n\n## Phase 1\n\n- Old step\n- Keep"

	got, err := SectionForAnchor(plan, session.Anchor{StartLine: 3, EndLine: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got != "## Phase 1\n\n- Old step" {
		t.Fatalf("unexpected section %q", got)
	}
}
