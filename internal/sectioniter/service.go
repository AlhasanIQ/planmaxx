package sectioniter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

var ErrUnavailable = errors.New("section iteration unavailable")

type PromptClient interface {
	AskPrompt(ctx context.Context, prompt string) (string, error)
}

type Service struct {
	currentThreadID string
	client          PromptClient
}

type Request struct {
	RevisionID          string
	ThreadID            string
	Plan                string
	FilePath            string
	Reference           string
	Anchor              session.Anchor
	ReplacementAnchor   session.Anchor
	SelectedSection     string
	PlanExcerpt         string
	ReviewerInstruction string
	ReviewerDecisions   []string
	PromotedSideAnswers []string
	IncludedThreadIDs   []string
}

func NewService(currentThreadID string, client PromptClient) Service {
	return Service{currentThreadID: currentThreadID, client: client}
}

func (s Service) Propose(ctx context.Context, req Request) (session.SectionProposalInput, error) {
	if err := ctx.Err(); err != nil {
		return session.SectionProposalInput{}, err
	}
	if strings.TrimSpace(req.ReviewerInstruction) == "" {
		return session.SectionProposalInput{}, fmt.Errorf("reviewer instruction is required")
	}
	if s.currentThreadID == "" || s.client == nil {
		return session.SectionProposalInput{}, ErrUnavailable
	}
	selected := req.SelectedSection
	if strings.TrimSpace(selected) == "" {
		var err error
		selected, err = SectionForAnchor(req.Plan, req.Anchor)
		if err != nil {
			return session.SectionProposalInput{}, err
		}
	}

	prompt := prompts.SectionIteration(prompts.SectionIterationInput{
		RevisionID:          req.RevisionID,
		FilePath:            req.FilePath,
		Reference:           req.Reference,
		SelectedSection:     selected,
		PlanExcerpt:         req.PlanExcerpt,
		ReviewerInstruction: req.ReviewerInstruction,
		ReviewerDecisions:   req.ReviewerDecisions,
		PromotedSideAnswers: req.PromotedSideAnswers,
	})
	raw, err := s.client.AskPrompt(ctx, prompt)
	if err != nil {
		return session.SectionProposalInput{}, err
	}
	parsed, err := ParseResponse(raw)
	if err != nil {
		return session.SectionProposalInput{}, err
	}
	replacementAnchor := req.Anchor
	if req.ReplacementAnchor.StartLine > 0 {
		replacementAnchor = req.ReplacementAnchor
	}
	proposedPlan, err := ReplaceSection(req.Plan, replacementAnchor, parsed.Replacement)
	if err != nil {
		return session.SectionProposalInput{}, err
	}
	proposedAnchor := anchorAfterReplacement(replacementAnchor, parsed.Replacement)

	return session.SectionProposalInput{
		ThreadID:          req.ThreadID,
		Anchor:            req.Anchor,
		ReplacementAnchor: proposedAnchor,
		OriginalSection:   selected,
		ProposedSection:   parsed.Replacement,
		ProposedPlan:      proposedPlan,
		Summary:           parsed.Summary,
		Instruction:       req.ReviewerInstruction,
		RawResponse:       raw,
		IncludedThreadIDs: append([]string(nil), req.IncludedThreadIDs...),
	}, nil
}

func anchorAfterReplacement(anchor session.Anchor, replacement string) session.Anchor {
	replacementLines := strings.Split(replacement, "\n")
	endLine := anchor.StartLine + len(replacementLines) - 1
	if !hasCharacterRange(anchor) {
		return session.Anchor{
			StartLine: anchor.StartLine,
			EndLine:   endLine,
		}
	}
	if len(replacementLines) == 1 {
		return session.Anchor{
			StartLine: anchor.StartLine,
			StartChar: anchor.StartChar,
			EndLine:   anchor.StartLine,
			EndChar:   anchor.StartChar + utf16Length(replacementLines[0]),
		}
	}
	return session.Anchor{
		StartLine: anchor.StartLine,
		StartChar: anchor.StartChar,
		EndLine:   endLine,
		EndChar:   utf16Length(replacementLines[len(replacementLines)-1]),
	}
}
