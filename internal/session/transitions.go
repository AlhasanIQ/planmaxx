package session

import (
	"errors"
	"fmt"
)

type TransitionKind string

const (
	TransitionMissing   TransitionKind = "missing"
	TransitionStale     TransitionKind = "stale"
	TransitionBlocked   TransitionKind = "blocked"
	TransitionInvariant TransitionKind = "invariant"
)

type TransitionError struct {
	Kind    TransitionKind
	Message string
}

func (e *TransitionError) Error() string { return e.Message }

func IsTransition(err error, kind TransitionKind) bool {
	var transition *TransitionError
	return errors.As(err, &transition) && transition.Kind == kind
}

func (s *Session) RestoreRevision(revisionID string, content string) (Revision, error) {
	if s.PendingProposal != nil {
		return Revision{}, &TransitionError{Kind: TransitionBlocked, Message: "apply or discard the pending proposal before restoring a revision"}
	}
	found := false
	for _, revision := range s.Revisions {
		if revision.ID == revisionID {
			found = true
			break
		}
	}
	if !found {
		return Revision{}, &TransitionError{Kind: TransitionMissing, Message: "revision not found"}
	}
	before := cloneForTransition(*s)
	revision := s.AddTurnRevision(content, "Restored revision "+revisionID)
	if err := s.Validate(); err != nil {
		*s = before
		return Revision{}, err
	}
	return revision, nil
}

func (s *Session) Validate() error {
	if s.ID == "" {
		return invariant("session id is empty")
	}
	if len(s.Revisions) == 0 {
		return invariant("session has no revisions")
	}
	revisions := make(map[string]Revision, len(s.Revisions))
	allowedSources := map[string]bool{
		RevisionSourceInitial: true, RevisionSourceTurn: true, RevisionSourceImmediate: true,
		RevisionSourceIteration: true, RevisionSourceExternal: true,
	}
	for index, revision := range s.Revisions {
		if revision.ID == "" {
			return invariant("revision id is empty")
		}
		if _, exists := revisions[revision.ID]; exists {
			return invariant("duplicate revision id " + revision.ID)
		}
		if !allowedSources[revision.Source] {
			return invariant("invalid revision source for " + revision.ID)
		}
		if index == 0 {
			if revision.ParentID != "" || revision.Source != RevisionSourceInitial {
				return invariant("first revision must be an initial root")
			}
		} else if _, exists := revisions[revision.ParentID]; !exists {
			return invariant("revision parent is missing for " + revision.ID)
		}
		revisions[revision.ID] = revision
	}
	current, exists := revisions[s.CurrentRevisionID]
	if !exists {
		return invariant("checked-out revision is missing")
	}
	if current.Plan != "" && current.Plan != s.Plan {
		return invariant("working plan does not match checked-out revision")
	}
	threadIDs := make(map[string]bool, len(s.Threads))
	messageIDs := map[string]bool{}
	for _, thread := range s.Threads {
		if thread.ID == "" || threadIDs[thread.ID] {
			return invariant("duplicate or empty thread id")
		}
		threadIDs[thread.ID] = true
		if thread.Kind != ThreadKindDecision && thread.Kind != ThreadKindNote {
			return invariant("invalid thread kind for " + thread.ID)
		}
		if thread.Status != ThreadStatusOpen && thread.Status != ThreadStatusResolved && thread.Status != ThreadStatusStale {
			return invariant("invalid thread status for " + thread.ID)
		}
		// Resolved and stale anchors are historical coordinates. They may refer
		// to text or lines that no longer exist in the checked-out revision and
		// must remain loadable for accepted-feedback history.
		if thread.Status == ThreadStatusOpen {
			if err := validateSessionAnchor(thread.Anchor, s.Plan); err != nil {
				return invariant(fmt.Sprintf("invalid anchor for %s: %v", thread.ID, err))
			}
		}
		for _, message := range thread.Messages {
			if message.ID == "" || messageIDs[message.ID] {
				return invariant("duplicate or empty message id")
			}
			messageIDs[message.ID] = true
		}
	}
	sideIDs := map[string]bool{}
	for _, answer := range s.SideAnswers {
		if answer.ID == "" || sideIDs[answer.ID] {
			return invariant("duplicate or empty side-answer id")
		}
		if !threadIDs[answer.ThreadID] {
			return invariant("side answer references missing thread")
		}
		sideIDs[answer.ID] = true
	}
	if proposal := s.PendingProposal; proposal != nil {
		if proposal.ID == "" {
			return invariant("pending proposal id is empty")
		}
		if !proposal.Obsolete && proposal.ParentID != s.CurrentRevisionID {
			return invariant("pending proposal parent is not checked out")
		}
		if proposal.Kind != "" && proposal.Kind != ProposalKindReview {
			return invariant("invalid pending proposal kind")
		}
		for _, threadID := range proposal.IncludedThreadIDs {
			if !threadIDs[threadID] {
				return invariant("pending proposal references missing thread")
			}
		}
		for _, answerID := range proposal.ConsumedSideAnswerIDs {
			if !sideIDs[answerID] {
				return invariant("pending proposal references missing side answer")
			}
		}
	}
	return nil
}

// RepairInvalidOpenAnchors preserves legacy reviews whose plan changed before
// anchor validation was introduced. Invalid open anchors cannot be edited or
// placed safely, so they become stale history instead of making the complete
// autosave unloadable.
func (s *Session) RepairInvalidOpenAnchors() bool {
	changed := false
	for index := range s.Threads {
		thread := &s.Threads[index]
		if thread.Status == ThreadStatusOpen && validateSessionAnchor(thread.Anchor, s.Plan) != nil {
			thread.Status = ThreadStatusStale
			changed = true
		}
	}
	return changed
}

func validateSessionAnchor(anchor Anchor, plan string) error {
	if anchor.StartLine < 1 || anchor.EndLine < anchor.StartLine {
		return errors.New("invalid line range")
	}
	lines := splitPlanLines(plan)
	if anchor.EndLine > len(lines) {
		return errors.New("line range is outside plan")
	}
	if !hasCharacterRange(anchor) {
		return nil
	}
	if anchor.StartChar < 0 || anchor.EndChar < 0 {
		return errors.New("negative character offset")
	}
	if _, ok := utf16ByteOffset(lines[anchor.StartLine-1], anchor.StartChar); !ok {
		return errors.New("start character is outside line")
	}
	if _, ok := utf16ByteOffset(lines[anchor.EndLine-1], anchor.EndChar); !ok {
		return errors.New("end character is outside line")
	}
	if anchor.StartLine == anchor.EndLine && anchor.EndChar <= anchor.StartChar {
		return errors.New("end character must follow start character")
	}
	return nil
}

func splitPlanLines(plan string) []string {
	if plan == "" {
		return []string{""}
	}
	return stringsSplit(plan)
}

// Kept as a small seam for anchor validation tests and to avoid giving empty
// documents a nonexistent line zero.
func stringsSplit(plan string) []string {
	var lines []string
	start := 0
	for index, value := range plan {
		if value == '\n' {
			lines = append(lines, plan[start:index])
			start = index + 1
		}
	}
	return append(lines, plan[start:])
}

func invariant(message string) error {
	return &TransitionError{Kind: TransitionInvariant, Message: message}
}
