package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestBuildClientStateHasVersionedNonNullContract(t *testing.T) {
	state := buildClientState(*session.New("plan-1", "# Plan"), false)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{`"schemaVersion":2`, `"revisions":[`, `"threads":[]`, `"sideAnswers":[]`, `"reviewerDecisions":[]`, `"promotedSideAnswers":[]`, `"phase":"active"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("client state missing %s: %s", expected, text)
		}
	}
	if !state.Capabilities.CanFinalize || !state.Capabilities.CanEditFeedback || state.Capabilities.CanApplyProposal {
		t.Fatalf("unexpected active capabilities %+v", state.Capabilities)
	}
}

func TestBuildClientStateProjectsPendingProposalAndCommentPlacement(t *testing.T) {
	s := session.New("plan-1", "head\nold one\n\nold two\ntail")
	first := s.AddThread(session.Anchor{StartLine: 2, EndLine: 3}, "first")
	second := s.AddThread(session.Anchor{StartLine: 3, EndLine: 4}, "second")
	s.CreateSectionProposal(session.SectionProposalInput{
		Kind: session.ProposalKindReview, Anchor: session.Anchor{StartLine: 1, EndLine: 5},
		ProposedPlan: "head\nnew one\n\nnew two\ntail", ProposedSection: "head\nnew one\n\nnew two\ntail",
		IncludedThreadIDs: []string{first.ID, second.ID},
	})
	state := buildClientState(*s, false)
	if state.Phase != "proposal_pending" || state.PendingProposal == nil || state.ActiveChange == nil {
		t.Fatalf("pending projection missing: %+v", state)
	}
	if state.PendingProposal.Instruction != "" || state.ActiveChange.Before.Text != s.Plan || state.ActiveChange.After.Text != s.PendingProposal.ProposedPlan {
		t.Fatalf("unexpected proposal projection: %+v", state.PendingProposal)
	}
	if len(state.ActiveChange.ThreadPlacements) != 2 {
		t.Fatalf("placements = %+v", state.ActiveChange.ThreadPlacements)
	}
	last := state.ActiveChange.Clusters[0].LastRow
	for _, placement := range state.ActiveChange.ThreadPlacements {
		if placement.RowIndex != last {
			t.Fatalf("overlapping comment was not placed after cluster: %+v", placement)
		}
	}
	if state.Capabilities.CanFinalize || state.Capabilities.CanEditFeedback || !state.Capabilities.CanApplyProposal {
		t.Fatalf("unexpected proposal capabilities %+v", state.Capabilities)
	}
}

func TestMigrateLoadedSessionIsIdempotent(t *testing.T) {
	s := session.New("plan-1", "alpha")
	s.SetDigest(session.Digest{Summary: "approved", ReviewerDecisions: []string{"change it"}})
	s.CreateSectionProposal(session.SectionProposalInput{
		Anchor: session.Anchor{StartLine: 1, EndLine: 1}, ProposedPlan: "beta", ProposedSection: "beta",
		Instruction: "Use the final-review feedback below as the authoritative instruction.",
	})
	status, changed, err := migrateLoadedSession(s, "rejected", true)
	if err != nil || status != "active" || !changed {
		t.Fatalf("first migration = %q %v %v", status, changed, err)
	}
	if s.PendingProposal.Kind != session.ProposalKindReview || s.PendingProposal.ReviewDigest == nil || s.PendingProposal.ReviewDigest.Summary != "approved" {
		t.Fatalf("legacy review proposal not migrated: %+v", s.PendingProposal)
	}
	status, changed, err = migrateLoadedSession(s, status, true)
	if err != nil || status != "active" || changed {
		t.Fatalf("second migration was not idempotent = %q %v %v", status, changed, err)
	}
}

func TestMigrateLoadedSessionRecognizesRefinedLegacyWholePlanProposal(t *testing.T) {
	s := session.New("plan-1", "alpha\nbeta")
	open := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "open")
	closed := s.AddThread(session.Anchor{StartLine: 2, EndLine: 2}, "closed")
	s.Threads[1].Status = session.ThreadStatusResolved
	openAnswer := s.AddSideAnswer(open.ID, "open?", "yes")
	closedAnswer := s.AddSideAnswer(closed.ID, "closed?", "no")
	s.PromoteSideAnswer(openAnswer.ID)
	s.PromoteSideAnswer(closedAnswer.ID)
	s.SetDigest(session.Digest{Summary: "iterate"})
	s.CreateSectionProposal(session.SectionProposalInput{
		Anchor: session.Anchor{StartLine: 1, EndLine: 2}, ProposedPlan: "alpha\ngamma", ProposedSection: "alpha\ngamma",
		Instruction: "Refine the second paragraph; preserve the rest.",
	})

	_, changed, err := migrateLoadedSession(s, "active", true)
	if err != nil || !changed {
		t.Fatalf("migration failed: changed=%v err=%v", changed, err)
	}
	proposal := s.PendingProposal
	if proposal == nil || proposal.Kind != session.ProposalKindReview || proposal.ReviewDigest == nil {
		t.Fatalf("refined whole-plan proposal was not recognized: %+v", proposal)
	}
	if len(proposal.ConsumedSideAnswerIDs) != 1 || proposal.ConsumedSideAnswerIDs[0] != openAnswer.ID {
		t.Fatalf("expected only promoted open-thread answer to be consumed, got %+v", proposal.ConsumedSideAnswerIDs)
	}
}

func TestMigrateLoadedSessionDoesNotPromoteOrdinaryWholePlanProposal(t *testing.T) {
	s := session.New("plan-1", "alpha\nbeta")
	s.CreateSectionProposal(session.SectionProposalInput{
		Anchor: session.Anchor{StartLine: 1, EndLine: 2}, ProposedPlan: "alpha\ngamma", ProposedSection: "alpha\ngamma",
		Instruction: "Rewrite the selected document.",
	})
	_, _, err := migrateLoadedSession(s, "active", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.PendingProposal == nil || s.PendingProposal.Kind != "" || s.PendingProposal.ReviewDigest != nil {
		t.Fatalf("ordinary whole-plan section proposal was misclassified: %+v", s.PendingProposal)
	}
}
