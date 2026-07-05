package sectioniter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

type fakePromptClient struct {
	answer string
	err    error
	prompt string
	called bool
}

func (f *fakePromptClient) AskPrompt(ctx context.Context, prompt string) (string, error) {
	f.called = true
	f.prompt = prompt
	return f.answer, f.err
}

func TestServiceReturnsUnavailableWithoutThreadID(t *testing.T) {
	client := &fakePromptClient{answer: "```markdown\n- New\n```"}
	service := NewService("", client)

	_, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "# Plan\n\n- Old",
		Anchor:              session.Anchor{StartLine: 3, EndLine: 3},
		SelectedSection:     "- Old",
		ReviewerInstruction: "Clarify this",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if client.called {
		t.Fatal("expected client not to be called")
	}
}

func TestServiceReturnsUnavailableWithoutClient(t *testing.T) {
	service := NewService("thread-1", nil)

	_, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "# Plan\n\n- Old",
		Anchor:              session.Anchor{StartLine: 3, EndLine: 3},
		SelectedSection:     "- Old",
		ReviewerInstruction: "Clarify this",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestServiceRejectsMalformedResponseWithoutProposal(t *testing.T) {
	client := &fakePromptClient{answer: "No fenced markdown here."}
	service := NewService("thread-1", client)

	_, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "# Plan\n\n- Old",
		Anchor:              session.Anchor{StartLine: 3, EndLine: 3},
		SelectedSection:     "- Old",
		ReviewerInstruction: "Clarify this",
	})
	if err == nil {
		t.Fatal("expected malformed response error")
	}
	if !strings.Contains(err.Error(), "markdown replacement") {
		t.Fatalf("expected markdown replacement error, got %q", err.Error())
	}
}

func TestServiceBuildsProposalInputFromAgentResponse(t *testing.T) {
	client := &fakePromptClient{answer: "Summary: Clarified rollout order.\n\n```markdown\n- New step\n```"}
	service := NewService("thread-1", client)

	got, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		ThreadID:            "review-thread",
		Plan:                "# Plan\n\n- Old step\n- Keep",
		FilePath:            "/repo/plan.md",
		Reference:           "/repo/plan.md:3",
		Anchor:              session.Anchor{StartLine: 3, EndLine: 3},
		SelectedSection:     "- Old step",
		PlanExcerpt:         "# Plan\n\n- Old step\n- Keep",
		ReviewerInstruction: "Clarify this",
		ReviewerDecisions:   []string{"Use concrete wording."},
		PromotedSideAnswers: []string{"Question:\nWhy?\nAnswer:\nBecause."},
		IncludedThreadIDs:   []string{"review-thread"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ThreadID != "review-thread" {
		t.Fatalf("expected source thread, got %q", got.ThreadID)
	}
	if got.ProposedSection != "- New step" {
		t.Fatalf("expected proposed section, got %q", got.ProposedSection)
	}
	if got.ProposedPlan != "# Plan\n\n- New step\n- Keep" {
		t.Fatalf("unexpected proposed plan %q", got.ProposedPlan)
	}
	if got.Summary != "Clarified rollout order." {
		t.Fatalf("expected summary, got %q", got.Summary)
	}
	if len(got.IncludedThreadIDs) != 1 || got.IncludedThreadIDs[0] != "review-thread" {
		t.Fatalf("expected included thread ids, got %+v", got.IncludedThreadIDs)
	}
	for _, want := range []string{
		"rev-1",
		"/repo/plan.md",
		"/repo/plan.md:3",
		"- Old step",
		"Use concrete wording.",
		"Question:",
		"Clarify this",
	} {
		if !strings.Contains(client.prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, client.prompt)
		}
	}
}

func TestServiceReturnsReplacementAnchorForCharacterRange(t *testing.T) {
	client := &fakePromptClient{answer: "Summary: Polished wording.\n\n```markdown\npolished\n```"}
	service := NewService("thread-1", client)

	got, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		ThreadID:            "review-thread",
		Plan:                "# Plan\n\n- Use rough wording",
		Anchor:              session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 11},
		SelectedSection:     "rough",
		ReviewerInstruction: "Polish this wording",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposedPlan != "# Plan\n\n- Use polished wording" {
		t.Fatalf("unexpected proposed plan %q", got.ProposedPlan)
	}
	want := session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 14}
	if got.ReplacementAnchor != want {
		t.Fatalf("expected replacement anchor %+v, got %+v", want, got.ReplacementAnchor)
	}
}
