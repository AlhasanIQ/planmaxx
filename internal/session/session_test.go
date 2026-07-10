package session

import "testing"

func TestSessionStoresThreadedComments(t *testing.T) {
	s := New("plan-1", "# Plan")

	thread := s.AddThread(Anchor{StartLine: 2, EndLine: 4}, "Check this sequencing")
	s.AddReply(thread.ID, "Reviewer agrees after side answer")

	if len(s.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(s.Threads))
	}
	if len(s.Threads[0].Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(s.Threads[0].Messages))
	}
	if s.Threads[0].Anchor.StartLine != 2 {
		t.Fatalf("expected anchor start line 2, got %d", s.Threads[0].Anchor.StartLine)
	}
	if s.Threads[0].Messages[0].Author != "reviewer" {
		t.Fatalf("expected first message author reviewer, got %q", s.Threads[0].Messages[0].Author)
	}
	if s.Threads[0].Messages[1].Author != "reviewer" {
		t.Fatalf("expected reply author reviewer, got %q", s.Threads[0].Messages[1].Author)
	}
	if s.Threads[0].Messages[0].ID != "msg-1" {
		t.Fatalf("expected first message ID msg-1, got %q", s.Threads[0].Messages[0].ID)
	}
	if s.Threads[0].Messages[1].ID != "msg-2" {
		t.Fatalf("expected second message ID msg-2, got %q", s.Threads[0].Messages[1].ID)
	}
}

func TestSessionDoesNotReuseThreadOrSideAnswerIDs(t *testing.T) {
	s := New("plan-1", "# Plan")
	first := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "First")
	if !s.DeleteThread(first.ID) {
		t.Fatal("expected thread deletion")
	}
	second := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Second")
	if second.ID != "thread-2" {
		t.Fatalf("expected a new thread ID, got %q", second.ID)
	}
	firstAnswer := s.AddSideAnswer(second.ID, "Why?", "Because.")
	s.SideAnswers = nil // Simulate deletion of an answer before a restart.
	s.RestoreCounters()
	secondAnswer := s.AddSideAnswer(second.ID, "Why now?", "Because now.")
	if firstAnswer.ID == secondAnswer.ID || secondAnswer.ID != "side-2" {
		t.Fatalf("expected a new side-answer ID, got %q after %q", secondAnswer.ID, firstAnswer.ID)
	}
}

func TestReconcileExternalPlanMovesUniqueAnchorsAndStalesAmbiguousOnes(t *testing.T) {
	previous := "# Plan\n\n- Keep\n- Duplicate\n- Duplicate\n"
	next := "# Plan\n\n- Added\n- Keep\n- Duplicate\n- Duplicate\n"
	s := New("plan-1", previous)
	moved := s.AddThread(Anchor{StartLine: 3, EndLine: 3}, "Keep this")
	stale := s.AddThread(Anchor{StartLine: 4, EndLine: 4}, "Which duplicate?")

	s.ReconcileExternalPlan(previous, next)
	if got := s.Threads[0].Anchor; got != (Anchor{StartLine: 4, EndLine: 4}) {
		t.Fatalf("expected unique line to move, got %+v", got)
	}
	if got := s.Threads[1].Status; got != ThreadStatusStale {
		t.Fatalf("expected ambiguous thread %q to be stale, got %q", stale.ID, got)
	}
	if s.Threads[0].ID != moved.ID || s.Plan != next || len(s.Revisions) != 2 {
		t.Fatalf("expected review data and external revision to survive: %+v", s)
	}
}

func TestReconcileExternalPlanPreservesPendingProposalAsObsolete(t *testing.T) {
	previous := "# Plan\n\n- Original\n"
	next := "# Plan\n\n- Changed outside PlanMaxx\n"
	s := New("plan-1", previous)
	proposal := s.CreateSectionProposal(SectionProposalInput{
		Anchor:          Anchor{StartLine: 3, EndLine: 3},
		OriginalSection: "- Original",
		ProposedSection: "- Proposed",
		ProposedPlan:    "# Plan\n\n- Proposed\n",
	})

	s.ReconcileExternalPlan(previous, next)
	if s.PendingProposal == nil || s.PendingProposal.ID != proposal.ID || !s.PendingProposal.Obsolete {
		t.Fatalf("expected proposal to remain as obsolete, got %+v", s.PendingProposal)
	}
	if _, ok := s.ApplyProposal(proposal.ID); ok {
		t.Fatal("expected obsolete proposal apply to fail")
	}
	if !s.DiscardProposal(proposal.ID) {
		t.Fatal("expected obsolete proposal to be discardable")
	}
}

func TestRestoreCountersContinuesMessageIDs(t *testing.T) {
	s := New("plan-1", "# Plan")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "First")
	s.nextMessage = 0

	s.RestoreCounters()

	if !s.AddReply(thread.ID, "Second") {
		t.Fatal("expected reply to succeed")
	}
	if got := s.Threads[0].Messages[1].ID; got != "msg-2" {
		t.Fatalf("expected restored next message ID msg-2, got %q", got)
	}
}

func TestNewSessionCreatesInitialRevision(t *testing.T) {
	s := New("plan-1", "# Plan")

	if s.CurrentRevisionID != "rev-1" {
		t.Fatalf("expected current revision rev-1, got %q", s.CurrentRevisionID)
	}
	if len(s.Revisions) != 1 {
		t.Fatalf("expected one initial revision, got %+v", s.Revisions)
	}
	rev := s.Revisions[0]
	if rev.ID != "rev-1" || rev.ParentID != "" || rev.Source != RevisionSourceInitial {
		t.Fatalf("unexpected initial revision metadata %+v", rev)
	}
	if rev.Plan != "# Plan" {
		t.Fatalf("expected initial revision plan to match session plan, got %q", rev.Plan)
	}
}

func TestAddTurnRevisionUpdatesCurrentPlan(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")

	rev := s.AddTurnRevision("# Plan\n\n- New", "Codex revised plan")

	if rev.ID != "rev-2" {
		t.Fatalf("expected turn revision rev-2, got %q", rev.ID)
	}
	if rev.ParentID != "rev-1" {
		t.Fatalf("expected parent rev-1, got %q", rev.ParentID)
	}
	if rev.Source != RevisionSourceTurn {
		t.Fatalf("expected source %q, got %q", RevisionSourceTurn, rev.Source)
	}
	if s.CurrentRevisionID != rev.ID {
		t.Fatalf("expected current revision %q, got %q", rev.ID, s.CurrentRevisionID)
	}
	if s.Plan != "# Plan\n\n- New" {
		t.Fatalf("expected session plan to update, got %q", s.Plan)
	}
}

func TestCreateSectionProposalDoesNotMutatePlan(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")

	proposal := s.CreateSectionProposal(SectionProposalInput{
		ThreadID:          "thread-1",
		Anchor:            Anchor{StartLine: 3, EndLine: 3},
		OriginalSection:   "- Old",
		ProposedSection:   "- New",
		ProposedPlan:      "# Plan\n\n- New",
		Summary:           "Updated bullet",
		Instruction:       "Clarify this",
		RawResponse:       "Summary: Updated bullet\n\n```markdown\n- New\n```",
		IncludedThreadIDs: []string{"thread-1"},
	})

	if proposal.ID != "proposal-1" {
		t.Fatalf("expected proposal-1, got %q", proposal.ID)
	}
	if proposal.ParentID != "rev-1" {
		t.Fatalf("expected proposal parent rev-1, got %q", proposal.ParentID)
	}
	if proposal.ReplacementAnchor != proposal.Anchor {
		t.Fatalf("expected default replacement anchor %+v, got %+v", proposal.Anchor, proposal.ReplacementAnchor)
	}
	if s.Plan != "# Plan\n\n- Old" {
		t.Fatalf("expected pending proposal not to mutate plan, got %q", s.Plan)
	}
	if s.PendingProposal == nil || s.PendingProposal.ID != proposal.ID {
		t.Fatalf("expected pending proposal to be stored, got %+v", s.PendingProposal)
	}
}

func TestApplySectionProposalCreatesImmediateRevision(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")
	thread := s.AddThread(Anchor{StartLine: 3, EndLine: 3}, "Update this")
	proposal := s.CreateSectionProposal(SectionProposalInput{
		ThreadID:          thread.ID,
		Anchor:            thread.Anchor,
		OriginalSection:   "- Old",
		ProposedSection:   "- New",
		ProposedPlan:      "# Plan\n\n- New",
		Summary:           "Updated bullet",
		Instruction:       "Clarify this",
		RawResponse:       "Summary: Updated bullet\n\n```markdown\n- New\n```",
		IncludedThreadIDs: []string{thread.ID},
	})

	rev, ok := s.ApplyProposal(proposal.ID)
	if !ok {
		t.Fatal("expected proposal apply to succeed")
	}
	if rev.ID != "rev-2" || rev.ParentID != "rev-1" || rev.Source != RevisionSourceImmediate {
		t.Fatalf("unexpected applied revision %+v", rev)
	}
	if len(rev.Feedback) != 1 || rev.Feedback[0].ThreadID != thread.ID || rev.Feedback[0].RevisionID != rev.ID {
		t.Fatalf("expected immutable feedback snapshot on applied revision, got %+v", rev.Feedback)
	}
	if len(rev.Feedback[0].Messages) != 1 || rev.Feedback[0].Messages[0].Body != "Update this" {
		t.Fatalf("expected feedback message snapshot, got %+v", rev.Feedback[0])
	}
	if rev.Feedback[0].ResultAnchor.StartLine != 3 || rev.Feedback[0].ResultAnchor.EndLine != 3 {
		t.Fatalf("expected result anchor on feedback snapshot, got %+v", rev.Feedback[0].ResultAnchor)
	}
	if s.Plan != "# Plan\n\n- New" {
		t.Fatalf("expected session plan to update, got %q", s.Plan)
	}
	if s.PendingProposal != nil {
		t.Fatalf("expected pending proposal to clear, got %+v", s.PendingProposal)
	}
	if s.Threads[0].Status != ThreadStatusResolved {
		t.Fatalf("expected included thread to resolve, got %+v", s.Threads[0])
	}
}

func TestApplySectionProposalAdjustsThreadStatusesAndAnchors(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- A\n- B\n- C\n- D")
	before := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Before")
	included := s.AddThread(Anchor{StartLine: 3, EndLine: 3}, "Use this")
	stale := s.AddThread(Anchor{StartLine: 4, EndLine: 4}, "Check this")
	after := s.AddThread(Anchor{StartLine: 6, EndLine: 6}, "After")
	proposal := s.CreateSectionProposal(SectionProposalInput{
		Anchor:            Anchor{StartLine: 3, EndLine: 4},
		OriginalSection:   "- A\n- B",
		ProposedSection:   "- A\n- B\n- B2",
		ProposedPlan:      "# Plan\n\n- A\n- B\n- B2\n- C\n- D",
		Summary:           "Expanded changed section",
		Instruction:       "Clarify this",
		RawResponse:       "Summary: Expanded changed section\n\n```markdown\n- A\n- B\n- B2\n```",
		IncludedThreadIDs: []string{included.ID},
	})

	if _, ok := s.ApplyProposal(proposal.ID); !ok {
		t.Fatal("expected proposal apply to succeed")
	}

	assertThread := func(id string) Thread {
		t.Helper()
		for _, thread := range s.Threads {
			if thread.ID == id {
				return thread
			}
		}
		t.Fatalf("missing thread %q", id)
		return Thread{}
	}
	if got := assertThread(before.ID); got.Anchor.StartLine != 1 || got.Status != ThreadStatusOpen {
		t.Fatalf("expected before thread unchanged/open, got %+v", got)
	}
	if got := assertThread(included.ID); got.Status != ThreadStatusResolved {
		t.Fatalf("expected included thread resolved, got %+v", got)
	}
	if got := assertThread(stale.ID); got.Status != ThreadStatusStale {
		t.Fatalf("expected overlapping unincluded thread stale, got %+v", got)
	}
	if got := assertThread(after.ID); got.Anchor.StartLine != 7 || got.Anchor.EndLine != 7 || got.Status != ThreadStatusOpen {
		t.Fatalf("expected after thread shifted/open, got %+v", got)
	}
}

func TestApplyProposalUsesExplicitAppliedAnchorForThreadLifecycle(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- One\n- Two\n- Three")
	included := s.AddThread(Anchor{StartLine: 3, StartChar: 2, EndLine: 3, EndChar: 5}, "Expand this")
	s.AddThread(Anchor{StartLine: 4, EndLine: 4}, "Still review this")
	s.AddThread(Anchor{StartLine: 5, EndLine: 5}, "After")
	proposal := s.CreateSectionProposal(SectionProposalInput{
		ThreadID:          included.ID,
		Anchor:            included.Anchor,
		AppliedAnchor:     Anchor{StartLine: 3, EndLine: 4},
		ReplacementAnchor: Anchor{StartLine: 3, EndLine: 5},
		OriginalSection:   "- One\n- Two",
		ProposedSection:   "- One refined\n- Two refined\n- Extra",
		ProposedPlan:      "# Plan\n\n- One refined\n- Two refined\n- Extra\n- Three",
		Summary:           "Expanded scope",
		Instruction:       "Expand this",
		IncludedThreadIDs: []string{included.ID},
	})
	if _, ok := s.ApplyProposal(proposal.ID); !ok {
		t.Fatal("expected proposal to apply")
	}
	if got := s.Threads[0].Status; got != ThreadStatusResolved {
		t.Fatalf("expected included thread resolved, got %q", got)
	}
	if got := s.Threads[1].Status; got != ThreadStatusStale {
		t.Fatalf("expected thread in declared full-line scope stale, got %q", got)
	}
	if got := s.Threads[2].Anchor; got != (Anchor{StartLine: 6, EndLine: 6}) {
		t.Fatalf("expected post-scope anchor to shift by line delta, got %+v", got)
	}
}

func TestApplyProposalAdjustsThreadsForMultipleHunks(t *testing.T) {
	s := New("plan-1", "before\nold one\nkeep\nold two\nafter")
	included := s.AddThread(Anchor{StartLine: 2, EndLine: 2}, "First")
	affected := s.AddThread(Anchor{StartLine: 4, EndLine: 4}, "Second")
	after := s.AddThread(Anchor{StartLine: 5, EndLine: 5}, "After")
	p := s.CreateSectionProposal(SectionProposalInput{Anchor: included.Anchor, AppliedAnchor: included.Anchor, AppliedHunks: []AppliedHunk{{Anchor: Anchor{StartLine: 2, EndLine: 2}, Result: Anchor{StartLine: 2, EndLine: 3}, LineDelta: 1}, {Anchor: Anchor{StartLine: 4, EndLine: 4}, Result: Anchor{StartLine: 5, EndLine: 5}, LineDelta: 0}}, ProposedPlan: "before\nnew one\nextra\nkeep\nnew two\nafter", ProposedSection: "new one", IncludedThreadIDs: []string{included.ID}})
	if _, ok := s.ApplyProposal(p.ID); !ok {
		t.Fatal("apply")
	}
	if s.Threads[0].Status != ThreadStatusResolved || s.Threads[1].Status != ThreadStatusStale {
		t.Fatalf("unexpected affected states %+v", s.Threads)
	}
	if got := s.Threads[2].Anchor; got != (Anchor{StartLine: 6, EndLine: 6}) || after.ID == "" || affected.ID == "" {
		t.Fatalf("after anchor %+v", got)
	}
}

func TestApplyProposalPreservesDisjointCharacterThreadOnSameLine(t *testing.T) {
	s := New("plan-1", "- Name: old | keep")
	selected := s.AddThread(Anchor{StartLine: 1, StartChar: 8, EndLine: 1, EndChar: 11}, "Rename old")
	disjoint := s.AddThread(Anchor{StartLine: 1, StartChar: 14, EndLine: 1, EndChar: 18}, "Keep wording")
	p := s.CreateSectionProposal(SectionProposalInput{
		ThreadID: selected.ID, Anchor: selected.Anchor, AppliedAnchor: selected.Anchor,
		AppliedHunks: []AppliedHunk{{Anchor: selected.Anchor, Result: Anchor{StartLine: 1, StartChar: 8, EndLine: 1, EndChar: 15}}},
		ProposedPlan: "- Name: renamed | keep", ProposedSection: "renamed", IncludedThreadIDs: []string{selected.ID},
	})
	if _, ok := s.ApplyProposal(p.ID); !ok {
		t.Fatal("apply")
	}
	if got := s.Threads[0].Status; got != ThreadStatusResolved {
		t.Fatalf("selected status %q", got)
	}
	if got := s.Threads[1]; got.Status != ThreadStatusOpen || got.Anchor != (Anchor{StartLine: 1, StartChar: 18, EndLine: 1, EndChar: 22}) || got.ID != disjoint.ID {
		t.Fatalf("expected disjoint thread to move, got %+v", got)
	}
}

func TestApplyProposalClearsResolvedCharacterSelection(t *testing.T) {
	s := New("plan-1", "- Name: old")
	thread := s.AddThreadWithSelectedText(Anchor{StartLine: 1, StartChar: 8, EndLine: 1, EndChar: 11}, "Rename old", "old")
	p := s.CreateSectionProposal(SectionProposalInput{
		ThreadID: thread.ID, Anchor: thread.Anchor, AppliedAnchor: thread.Anchor,
		AppliedHunks:      []AppliedHunk{{Anchor: thread.Anchor, Result: Anchor{StartLine: 1, StartChar: 8, EndLine: 1, EndChar: 15}}},
		ReplacementAnchor: Anchor{StartLine: 1, StartChar: 8, EndLine: 1, EndChar: 15},
		ProposedPlan:      "- Name: renamed", ProposedSection: "renamed", IncludedThreadIDs: []string{thread.ID},
	})
	if _, ok := s.ApplyProposal(p.ID); !ok {
		t.Fatal("apply")
	}
	got := s.Threads[0]
	if got.Status != ThreadStatusResolved {
		t.Fatalf("expected resolved thread, got %+v", got)
	}
	if got.SelectedText != "" || got.Anchor != (Anchor{StartLine: 1, EndLine: 1}) {
		t.Fatalf("expected old character selection to be cleared, got %+v", got)
	}
}

func TestReconcileExternalPlanReanchorsMultilineCharacterSelection(t *testing.T) {
	s := New("plan-1", "before\nalpha one\nbeta two\nafter")
	thread := s.AddThread(Anchor{StartLine: 2, StartChar: 6, EndLine: 3, EndChar: 4}, "Keep this together")
	next := "intro\nbefore\nalpha one\nbeta two\nafter"
	s.ReconcileExternalPlan(s.Plan, next)
	if got := s.Threads[0].Anchor; got != (Anchor{StartLine: 3, StartChar: 6, EndLine: 4, EndChar: 4}) {
		t.Fatalf("expected multiline character anchor to reanchor, got %+v", got)
	}
	if s.Threads[0].Status != ThreadStatusOpen || thread.ID == "" {
		t.Fatalf("expected retained open thread, got %+v", s.Threads[0])
	}
}

func TestApplyRefinedSectionProposalShiftsThreadsByPlanDelta(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- A\n- B\n- C\n- D\n- Later")
	after := s.AddThread(Anchor{StartLine: 7, EndLine: 7}, "After")
	anchor := Anchor{StartLine: 3, EndLine: 6}

	s.CreateSectionProposal(SectionProposalInput{
		Anchor:          anchor,
		OriginalSection: "- A\n- B\n- C\n- D",
		ProposedSection: "- A\n- B\n- C\n- D\n- E\n- F",
		ProposedPlan:    "# Plan\n\n- A\n- B\n- C\n- D\n- E\n- F\n- Later",
		Summary:         "Expanded section",
		Instruction:     "Expand this",
		RawResponse:     "Summary: Expanded section\n\n```markdown\n- A\n- B\n- C\n- D\n- E\n- F\n```",
	})
	refined := s.CreateSectionProposal(SectionProposalInput{
		Anchor:          anchor,
		OriginalSection: "- A\n- B\n- C\n- D\n- E\n- F",
		ProposedSection: "- A\n- B\n- C\n- D\n- E\n- F\n- G",
		ProposedPlan:    "# Plan\n\n- A\n- B\n- C\n- D\n- E\n- F\n- G\n- Later",
		Summary:         "Expanded section again",
		Instruction:     "Expand this again",
		RawResponse:     "Summary: Expanded section again\n\n```markdown\n- A\n- B\n- C\n- D\n- E\n- F\n- G\n```",
	})

	if _, ok := s.ApplyProposal(refined.ID); !ok {
		t.Fatal("expected refined proposal apply to succeed")
	}
	got := s.Threads[0]
	if got.ID != after.ID || got.Anchor.StartLine != 10 || got.Anchor.EndLine != 10 {
		t.Fatalf("expected downstream thread shifted by full plan delta to line 10, got %+v", got)
	}
}

func TestAddTurnRevisionClearsPendingSectionProposal(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")
	s.CreateSectionProposal(SectionProposalInput{
		Anchor:          Anchor{StartLine: 3, EndLine: 3},
		OriginalSection: "- Old",
		ProposedSection: "- Proposed",
		ProposedPlan:    "# Plan\n\n- Proposed",
		Summary:         "Proposed change",
		Instruction:     "Change this",
		RawResponse:     "Summary: Proposed change\n\n```markdown\n- Proposed\n```",
	})

	s.AddTurnRevision("# Plan\n\n- Codex updated", "Codex revised plan")

	if s.PendingProposal != nil {
		t.Fatalf("expected turn revision to clear pending proposal, got %+v", s.PendingProposal)
	}
}

func TestApplySectionProposalRejectsStaleParent(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")
	proposal := s.CreateSectionProposal(SectionProposalInput{
		Anchor:          Anchor{StartLine: 3, EndLine: 3},
		OriginalSection: "- Old",
		ProposedSection: "- Proposed",
		ProposedPlan:    "# Plan\n\n- Proposed",
		Summary:         "Proposed change",
		Instruction:     "Change this",
		RawResponse:     "Summary: Proposed change\n\n```markdown\n- Proposed\n```",
	})
	s.addRevision(s.CurrentRevisionID, RevisionSourceTurn, "# Plan\n\n- Codex updated", Anchor{}, "Codex revised plan")
	s.PendingProposal = &proposal

	if _, ok := s.ApplyProposal(proposal.ID); ok {
		t.Fatal("expected stale proposal apply to fail")
	}
	if s.Plan != "# Plan\n\n- Codex updated" {
		t.Fatalf("expected stale apply not to replace current plan, got %q", s.Plan)
	}
	if s.PendingProposal == nil || s.PendingProposal.ID != proposal.ID {
		t.Fatalf("expected stale proposal to remain available for discard, got %+v", s.PendingProposal)
	}
}

func TestDiscardSectionProposalLeavesPlanUnchanged(t *testing.T) {
	s := New("plan-1", "# Plan\n\n- Old")
	proposal := s.CreateSectionProposal(SectionProposalInput{
		Anchor:            Anchor{StartLine: 3, EndLine: 3},
		OriginalSection:   "- Old",
		ProposedSection:   "- New",
		ProposedPlan:      "# Plan\n\n- New",
		Summary:           "Updated bullet",
		Instruction:       "Clarify this",
		RawResponse:       "Summary: Updated bullet\n\n```markdown\n- New\n```",
		IncludedThreadIDs: nil,
	})

	if !s.DiscardProposal(proposal.ID) {
		t.Fatal("expected proposal discard to succeed")
	}
	if s.Plan != "# Plan\n\n- Old" {
		t.Fatalf("expected plan to remain unchanged, got %q", s.Plan)
	}
	if s.PendingProposal != nil {
		t.Fatalf("expected pending proposal to clear, got %+v", s.PendingProposal)
	}
	if s.DiscardProposal(proposal.ID) {
		t.Fatal("expected second discard to return false")
	}
}

func TestRestoreCountersInitializesRevisionStateForOlderAutosaves(t *testing.T) {
	s := Session{ID: "plan-1", Plan: "# Plan"}

	s.RestoreCounters()

	if s.CurrentRevisionID != "rev-1" {
		t.Fatalf("expected current revision rev-1, got %q", s.CurrentRevisionID)
	}
	if len(s.Revisions) != 1 {
		t.Fatalf("expected restored initial revision, got %+v", s.Revisions)
	}
	if s.Revisions[0].Source != RevisionSourceInitial || s.Revisions[0].Plan != s.Plan {
		t.Fatalf("unexpected restored revision %+v", s.Revisions[0])
	}
}

func TestAddThreadAssignsStableIDsAndDefaultPositions(t *testing.T) {
	s := New("plan-1", "# Plan")

	first := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "First")
	second := s.AddThread(Anchor{StartLine: 3, EndLine: 3}, "Second")

	if first.ID != "thread-1" {
		t.Fatalf("expected first thread ID thread-1, got %q", first.ID)
	}
	if second.ID != "thread-2" {
		t.Fatalf("expected second thread ID thread-2, got %q", second.ID)
	}
	if first.Position != (Position{X: 720, Y: 120}) {
		t.Fatalf("expected first default position 720,120, got %+v", first.Position)
	}
	if second.Position != (Position{X: 720, Y: 216}) {
		t.Fatalf("expected second default position 720,216, got %+v", second.Position)
	}
	if s.Threads[0].Position != first.Position {
		t.Fatalf("expected stored first position %+v, got %+v", first.Position, s.Threads[0].Position)
	}
	if first.Kind != ThreadKindDecision {
		t.Fatalf("expected first thread kind %q, got %q", ThreadKindDecision, first.Kind)
	}
	if first.Status != ThreadStatusOpen {
		t.Fatalf("expected first thread status %q, got %q", ThreadStatusOpen, first.Status)
	}
	if s.Threads[1].Kind != ThreadKindDecision {
		t.Fatalf("expected stored second thread kind %q, got %q", ThreadKindDecision, s.Threads[1].Kind)
	}
	if s.Threads[1].Status != ThreadStatusOpen {
		t.Fatalf("expected stored second thread status %q, got %q", ThreadStatusOpen, s.Threads[1].Status)
	}
}

func TestSessionSetsThreadKind(t *testing.T) {
	s := New("plan-1", "# Plan")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Keep private")

	if !s.SetThreadKind(thread.ID, ThreadKindNote) {
		t.Fatal("expected kind update to succeed")
	}
	if got := s.Threads[0].Kind; got != ThreadKindNote {
		t.Fatalf("expected thread kind %q, got %q", ThreadKindNote, got)
	}
	if s.SetThreadKind(thread.ID, "private") {
		t.Fatal("expected invalid kind update to return false")
	}
	if got := s.Threads[0].Kind; got != ThreadKindNote {
		t.Fatalf("expected invalid kind update not to mutate kind, got %q", got)
	}
	if s.SetThreadKind("thread-missing", ThreadKindDecision) {
		t.Fatal("expected unknown thread kind update to return false")
	}
}

func TestSessionSetsThreadStatus(t *testing.T) {
	s := New("plan-1", "# Plan")
	first := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Resolved")
	second := s.AddThread(Anchor{StartLine: 2, EndLine: 2}, "Stale")

	if !s.ResolveThread(first.ID) {
		t.Fatal("expected resolve to succeed")
	}
	if got := s.Threads[0].Status; got != ThreadStatusResolved {
		t.Fatalf("expected thread status %q, got %q", ThreadStatusResolved, got)
	}
	if !s.MarkThreadStale(second.ID) {
		t.Fatal("expected stale mark to succeed")
	}
	if got := s.Threads[1].Status; got != ThreadStatusStale {
		t.Fatalf("expected thread status %q, got %q", ThreadStatusStale, got)
	}
	if s.ResolveThread("thread-missing") {
		t.Fatal("expected unknown thread resolve to return false")
	}
	if s.MarkThreadStale("thread-missing") {
		t.Fatal("expected unknown thread stale mark to return false")
	}
}

func TestSessionMovesThread(t *testing.T) {
	s := New("plan-1", "# Plan")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Move me")

	if !s.MoveThread(thread.ID, Position{X: 12, Y: 34}) {
		t.Fatal("expected move to succeed")
	}
	if s.Threads[0].Position != (Position{X: 12, Y: 34}) {
		t.Fatalf("expected moved position 12,34, got %+v", s.Threads[0].Position)
	}
	if s.MoveThread("thread-missing", Position{X: 1, Y: 2}) {
		t.Fatal("expected unknown thread move to return false")
	}
}

func TestSessionReanchorsThread(t *testing.T) {
	s := New("plan-1", "# Plan")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Move anchor")

	if !s.ReanchorThread(thread.ID, Anchor{StartLine: 5, EndLine: 8}) {
		t.Fatal("expected reanchor to succeed")
	}
	if s.Threads[0].Anchor != (Anchor{StartLine: 5, EndLine: 8}) {
		t.Fatalf("expected anchor 5-8, got %+v", s.Threads[0].Anchor)
	}
	if s.ReanchorThread("thread-missing", Anchor{StartLine: 9, EndLine: 9}) {
		t.Fatal("expected unknown thread reanchor to return false")
	}
}

func TestSessionEditsThreadAnchorAndOriginalComment(t *testing.T) {
	s := New("plan-1", "# Plan")
	thread := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Original comment")
	s.AddReply(thread.ID, "Keep this reply")

	nextAnchor := Anchor{StartLine: 2, StartChar: 3, EndLine: 4, EndChar: 8}
	if !s.EditThread(thread.ID, nextAnchor, "Updated comment") {
		t.Fatal("expected edit to succeed")
	}
	if s.Threads[0].Anchor != nextAnchor {
		t.Fatalf("expected anchor %+v, got %+v", nextAnchor, s.Threads[0].Anchor)
	}
	if got := s.Threads[0].Messages[0].Body; got != "Updated comment" {
		t.Fatalf("expected original comment to change, got %q", got)
	}
	if got := s.Threads[0].Messages[1].Body; got != "Keep this reply" {
		t.Fatalf("expected reply to be preserved, got %q", got)
	}
	if s.EditThread("thread-missing", Anchor{StartLine: 1, EndLine: 1}, "Nope") {
		t.Fatal("expected unknown thread edit to return false")
	}
}

func TestSessionUnknownReplyReturnsFalse(t *testing.T) {
	s := New("plan-1", "# Plan")

	if s.AddReply("thread-missing", "Reply") {
		t.Fatal("expected unknown thread reply to return false")
	}
	if len(s.Threads) != 0 {
		t.Fatalf("expected no threads, got %d", len(s.Threads))
	}
}

func TestSessionDeletesThreadAndAttachedSideAnswers(t *testing.T) {
	s := New("plan-1", "# Plan")
	first := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Delete this")
	second := s.AddThread(Anchor{StartLine: 1, EndLine: 1}, "Keep this")
	s.AddSideAnswer(first.ID, "Why?", "Delete answer")
	keptAnswer := s.AddSideAnswer(second.ID, "Why?", "Keep answer")

	if !s.DeleteThread(first.ID) {
		t.Fatal("expected delete to succeed")
	}
	if len(s.Threads) != 1 || s.Threads[0].ID != second.ID {
		t.Fatalf("expected only second thread to remain, got %+v", s.Threads)
	}
	if len(s.SideAnswers) != 1 || s.SideAnswers[0].ID != keptAnswer.ID {
		t.Fatalf("expected only kept side answer to remain, got %+v", s.SideAnswers)
	}
}

func TestSessionDeleteUnknownThreadReturnsFalse(t *testing.T) {
	s := New("plan-1", "# Plan")

	if s.DeleteThread("thread-missing") {
		t.Fatal("expected unknown thread delete to return false")
	}
}

func TestSessionPromotesSideAnswer(t *testing.T) {
	s := New("plan-1", "# Plan")

	answer := s.AddSideAnswer("thread-1", "Why this order?", "Because CLI contract is first.")
	s.PromoteSideAnswer(answer.ID)

	if !s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to be promoted")
	}
}

func TestSessionUnpromotesSideAnswer(t *testing.T) {
	s := New("plan-1", "# Plan")

	answer := s.AddSideAnswer("thread-1", "Why this order?", "Because CLI contract is first.")
	s.PromoteSideAnswer(answer.ID)

	if !s.UnpromoteSideAnswer(answer.ID) {
		t.Fatal("expected unpromote to succeed")
	}
	if s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to be unpromoted")
	}
}

func TestSessionAddsSideAnswersWithStableIDs(t *testing.T) {
	s := New("plan-1", "# Plan")

	first := s.AddSideAnswer("thread-1", "Question 1?", "Answer 1")
	second := s.AddSideAnswer("thread-2", "Question 2?", "Answer 2")

	if first.ID != "side-1" {
		t.Fatalf("expected first side answer ID side-1, got %q", first.ID)
	}
	if second.ID != "side-2" {
		t.Fatalf("expected second side answer ID side-2, got %q", second.ID)
	}
	if s.SideAnswers[0].ID != first.ID || s.SideAnswers[1].ID != second.ID {
		t.Fatalf("expected stored side answer IDs %q and %q, got %q and %q", first.ID, second.ID, s.SideAnswers[0].ID, s.SideAnswers[1].ID)
	}
}

func TestSessionUnknownSideAnswerPromotionReturnsFalse(t *testing.T) {
	s := New("plan-1", "# Plan")

	if s.PromoteSideAnswer("side-missing") {
		t.Fatal("expected unknown side answer promotion to return false")
	}
}

func TestSessionUnknownSideAnswerUnpromotionReturnsFalse(t *testing.T) {
	s := New("plan-1", "# Plan")

	if s.UnpromoteSideAnswer("side-missing") {
		t.Fatal("expected unknown side answer unpromotion to return false")
	}
}

func TestSessionSetDigestStoresAllFields(t *testing.T) {
	s := New("plan-1", "# Plan")
	digest := Digest{
		Summary:             "Approved.",
		ReviewerDecisions:   []string{"Use Go."},
		PromotedSideAnswers: []string{"Because the CLI contract is first."},
	}

	s.SetDigest(digest)

	if s.Digest.Summary != digest.Summary {
		t.Fatalf("expected digest summary %q, got %q", digest.Summary, s.Digest.Summary)
	}
	if len(s.Digest.ReviewerDecisions) != 1 || s.Digest.ReviewerDecisions[0] != digest.ReviewerDecisions[0] {
		t.Fatalf("expected reviewer decisions %+v, got %+v", digest.ReviewerDecisions, s.Digest.ReviewerDecisions)
	}
	if len(s.Digest.PromotedSideAnswers) != 1 || s.Digest.PromotedSideAnswers[0] != digest.PromotedSideAnswers[0] {
		t.Fatalf("expected promoted side answers %+v, got %+v", digest.PromotedSideAnswers, s.Digest.PromotedSideAnswers)
	}
}
