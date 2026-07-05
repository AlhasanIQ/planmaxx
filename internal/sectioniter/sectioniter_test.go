package sectioniter

import (
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestParseResponseExtractsSummaryAndReplacement(t *testing.T) {
	raw := "Summary: Clarified rollout order.\n\n```markdown\n## Replacement Section\n\n- Updated step.\n```"

	got, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Clarified rollout order." {
		t.Fatalf("expected summary, got %q", got.Summary)
	}
	if got.Replacement != "## Replacement Section\n\n- Updated step." {
		t.Fatalf("unexpected replacement %q", got.Replacement)
	}
}

func TestParseResponsePreservesInnerMarkdownFences(t *testing.T) {
	raw := "Summary: Added CLI example.\n\n````markdown\n## Usage\n\n```bash\nplanmaxx review PLAN.md\n```\n\n- Keep verifying.\n````"

	got, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Usage\n\n```bash\nplanmaxx review PLAN.md\n```\n\n- Keep verifying."
	if got.Replacement != want {
		t.Fatalf("unexpected replacement\nwant: %q\ngot:  %q", want, got.Replacement)
	}
}

func TestParseResponseRejectsMissingMarkdownFence(t *testing.T) {
	_, err := ParseResponse("Summary: Clarified rollout order.\n\nNo replacement fence.")
	if err == nil {
		t.Fatal("expected missing fence error")
	}
	if !strings.Contains(err.Error(), "markdown replacement") {
		t.Fatalf("expected markdown replacement error, got %q", err.Error())
	}
}

func TestParseResponseRejectsEmptyMarkdownReplacement(t *testing.T) {
	_, err := ParseResponse("Summary: No-op.\n\n```markdown\n\n```")
	if err == nil {
		t.Fatal("expected empty replacement error")
	}
	if !strings.Contains(err.Error(), "empty markdown replacement") {
		t.Fatalf("expected empty replacement error, got %q", err.Error())
	}
}

func TestParseResponseRejectsTrailingContentAfterReplacement(t *testing.T) {
	_, err := ParseResponse("Summary: No-op.\n\n```markdown\n- New\n```\nextra")
	if err == nil {
		t.Fatal("expected trailing content error")
	}
	if !strings.Contains(err.Error(), "after the markdown replacement fence") {
		t.Fatalf("expected trailing content error, got %q", err.Error())
	}
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

func TestReplaceSectionReplacesLineRange(t *testing.T) {
	plan := "# Plan\n\n## Phase 1\n\n- Old step\n- Keep"

	got, err := ReplaceSection(plan, session.Anchor{StartLine: 3, EndLine: 5}, "## Phase 1\n\n- New step")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Plan\n\n## Phase 1\n\n- New step\n- Keep"
	if got != want {
		t.Fatalf("unexpected plan\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReplaceSectionReplacesSingleLineCharacterRange(t *testing.T) {
	plan := "# Plan\n\n- Use rough wording"

	got, err := ReplaceSection(plan, session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 11}, "clear")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Plan\n\n- Use clear wording"
	if got != want {
		t.Fatalf("unexpected plan\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReplaceSectionRejectsOutOfRangeAnchor(t *testing.T) {
	_, err := ReplaceSection("# Plan", session.Anchor{StartLine: 2, EndLine: 2}, "Nope")
	if err == nil {
		t.Fatal("expected out of range error")
	}
	if !strings.Contains(err.Error(), "outside plan") {
		t.Fatalf("expected outside plan error, got %q", err.Error())
	}
}
