package handoff

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/reviewxml"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func Format(s session.Session) (string, error) {
	digest, err := encodeDigest(s.Digest)
	if err != nil {
		return "", err
	}

	return prompts.ApprovedHandoff(s.Plan, digest, reviewxml.Handoff(s), noReviewItems(s.Digest)), nil
}

func FormatRejected(s session.Session) (string, error) {
	digest, err := encodeDigest(s.Digest)
	if err != nil {
		return "", err
	}

	return prompts.RejectedHandoff(s.Plan, digest, reviewxml.Handoff(s)), nil
}

func encodeDigest(d session.Digest) (string, error) {
	var digest bytes.Buffer
	encoder := json.NewEncoder(&digest)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(d); err != nil {
		return "", fmt.Errorf("encode review digest: %w", err)
	}
	return digest.String(), nil
}

func noReviewItems(d session.Digest) bool {
	return len(d.ReviewerDecisions) == 0 && len(d.PromotedSideAnswers) == 0
}
