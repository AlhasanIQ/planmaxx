package sectioniter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
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
	client := &fakePromptClient{answer: proposalV1("rev-1", "selection", "Old", "Updated", "- New")}
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
	client := &fakePromptClient{answer: "No XML here."}
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
	if !strings.Contains(err.Error(), "not valid XML") {
		t.Fatalf("expected XML protocol error, got %q", err.Error())
	}
}

func TestServiceBuildsProposalInputFromAgentResponse(t *testing.T) {
	client := &fakePromptClient{answer: proposalV1("rev-1", "lines", "- Old step", "Clarified rollout order.", "- New step")}
	service := NewService("thread-1", client)

	got, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		ThreadID:            "review-thread",
		Plan:                "# Plan\n\n- Old step\n- Keep",
		Anchor:              session.Anchor{StartLine: 3, EndLine: 3},
		SelectedSection:     "- Old step",
		ReviewerInstruction: "Clarify this",
		Protocol:            `<planmaxx_iteration version="1" revision="rev-1"><reviewer_instruction>Clarify this</reviewer_instruction><annotated_plan>- Old step</annotated_plan></planmaxx_iteration>`,
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
		"planmaxx_proposal",
		"- Old step",
		"Clarify this",
	} {
		if !strings.Contains(client.prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, client.prompt)
		}
	}
}

func TestServiceAppliesHTMLSourceReplacement(t *testing.T) {
	client := &fakePromptClient{answer: `<planmaxx_proposal version="1" revision="rev-1"><summary>Clarified heading.</summary><replacement target="lines"><expected>&lt;h1&gt;Old&lt;/h1&gt;</expected><content>&lt;h1&gt;New&lt;/h1&gt;</content></replacement></planmaxx_proposal>`}
	got, err := NewService("thread-1", client).Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "<h1>Old</h1>\n<p>Keep</p>",
		Anchor:              session.Anchor{StartLine: 1, EndLine: 1},
		ReviewerInstruction: "Clarify the heading",
		Format:              planformat.HTML,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposedPlan != "<h1>New</h1>\n<p>Keep</p>" {
		t.Fatalf("unexpected HTML proposal %q", got.ProposedPlan)
	}
	if !strings.Contains(client.prompt, "This request includes an HTML plan") ||
		!strings.Contains(client.prompt, "XML-escape HTML markup") {
		t.Fatalf("expected HTML source instructions\n%s", client.prompt)
	}
}

func TestServiceReturnsReplacementAnchorForCharacterRange(t *testing.T) {
	client := &fakePromptClient{answer: proposalV1("rev-1", "selection", "rough", "Polished wording.", "polished")}
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

func TestServiceAppliesExplicitFullLineScopeForCharacterSelection(t *testing.T) {
	client := &fakePromptClient{answer: proposalV1("rev-1", "lines", "- Product name: **From Zero to AI Engineer**", "Updated audience.", "- Product name: **From Zero to AI Engineer**\n- Primary learner: **Software engineers**")}
	service := NewService("thread-1", client)

	got, err := service.Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "# Plan\n\n- Product name: **From Zero to AI Engineer**",
		Anchor:              session.Anchor{StartLine: 3, StartChar: 20, EndLine: 3, EndChar: 24},
		SelectedSection:     "om Z",
		ReviewerInstruction: "Clarify the audience",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Plan\n\n- Product name: **From Zero to AI Engineer**\n- Primary learner: **Software engineers**"
	if got.ProposedPlan != want {
		t.Fatalf("expected complete lines to replace the original line cleanly\nwant: %q\ngot:  %q", want, got.ProposedPlan)
	}
	if got.ReplacementAnchor != (session.Anchor{StartLine: 3, EndLine: 4}) {
		t.Fatalf("expected full-line replacement anchor, got %+v", got.ReplacementAnchor)
	}
	if got.AppliedAnchor != (session.Anchor{StartLine: 3, EndLine: 3}) {
		t.Fatalf("expected applied full-line anchor, got %+v", got.AppliedAnchor)
	}
	if !strings.Contains(client.prompt, `target="lines"`) {
		t.Fatalf("expected explicit line scope protocol, got %q", client.prompt)
	}
}

func TestServiceRejectsMismatchedRevision(t *testing.T) {
	client := &fakePromptClient{answer: proposalV1("rev-other", "selection", "old", "Updated", "new")}
	_, err := NewService("thread-1", client).Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "old",
		Anchor:              session.Anchor{StartLine: 1, StartChar: 0, EndLine: 1, EndChar: 3},
		ReviewerInstruction: "Update",
	})
	if err == nil || !strings.Contains(err.Error(), "targets revision") {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
}

func TestServicePreservesOriginalAppliedScopeWhenRefiningPendingProposal(t *testing.T) {
	client := &fakePromptClient{answer: proposalV1("rev-1", "lines", "- Generated one\n- Generated two", "Refined", "- Final")}
	got, err := NewService("thread-1", client).Propose(context.Background(), Request{
		RevisionID:          "rev-1",
		Plan:                "# Plan\n\n- Generated one\n- Generated two\n- Keep",
		Anchor:              session.Anchor{StartLine: 3, StartChar: 2, EndLine: 3, EndChar: 6},
		ReplacementAnchor:   session.Anchor{StartLine: 3, EndLine: 4},
		RootAppliedAnchor:   session.Anchor{StartLine: 3, EndLine: 3},
		ReviewerInstruction: "Refine",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AppliedAnchor != (session.Anchor{StartLine: 3, EndLine: 3}) {
		t.Fatalf("expected original source scope retained, got %+v", got.AppliedAnchor)
	}
	if got.ReplacementAnchor != (session.Anchor{StartLine: 3, EndLine: 3}) {
		t.Fatalf("expected result anchor from refined replacement, got %+v", got.ReplacementAnchor)
	}
}

func TestServiceAppliesDistantHunksWithoutTrustingHints(t *testing.T) {
	raw := `<planmaxx_proposal version="1" revision="rev-1"><summary>Rename.</summary><replacement target="lines" start_hint="99" end_hint="99"><before>before</before><expected>old one</expected><after>keep</after><content>new one</content></replacement><replacement target="lines" start_hint="1" end_hint="1"><before>keep</before><expected>old two</expected><after>after</after><content>new two</content></replacement></planmaxx_proposal>`
	client := &fakePromptClient{answer: raw}
	got, err := NewService("thread-1", client).Propose(context.Background(), Request{RevisionID: "rev-1", Plan: "before\nold one\nkeep\nold two\nafter", Anchor: session.Anchor{StartLine: 2, EndLine: 2}, ReviewerInstruction: "Rename"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposedPlan != "before\nnew one\nkeep\nnew two\nafter" || len(got.AppliedHunks) != 2 {
		t.Fatalf("unexpected proposal %+v", got)
	}
}

func TestServiceAppliesCharacterHunkWithoutLineSplice(t *testing.T) {
	plan := "- Product name: **From Zero to AI Engineer**"
	raw := `<planmaxx_proposal version="1" revision="base-commit"><summary>Rename precisely.</summary><replacement target="selection" start_hint="wrong" end_hint="wrong"><before>**From </before><expected>Zero</expected><after> to AI</after><content>AI</content></replacement></planmaxx_proposal>`
	got, err := NewService("thread-1", &fakePromptClient{answer: raw}).Propose(context.Background(), Request{
		RevisionID: "base-commit", Plan: plan,
		Anchor:          session.Anchor{StartLine: 1, StartChar: 23, EndLine: 1, EndChar: 27},
		SelectedSection: "Zero", ReviewerInstruction: "Rename it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "- Product name: **From AI to AI Engineer**"; got.ProposedPlan != want {
		t.Fatalf("character hunk corrupted plan\nwant: %q\ngot:  %q", want, got.ProposedPlan)
	}
	if got.AppliedAnchor != (session.Anchor{StartLine: 1, StartChar: 23, EndLine: 1, EndChar: 27}) {
		t.Fatalf("unexpected exact applied anchor %+v", got.AppliedAnchor)
	}
}
