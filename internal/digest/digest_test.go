package digest

import (
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestBuildPromptDistinguishesReviewerAndPlan(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Agent task")
	s.PlanPath = "/repo/plan.md"
	s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, StartChar: 2, EndLine: 1, EndChar: 6}, "Use Cobra for CLI.", "Plan")
	unpromoted := s.AddSideAnswer("thread-1", "Why?", "Unpromoted answer should stay out.")
	promoted := s.AddSideAnswer("thread-1", "Why Cobra?", "Cobra gives clean subcommands.")
	s.PromoteSideAnswer(promoted.ID)

	prompt := BuildPrompt(*s)
	for _, want := range []string{
		"Reviewer decisions",
		"Agent-generated plan",
		"Use Cobra for CLI.",
		"# Plan",
		"Question:\nWhy Cobra?",
		"Cobra gives clean subcommands.",
		"/repo/plan.md:1:3-1:7",
		"Selected text:\nPlan",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, unpromoted.Answer) {
		t.Fatalf("expected prompt to omit unpromoted answer %q\n%s", unpromoted.Answer, prompt)
	}
}

func TestDraftFromStateUsesPromotedAnswersOnly(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	s.PlanPath = "/repo/plan.md"
	s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, StartChar: 2, EndLine: 1, EndChar: 6}, "Use Cobra for CLI.", "Plan")
	unpromoted := s.AddSideAnswer("thread-1", "Why?", "Unpromoted answer should stay out.")
	promoted := s.AddSideAnswer("thread-1", "Why Cobra?", "Cobra gives clean subcommands.")
	s.PromoteSideAnswer(promoted.ID)

	got := DraftFromState(*s)
	if got.Summary == "" {
		t.Fatal("expected summary")
	}
	if len(got.ReviewerDecisions) != 1 || got.ReviewerDecisions[0] != "Use Cobra for CLI." {
		t.Fatalf("expected reviewer decision from thread message, got %+v", got.ReviewerDecisions)
	}
	if len(got.PromotedSideAnswers) != 1 {
		t.Fatalf("expected one promoted answer, got %+v", got.PromotedSideAnswers)
	}
	for _, want := range []string{
		"/repo/plan.md:1:3-1:7",
		"Selected text:\nPlan",
		"Question:\nWhy Cobra?",
		"Answer:\n" + promoted.Answer,
	} {
		if !strings.Contains(got.PromotedSideAnswers[0], want) {
			t.Fatalf("expected promoted context to contain %q, got %+v", want, got.PromotedSideAnswers)
		}
	}
	for _, answer := range got.PromotedSideAnswers {
		if answer == unpromoted.Answer {
			t.Fatalf("expected unpromoted answer to be omitted, got %+v", got.PromotedSideAnswers)
		}
	}
}

func TestDraftFromStateUsesOnlyOpenDecisionThreads(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	decision := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Send this decision.")
	note := s.AddThread(session.Anchor{StartLine: 2, EndLine: 2}, "Keep this private.")
	resolved := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Already addressed.")
	stale := s.AddThread(session.Anchor{StartLine: 4, EndLine: 4}, "Needs re-anchor.")

	if !s.SetThreadKind(note.ID, session.ThreadKindNote) {
		t.Fatal("expected note kind update to succeed")
	}
	if !s.ResolveThread(resolved.ID) {
		t.Fatal("expected resolved status update to succeed")
	}
	if !s.MarkThreadStale(stale.ID) {
		t.Fatal("expected stale status update to succeed")
	}

	got := DraftFromState(*s)
	if len(got.ReviewerDecisions) != 1 {
		t.Fatalf("expected one open decision, got %+v", got.ReviewerDecisions)
	}
	if got.ReviewerDecisions[0] != decision.Messages[0].Body {
		t.Fatalf("expected reviewer decision %q, got %+v", decision.Messages[0].Body, got.ReviewerDecisions)
	}
}
