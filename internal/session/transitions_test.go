package session

import "testing"

func TestValidateAcceptsNewSessionAndRejectsBrokenGraph(t *testing.T) {
	s := New("test", "alpha\nbeta")
	if err := s.Validate(); err != nil {
		t.Fatalf("new session invalid: %v", err)
	}
	s.Revisions = append(s.Revisions, Revision{ID: "rev-2", ParentID: "missing", Source: RevisionSourceTurn, Plan: s.Plan})
	if err := s.Validate(); !IsTransition(err, TransitionInvariant) {
		t.Fatalf("expected invariant error, got %v", err)
	}
}

func TestValidateRejectsDuplicateIDsAndOutOfBoundsAnchors(t *testing.T) {
	s := New("test", "alpha")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "first")
	s.Threads = append(s.Threads, thread)
	if err := s.Validate(); !IsTransition(err, TransitionInvariant) {
		t.Fatalf("expected duplicate id invariant, got %v", err)
	}
	s.Threads = s.Threads[:1]
	s.Threads[0].Anchor.EndLine = 2
	if err := s.Validate(); !IsTransition(err, TransitionInvariant) {
		t.Fatalf("expected anchor invariant, got %v", err)
	}
}

func TestValidatePreservesHistoricalOutOfBoundsAnchors(t *testing.T) {
	s := New("test", "short")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "historical")
	s.Threads[0].Status = ThreadStatusResolved
	s.Threads[0].Anchor = Anchor{StartLine: 99, StartChar: 50, EndLine: 99, EndChar: 60}
	if err := s.Validate(); err != nil {
		t.Fatalf("resolved historical anchor should remain loadable: %v (thread %s)", err, thread.ID)
	}
}

func TestRepairInvalidOpenAnchorsMarksOnlyBrokenThreadsStale(t *testing.T) {
	s := New("test", "one\ntwo")
	valid := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "valid")
	broken := s.AddThread(Anchor{StartLine: 9, EndLine: 9}, "broken")
	if !s.RepairInvalidOpenAnchors() {
		t.Fatal("expected a repair")
	}
	if s.Threads[0].ID != valid.ID || s.Threads[0].Status != ThreadStatusOpen {
		t.Fatalf("valid thread changed: %+v", s.Threads[0])
	}
	if s.Threads[1].ID != broken.ID || s.Threads[1].Status != ThreadStatusStale {
		t.Fatalf("broken thread was not preserved as stale: %+v", s.Threads[1])
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("repaired session remains invalid: %v", err)
	}
}

func TestCheckedProposalTransitionsDistinguishMissingAndStale(t *testing.T) {
	s := New("test", "alpha")
	if _, err := s.ApplyProposalChecked("proposal-1"); !IsTransition(err, TransitionMissing) {
		t.Fatalf("expected missing, got %v", err)
	}
	s.CreateSectionProposal(SectionProposalInput{Anchor: Anchor{StartLine: 1, EndLine: 1}, ProposedPlan: "beta", ProposedSection: "beta"})
	if _, err := s.ApplyProposalChecked("proposal-old"); !IsTransition(err, TransitionStale) {
		t.Fatalf("expected stale, got %v", err)
	}
}
