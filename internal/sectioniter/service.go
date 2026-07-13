package sectioniter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/patches"
	"github.com/AlhasanIQ/planmaxx/internal/planformat"
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
	Anchor              session.Anchor
	ReplacementAnchor   session.Anchor
	RootAppliedAnchor   session.Anchor
	SelectedSection     string
	Protocol            string
	ReviewerInstruction string
	IncludedThreadIDs   []string
	Format              planformat.Format
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
	targetAnchor := req.Anchor
	if req.ReplacementAnchor.StartLine > 0 {
		targetAnchor = req.ReplacementAnchor
	}
	selected := req.SelectedSection
	if strings.TrimSpace(selected) == "" {
		var err error
		selected, err = SectionForAnchor(req.Plan, targetAnchor)
		if err != nil {
			return session.SectionProposalInput{}, err
		}
	}

	prompt := prompts.SectionIteration(prompts.SectionIterationInput{Protocol: req.Protocol, Format: req.Format})
	raw, err := s.client.AskPrompt(ctx, prompt)
	if err != nil {
		return session.SectionProposalInput{}, err
	}
	parsed, err := ParseResponse(raw)
	if err != nil {
		return session.SectionProposalInput{}, err
	}
	if parsed.RevisionID != req.RevisionID {
		return session.SectionProposalInput{}, fmt.Errorf("section iteration response targets revision %q, expected %q", parsed.RevisionID, req.RevisionID)
	}
	if len(parsed.Hunks) > 0 {
		resolved, err := patches.Resolve(req.Plan, parsed.Hunks)
		if err != nil {
			return session.SectionProposalInput{}, err
		}
		proposedPlan := patches.Apply(req.Plan, resolved)
		primary := primaryResolvedHunk(resolved, req.ReplacementAnchor)
		applied := anchorForResolvedHunk(primary)
		proposedAnchor := anchorAfterReplacement(applied, primary.Hunk.Content)
		byteShift := 0
		appliedHunks := make([]session.AppliedHunk, 0, len(resolved))
		for _, hunk := range resolved {
			anchor := anchorForResolvedHunk(hunk)
			resultStart := hunk.StartOffset + byteShift
			resultEnd := resultStart + len(hunk.Hunk.Content)
			startLine, startChar := patches.PositionAt(proposedPlan, resultStart)
			endLine, endChar := patches.PositionAt(proposedPlan, resultEnd)
			result := session.Anchor{StartLine: startLine, StartChar: startChar, EndLine: endLine, EndChar: endChar}
			if hunk.Hunk.Target == "lines" {
				result.StartChar, result.EndChar = 0, 0
			}
			delta := strings.Count(hunk.Hunk.Content, "\n") + 1 - (hunk.EndLine - hunk.StartLine + 1)
			appliedHunks = append(appliedHunks, session.AppliedHunk{Anchor: anchor, Result: result, LineDelta: delta})
			byteShift += len(hunk.Hunk.Content) - (hunk.EndOffset - hunk.StartOffset)
		}
		if req.RootAppliedAnchor.StartLine > 0 {
			// A refinement is calculated against a pending proposed plan. Its exact
			// hunks cannot safely be projected back into the original source, so
			// preserve the declared original scope and use the established
			// single-scope lifecycle path on final apply.
			applied = req.RootAppliedAnchor
			appliedHunks = nil
		}
		return session.SectionProposalInput{ThreadID: req.ThreadID, Anchor: req.Anchor, AppliedAnchor: applied, AppliedHunks: appliedHunks, ReplacementAnchor: proposedAnchor, OriginalSection: selected, ProposedSection: primary.Hunk.Content, ProposedPlan: proposedPlan, Summary: parsed.Summary, Instruction: req.ReviewerInstruction, RawResponse: raw, IncludedThreadIDs: append([]string(nil), req.IncludedThreadIDs...)}, nil
	}
	return session.SectionProposalInput{}, errors.New("section iteration response contains no patch hunks")
}

func primaryResolvedHunk(resolved []patches.Resolved, target session.Anchor) patches.Resolved {
	for _, hunk := range resolved {
		if anchorsOverlap(anchorForResolvedHunk(hunk), target) {
			return hunk
		}
	}
	return resolved[0]
}

func anchorForResolvedHunk(hunk patches.Resolved) session.Anchor {
	anchor := session.Anchor{StartLine: hunk.StartLine, EndLine: hunk.EndLine}
	if hunk.Hunk.Target == "selection" {
		anchor.StartChar = hunk.StartChar
		anchor.EndChar = hunk.EndChar
	}
	return anchor
}

func anchorsOverlap(left, right session.Anchor) bool {
	if left.EndLine < right.StartLine || right.EndLine < left.StartLine {
		return false
	}
	if left.StartLine != left.EndLine || right.StartLine != right.EndLine || !hasCharacterRange(left) || !hasCharacterRange(right) {
		return true
	}
	return left.StartChar < right.EndChar && right.StartChar < left.EndChar
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
