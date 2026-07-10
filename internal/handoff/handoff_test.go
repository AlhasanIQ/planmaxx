package handoff

import (
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestFormatIncludesPromptPlanAndDigest(t *testing.T) {
	s := session.New("plan-1", "# Final Plan")
	s.SetDigest(session.Digest{
		Summary:           "Reviewer approved the CLI-first implementation.",
		ReviewerDecisions: []string{"Use Go with Cobra for CLI commands."},
	})

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Continue from this approved PlanMaxx review.",
		"```markdown",
		"# Final Plan",
		"```json",
		"Reviewer approved the CLI-first implementation.",
		"Use Go with Cobra for CLI commands.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected handoff to contain %q\n%s", want, out)
		}
	}
}

func TestFormatWithoutCommentsBlocksUnrequestedExtraPasses(t *testing.T) {
	s := session.New("plan-1", "# Final Plan")
	s.SetDigest(session.Digest{
		Summary: "Approved without comments.",
	})

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"No reviewer comments were submitted.",
		"Continue from the final plan as written",
		"do not make additional edits, cleanup passes, or \"one last pass\" changes unless the user explicitly asks.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected no-comments handoff to contain %q\n%s", want, out)
		}
	}
}

func TestFormatWithCommentsOmitsNoCommentsInstruction(t *testing.T) {
	s := session.New("plan-1", "# Final Plan")
	s.SetDigest(session.Digest{
		Summary:           "Approved with review comments.",
		ReviewerDecisions: []string{"Use the current interface."},
	})

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "No reviewer comments were submitted.") {
		t.Fatalf("expected commented handoff not to include no-comments instruction\n%s", out)
	}
}

func TestFormatRejectedIncludesReiterationInstructionPlanAndDigest(t *testing.T) {
	s := session.New("plan-1", "# Rejected Plan")
	s.SetDigest(session.Digest{
		Summary:           "Reviewer rejected the plan because the rollout order is wrong.",
		ReviewerDecisions: []string{"Do not implement before revising the migration step."},
	})

	out, err := FormatRejected(*s)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"PlanMaxx review rejected.",
		"The user rejected this plan with comments.",
		"Address the comments in the rejection digest, then reiterate the plan until the user is satisfied.",
		"Rejected plan:",
		"```markdown",
		"# Rejected Plan",
		"Rejected review digest:",
		"```json",
		"Reviewer rejected the plan because the rollout order is wrong.",
		"Do not implement before revising the migration step.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rejection handoff to contain %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "Continue from this approved PlanMaxx review.") {
		t.Fatalf("expected rejection handoff not to approve continuation\n%s", out)
	}
	if strings.Contains(out, "Do not continue implementation") {
		t.Fatalf("expected rejection handoff to avoid implementation-specific stop wording\n%s", out)
	}
}

func TestFormatAddsModelFacingReviewContext(t *testing.T) {
	s := session.New("plan-1", "# Final Plan")
	thread := s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, StartChar: 2, EndLine: 1, EndChar: 7}, "Use Go with Cobra for CLI commands.", "Final")
	s.PlanPath = "/repo/PLAN.md"
	promoted := s.AddSideAnswer(thread.ID, "Why Cobra?", "It keeps commands consistent.")
	s.PromoteSideAnswer(promoted.ID)
	s.SetDigest(session.Digest{
		Summary:             "Reviewer approved the CLI-first implementation.",
		ReviewerDecisions:   []string{"Use Go with Cobra for CLI commands."},
		PromotedSideAnswers: []string{"Promote answer about CLI contract."},
	})

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Model-facing review context",
		`<planmaxx_review version="1">`,
		`<review_target threads="thread-1">Final</review_target>`,
		`<thread id="thread-1" target="1:3-1:8">`,
		"Use Go with Cobra for CLI commands.",
		"It keeps commands consistent.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected review context to contain %q\n%s", want, out)
		}
	}
}

func TestFormatUsesLongerFenceWhenPlanContainsTripleBackticks(t *testing.T) {
	s := session.New("plan-1", "# Final Plan\n\n```go\nfmt.Println(\"hello\")\n```")

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	want := "````markdown\n# Final Plan\n\n```go\nfmt.Println(\"hello\")\n```\n````"
	if !strings.Contains(out, want) {
		t.Fatalf("expected markdown block to use a longer outer fence containing %q\n%s", want, out)
	}
}

func TestFormatUsesLongerFenceWhenDigestContainsTripleBackticks(t *testing.T) {
	s := session.New("plan-1", "# Final Plan")
	s.SetDigest(session.Digest{
		Summary:           "Reviewer approved the CLI-first implementation.",
		ReviewerDecisions: []string{"Use ```go\\nfmt.Println(1)\\n``` for examples."},
	})

	out, err := Format(*s)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "````json\n") {
		t.Fatalf("expected json block to open with four backticks\n%s", out)
	}
	if !strings.Contains(out, "\"Use ```go\\\\nfmt.Println(1)\\\\n``` for examples.\"") {
		t.Fatalf("expected digest content to retain triple backticks inside JSON\n%s", out)
	}
	if !strings.Contains(out, "\n````\n") {
		t.Fatalf("expected json block to close with four backticks\n%s", out)
	}
}
