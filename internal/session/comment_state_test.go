package session

import (
	"testing"
	"time"
)

func TestCommentStateTransitionsKeepIntentAndLifecycleOrthogonal(t *testing.T) {
	s := New("plan", "one\ntwo\nthree")
	thread, err := s.AddThreadWithIntent(Anchor{StartLine: 2, EndLine: 2}, "change two", "two", ThreadIntentPrivate)
	if err != nil || thread.Intent() != ThreadIntentPrivate || thread.Lifecycle() != ThreadLifecycleActive {
		t.Fatalf("create private feedback = %+v, %v", thread, err)
	}
	answer := s.AddSideAnswer(thread.ID, "why?", "because")
	if err := s.IncludeSideAnswer(answer.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DetachThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if s.Threads[0].Lifecycle() != ThreadLifecycleDetached || s.SideAnswers[0].Promoted {
		t.Fatalf("detach did not normalize state: thread=%+v answer=%+v", s.Threads[0], s.SideAnswers[0])
	}
	if err := s.ReanchorThreadChecked(thread.ID, Anchor{StartLine: 3, EndLine: 3}); err != nil {
		t.Fatal(err)
	}
	if s.Threads[0].Lifecycle() != ThreadLifecycleActive || s.Threads[0].Intent() != ThreadIntentPrivate {
		t.Fatalf("reactivation lost intent: %+v", s.Threads[0])
	}
}

func TestAddressedFeedbackIsReadOnlyAndExternalReconcilePreservesIt(t *testing.T) {
	s := New("plan", "one\ntwo")
	thread := s.AddThreadWithSelectedText(Anchor{StartLine: 2, EndLine: 2}, "change", "two")
	if err := s.AddressThread(thread.ID, Anchor{StartLine: 2, EndLine: 2}); err != nil {
		t.Fatal(err)
	}
	before := s.Threads[0]
	if err := s.EditThreadChecked(thread.ID, Anchor{StartLine: 1, EndLine: 1}, "rewrite", "one"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("addressed edit error = %v", err)
	}
	s.ReconcileExternalPlan("one\ntwo", "new\ncontent")
	got := s.Threads[0]
	if got.Status != before.Status || got.Anchor != before.Anchor || got.Messages[0].Body != before.Messages[0].Body {
		t.Fatalf("external reconcile mutated addressed feedback: before=%+v after=%+v", before, got)
	}
}

func TestDetachedFeedbackCanBeRecordedAsAddressedInARevision(t *testing.T) {
	s := New("plan", "one\ntwo")
	thread := s.AddThreadWithSelectedText(Anchor{StartLine: 2, EndLine: 2}, "change two", "two")
	answer := s.AddSideAnswer(thread.ID, "why?", "because")
	_ = s.IncludeSideAnswer(answer.ID)
	s.ReconcileExternalPlan("one\ntwo", "one\nchanged")
	if s.Threads[0].Lifecycle() != ThreadLifecycleDetached {
		t.Fatalf("expected detached feedback, got %+v", s.Threads[0])
	}
	if err := s.RecordDetachedThreadAddressed(thread.ID, "rev-2"); err != nil {
		t.Fatal(err)
	}
	if s.Threads[0].Lifecycle() != ThreadLifecycleAddressed || s.SideAnswers[0].Promoted {
		t.Fatalf("addressing did not normalize state: thread=%+v answer=%+v", s.Threads[0], s.SideAnswers[0])
	}
	feedback := s.Revisions[1].Feedback
	if len(feedback) != 1 || feedback[0].ThreadID != thread.ID || feedback[0].SelectedText != "two" || feedback[0].RevisionID != "rev-2" {
		t.Fatalf("revision feedback = %+v", feedback)
	}
	if feedback[0].Messages[0].Body != "change two" {
		t.Fatalf("revision feedback lost messages: %+v", feedback[0])
	}
	if err := s.EditThreadChecked(thread.ID, Anchor{StartLine: 1, EndLine: 1}, "again", "one"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("recorded feedback should be read-only, got %v", err)
	}
}

func TestRecordDetachedFeedbackRejectsInvalidTransitions(t *testing.T) {
	s := New("plan", "one")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "change")
	if err := s.RecordDetachedThreadAddressed(thread.ID, "rev-1"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("active feedback error = %v", err)
	}
	_ = s.DetachThread(thread.ID)
	if err := s.RecordDetachedThreadAddressed(thread.ID, "missing"); !IsTransition(err, TransitionMissing) {
		t.Fatalf("missing revision error = %v", err)
	}
	if err := s.RecordDetachedThreadAddressed(thread.ID, "rev-1"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("initial revision error = %v", err)
	}
	s.AddExternalRevision("changed", "older change")
	s.Revisions[1].CreatedAt = thread.Messages[0].CreatedAt.Add(-time.Minute)
	if err := s.RecordDetachedThreadAddressed(thread.ID, "rev-2"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("older revision error = %v", err)
	}
}

func TestSelectContextIsCanonicalAcrossEveryStateAxis(t *testing.T) {
	s := New("plan", "one\ntwo\nthree\nfour")
	instruction := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "instruction")
	private, _ := s.AddThreadWithIntent(Anchor{StartLine: 2, EndLine: 2}, "private", "", ThreadIntentPrivate)
	addressed := s.AddThread(Anchor{StartLine: 3, EndLine: 3}, "addressed")
	detached := s.AddThread(Anchor{StartLine: 4, EndLine: 4}, "detached")
	_ = s.AddressThread(addressed.ID, addressed.Anchor)
	_ = s.DetachThread(detached.ID)
	included := s.AddSideAnswer(private.ID, "include?", "yes")
	_ = s.IncludeSideAnswer(included.ID)
	addressedAnswer := s.AddSideAnswer(addressed.ID, "leak?", "no")
	s.SideAnswers[len(s.SideAnswers)-1].Promoted = true // invalid legacy state must still be excluded by projection.

	selection := SelectContext(*s, ContextOptions{})
	if got := selection.ThreadIDs; len(got) != 1 || got[0] != instruction.ID {
		t.Fatalf("instruction IDs = %+v", got)
	}
	if got := selection.AnswerIDs; len(got) != 1 || got[0] != included.ID {
		t.Fatalf("answer IDs = %+v (addressed=%s)", got, addressedAnswer.ID)
	}
	if len(selection.ContextThreads) != 2 {
		t.Fatalf("context threads = %+v", selection.ContextThreads)
	}

	explicit := SelectContext(*s, ContextOptions{ExplicitThreadID: private.ID})
	if len(explicit.ThreadIDs) != 2 || explicit.ThreadIDs[1] != private.ID {
		t.Fatalf("explicit private feedback not selected: %+v", explicit.ThreadIDs)
	}
}

func TestAddSideAnswerCheckedRequiresActiveParent(t *testing.T) {
	s := New("test", "one\ntwo")
	thread := s.AddThread(Anchor{StartLine: 2, EndLine: 2}, "change this")
	if err := s.DetachThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSideAnswerChecked(thread.ID, "why?", "because"); !IsTransition(err, TransitionBlocked) {
		t.Fatalf("expected blocked transition, got %v", err)
	}
	if len(s.SideAnswers) != 0 {
		t.Fatalf("blocked side answer mutated session: %+v", s.SideAnswers)
	}
}

func TestSelectContextAlwaysIncludesExplicitActiveThread(t *testing.T) {
	s := New("test", "one\ntwo\nthree")
	explicit, err := s.AddThreadWithIntent(Anchor{StartLine: 3, EndLine: 3}, "change three", "three", ThreadIntentPrivate)
	if err != nil {
		t.Fatal(err)
	}
	answer := s.AddSideAnswer(explicit.ID, "why?", "because")
	if err := s.IncludeSideAnswer(answer.ID); err != nil {
		t.Fatal(err)
	}
	selection := SelectContext(*s, ContextOptions{Anchor: &Anchor{StartLine: 1, EndLine: 1}, ExplicitThreadID: explicit.ID})
	if len(selection.ThreadIDs) != 1 || selection.ThreadIDs[0] != explicit.ID {
		t.Fatalf("explicit thread was silently dropped: %+v", selection.ThreadIDs)
	}
	if len(selection.AnswerIDs) != 1 || selection.AnswerIDs[0] != answer.ID {
		t.Fatalf("included context on explicit thread was silently dropped: %+v", selection.AnswerIDs)
	}
}

func TestSectionProposalConsumesRecordedIncludedAnswer(t *testing.T) {
	s := New("plan", "one\ntwo")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "change")
	answer := s.AddSideAnswer(thread.ID, "why?", "because")
	_ = s.IncludeSideAnswer(answer.ID)
	proposal := s.CreateSectionProposal(SectionProposalInput{
		Anchor: Anchor{StartLine: 1, EndLine: 1}, AppliedAnchor: Anchor{StartLine: 1, EndLine: 1},
		ReplacementAnchor: Anchor{StartLine: 1, EndLine: 1}, ProposedPlan: "new\ntwo", ProposedSection: "new",
		IncludedThreadIDs: []string{thread.ID}, ConsumedSideAnswerIDs: []string{answer.ID},
	})
	if _, err := s.ApplyProposalChecked(proposal.ID); err != nil {
		t.Fatal(err)
	}
	if s.SideAnswers[0].Promoted || s.Threads[0].Lifecycle() != ThreadLifecycleAddressed {
		t.Fatalf("apply did not consume exact context: %+v %+v", s.Threads[0], s.SideAnswers[0])
	}
}
