package review

import (
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/reviewmodel"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const clientSchemaVersion = 4

type clientCapabilities struct {
	CanFinalize        bool `json:"canFinalize"`
	CanIterate         bool `json:"canIterate"`
	CanEditFeedback    bool `json:"canEditFeedback"`
	CanRestoreRevision bool `json:"canRestoreRevision"`
	CanApplyProposal   bool `json:"canApplyProposal"`
}

type threadCapabilities struct {
	CanEdit           bool `json:"canEdit"`
	CanReply          bool `json:"canReply"`
	CanChangeIntent   bool `json:"canChangeIntent"`
	CanAsk            bool `json:"canAsk"`
	CanIterate        bool `json:"canIterate"`
	CanReanchor       bool `json:"canReanchor"`
	CanMarkAddressed  bool `json:"canMarkAddressed"`
	CanDelete         bool `json:"canDelete"`
	CanCreateFollowUp bool `json:"canCreateFollowUp"`
}

type threadView struct {
	ID                  string                  `json:"id"`
	Anchor              session.Anchor          `json:"anchor"`
	SelectedText        string                  `json:"selectedText,omitempty"`
	Intent              session.ThreadIntent    `json:"intent"`
	Lifecycle           session.ThreadLifecycle `json:"lifecycle"`
	Bucket              string                  `json:"bucket"`
	Delivery            string                  `json:"delivery"`
	AddressedRevisionID string                  `json:"addressedRevisionId,omitempty"`
	Position            session.Position        `json:"position"`
	Messages            []session.Message       `json:"messages"`
	Capabilities        threadCapabilities      `json:"capabilities"`
}

type sideAnswerCapabilities struct {
	CanInclude     bool `json:"canInclude"`
	CanKeepPrivate bool `json:"canKeepPrivate"`
}

type sideAnswerView struct {
	ID           string                 `json:"id"`
	ThreadID     string                 `json:"threadId"`
	Question     string                 `json:"question"`
	Answer       string                 `json:"answer"`
	Included     bool                   `json:"included"`
	Delivery     string                 `json:"delivery"`
	CreatedAt    time.Time              `json:"createdAt"`
	Capabilities sideAnswerCapabilities `json:"capabilities"`
}

type reviewCounts struct {
	ActiveInstructions int `json:"activeInstructions"`
	ActivePrivateNotes int `json:"activePrivateNotes"`
	IncludedAnswers    int `json:"includedAnswers"`
	PrivateAnswers     int `json:"privateAnswers"`
	DetachedFeedback   int `json:"detachedFeedback"`
	AddressedHistory   int `json:"addressedHistory"`
}

type pendingProposalSummary struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind,omitempty"`
	ParentID          string          `json:"parentId"`
	ThreadID          string          `json:"threadId,omitempty"`
	Anchor            session.Anchor  `json:"anchor"`
	ReplacementAnchor session.Anchor  `json:"replacementAnchor"`
	Summary           string          `json:"summary"`
	Instruction       string          `json:"instruction"`
	ReviewDigest      *session.Digest `json:"reviewDigest,omitempty"`
	Obsolete          bool            `json:"obsolete,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
}

type clientState struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	ID                string                  `json:"id"`
	Plan              string                  `json:"plan"`
	PlanPath          string                  `json:"planPath"`
	PlanFormat        planformat.Format       `json:"planFormat"`
	CurrentRevisionID string                  `json:"currentRevisionId"`
	Revisions         []session.Revision      `json:"revisions"`
	PendingProposal   *pendingProposalSummary `json:"pendingProposal"`
	Threads           []threadView            `json:"threads"`
	SideAnswers       []sideAnswerView        `json:"sideAnswers"`
	Counts            reviewCounts            `json:"counts"`
	Digest            session.Digest          `json:"digest"`
	Phase             string                  `json:"phase"`
	Capabilities      clientCapabilities      `json:"capabilities"`
	ActiveChange      *reviewmodel.ChangeView `json:"activeChange"`
}

func buildClientState(source session.Session, finished bool) clientState {
	revisions := append([]session.Revision{}, source.Revisions...)
	for index := range revisions {
		revisions[index].Plan = ""
		revisions[index].Feedback = nil
	}
	locked := finished || source.PendingProposal != nil
	threads, counts := buildThreadViews(source, locked)
	sideAnswers := buildSideAnswerViews(source, locked, &counts)
	digest := source.Digest
	digest.ReviewerDecisions = nonNilStrings(source.Digest.ReviewerDecisions)
	digest.PromotedSideAnswers = nonNilStrings(source.Digest.PromotedSideAnswers)
	state := clientState{
		SchemaVersion: clientSchemaVersion,
		ID:            source.ID, Plan: source.Plan, PlanPath: source.PlanPath, PlanFormat: source.PlanFormat,
		CurrentRevisionID: source.CurrentRevisionID,
		Revisions:         nonNilRevisions(revisions), Threads: threads,
		SideAnswers: sideAnswers, Counts: counts, Digest: digest,
		Phase:        "active",
		Capabilities: clientCapabilities{CanFinalize: !locked, CanIterate: !locked, CanEditFeedback: !locked, CanRestoreRevision: !locked},
	}
	if finished {
		state.Phase = "terminal"
	}
	if proposal := source.PendingProposal; proposal != nil {
		state.Phase = "proposal_pending"
		state.Capabilities.CanApplyProposal = !finished && !proposal.Obsolete && proposal.ParentID == source.CurrentRevisionID
		state.PendingProposal = &pendingProposalSummary{
			ID: proposal.ID, Kind: proposal.Kind, ParentID: proposal.ParentID, ThreadID: proposal.ThreadID,
			Anchor: proposal.Anchor, ReplacementAnchor: proposal.ReplacementAnchor,
			Summary: proposal.Summary, Instruction: proposal.Instruction,
			ReviewDigest: cloneDigestPointer(proposal.ReviewDigest), Obsolete: proposal.Obsolete, CreatedAt: proposal.CreatedAt,
		}
		change := reviewmodel.Build(reviewmodel.BuildInput{
			Mode: reviewmodel.ModeProposal, BaseID: proposal.ParentID, TargetID: proposal.ID,
			Format: string(source.PlanFormat), Before: source.Plan, After: proposal.ProposedPlan, Threads: source.Threads,
			ReviewThreadIDs: proposal.IncludedThreadIDs,
		})
		state.ActiveChange = &change
	}
	return state
}

func buildThreadViews(source session.Session, locked bool) ([]threadView, reviewCounts) {
	addressedByThread := map[string]string{}
	for _, revision := range source.Revisions {
		for _, feedback := range revision.Feedback {
			addressedByThread[feedback.ThreadID] = revision.ID
		}
	}
	views := make([]threadView, 0, len(source.Threads))
	counts := reviewCounts{}
	for _, thread := range source.Threads {
		lifecycle := thread.Lifecycle()
		intent := thread.Intent()
		bucket, delivery := "active", "private"
		capabilities := threadCapabilities{}
		switch lifecycle {
		case session.ThreadLifecycleActive:
			if intent == session.ThreadIntentInstruction {
				delivery = "iteration"
				counts.ActiveInstructions++
			} else {
				counts.ActivePrivateNotes++
			}
			if !locked {
				capabilities = threadCapabilities{CanEdit: true, CanReply: true, CanChangeIntent: true, CanAsk: true, CanIterate: true, CanDelete: true}
			}
		case session.ThreadLifecycleDetached:
			bucket, delivery = "attention", "none"
			counts.DetachedFeedback++
			if !locked {
				capabilities = threadCapabilities{CanEdit: true, CanReanchor: true, CanMarkAddressed: true, CanDelete: true}
			}
		case session.ThreadLifecycleAddressed:
			bucket, delivery = "history", "none"
			counts.AddressedHistory++
			if !locked {
				capabilities = threadCapabilities{CanDelete: true, CanCreateFollowUp: true}
			}
		}
		views = append(views, threadView{ID: thread.ID, Anchor: thread.Anchor, SelectedText: thread.SelectedText, Intent: intent, Lifecycle: lifecycle, Bucket: bucket, Delivery: delivery, AddressedRevisionID: addressedByThread[thread.ID], Position: thread.Position, Messages: append([]session.Message{}, thread.Messages...), Capabilities: capabilities})
	}
	return views, counts
}

func buildSideAnswerViews(source session.Session, locked bool, counts *reviewCounts) []sideAnswerView {
	threadLifecycle := make(map[string]session.ThreadLifecycle, len(source.Threads))
	for _, thread := range source.Threads {
		threadLifecycle[thread.ID] = thread.Lifecycle()
	}
	views := make([]sideAnswerView, 0, len(source.SideAnswers))
	for _, answer := range source.SideAnswers {
		active := threadLifecycle[answer.ThreadID] == session.ThreadLifecycleActive
		included := active && answer.Promoted
		delivery := "private"
		if !active {
			delivery = "none"
		} else if included {
			delivery = "iteration"
		}
		if included {
			counts.IncludedAnswers++
		} else if active {
			counts.PrivateAnswers++
		}
		caps := sideAnswerCapabilities{}
		if active && !locked {
			caps.CanInclude = !included
			caps.CanKeepPrivate = included
		}
		views = append(views, sideAnswerView{ID: answer.ID, ThreadID: answer.ThreadID, Question: answer.Question, Answer: answer.Answer, Included: included, Delivery: delivery, CreatedAt: answer.CreatedAt, Capabilities: caps})
	}
	return views
}

func projectThread(source session.Session, finished bool, threadID string) (threadView, bool) {
	views, _ := buildThreadViews(source, finished || source.PendingProposal != nil)
	for _, view := range views {
		if view.ID == threadID {
			return view, true
		}
	}
	return threadView{}, false
}

func projectSideAnswer(source session.Session, finished bool, answerID string) (sideAnswerView, bool) {
	counts := reviewCounts{}
	views := buildSideAnswerViews(source, finished || source.PendingProposal != nil, &counts)
	for _, view := range views {
		if view.ID == answerID {
			return view, true
		}
	}
	return sideAnswerView{}, false
}

func nonNilStrings(source []string) []string {
	if source == nil {
		return []string{}
	}
	return source
}
func nonNilRevisions(source []session.Revision) []session.Revision {
	if source == nil {
		return []session.Revision{}
	}
	return source
}
