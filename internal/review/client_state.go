package review

import (
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/reviewmodel"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const clientSchemaVersion = 2

type clientCapabilities struct {
	CanFinalize        bool `json:"canFinalize"`
	CanEditFeedback    bool `json:"canEditFeedback"`
	CanRestoreRevision bool `json:"canRestoreRevision"`
	CanApplyProposal   bool `json:"canApplyProposal"`
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
	Threads           []session.Thread        `json:"threads"`
	SideAnswers       []session.SideAnswer    `json:"sideAnswers"`
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
	threads := append([]session.Thread{}, source.Threads...)
	for index := range threads {
		threads[index].Messages = append([]session.Message{}, source.Threads[index].Messages...)
	}
	sideAnswers := append([]session.SideAnswer{}, source.SideAnswers...)
	digest := source.Digest
	digest.ReviewerDecisions = nonNilStrings(source.Digest.ReviewerDecisions)
	digest.PromotedSideAnswers = nonNilStrings(source.Digest.PromotedSideAnswers)
	state := clientState{
		SchemaVersion: clientSchemaVersion,
		ID:            source.ID, Plan: source.Plan, PlanPath: source.PlanPath, PlanFormat: source.PlanFormat,
		CurrentRevisionID: source.CurrentRevisionID,
		Revisions:         nonNilRevisions(revisions), Threads: nonNilThreads(threads),
		SideAnswers: nonNilSideAnswers(sideAnswers), Digest: digest,
		Phase:        "active",
		Capabilities: clientCapabilities{CanFinalize: !finished && source.PendingProposal == nil, CanEditFeedback: !finished && source.PendingProposal == nil, CanRestoreRevision: !finished && source.PendingProposal == nil},
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
			Format: string(source.PlanFormat), Before: source.Plan, After: proposal.ProposedPlan, Threads: threads,
		})
		state.ActiveChange = &change
	}
	return state
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
func nonNilThreads(source []session.Thread) []session.Thread {
	if source == nil {
		return []session.Thread{}
	}
	return source
}
func nonNilSideAnswers(source []session.SideAnswer) []session.SideAnswer {
	if source == nil {
		return []session.SideAnswer{}
	}
	return source
}
