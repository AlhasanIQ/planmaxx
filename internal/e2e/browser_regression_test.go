package e2e

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestBrowserDiffRegression(t *testing.T) {
	if os.Getenv("PLANMAXX_BROWSER_E2E") != "1" {
		t.Skip("set PLANMAXX_BROWSER_E2E=1 to run the Playwright regression")
	}
	t.Run("pending proposal comments and tables", func(t *testing.T) {
		filler := make([]string, 35)
		for index := range filler {
			filler[index] = "Filler line"
		}
		beforeLines := append([]string{"# Regression fixture", "", "old first", "old second", "", "| Capability | Status |", "| --- | --- |", "| Old row | no |", "| Keep row | yes |", ""}, filler...)
		beforeLines = append(beforeLines, "Tail unchanged", "old footer")
		afterLines := append([]string{"# Regression fixture", "", "new first", "new second", "", "| Capability | Status |", "| --- | --- |", "| New row | ready |", "| Added row | ready |", "| Keep row | yes |", ""}, filler...)
		afterLines = append(afterLines, "Tail unchanged", "new footer")
		before, after := strings.Join(beforeLines, "\n"), strings.Join(afterLines, "\n")
		s := session.New("browser-regression", before)
		first := s.AddThread(session.Anchor{StartLine: 3, EndLine: 4}, "replace both lines")
		second := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "overlapping replacement")
		remote := s.AddThread(session.Anchor{StartLine: len(beforeLines) - 1, EndLine: len(beforeLines) - 1}, "unchanged anchor with distant edits")
		s.CreateSectionProposal(session.SectionProposalInput{
			Kind: session.ProposalKindReview, Anchor: session.Anchor{StartLine: 1, EndLine: len(beforeLines)},
			OriginalSection: before, ProposedSection: after, ProposedPlan: after,
			Summary: "Exercise comments, distant changes, and added table rows.", IncludedThreadIDs: []string{first.ID, second.ID, remote.ID},
		})
		runBrowserRegression(t, s, "proposal")
	})

	t.Run("accepted revision feedback placement", func(t *testing.T) {
		before := "head\nold first\nold second\ntail"
		after := "head\nnew first\nnew second\ntail"
		s := session.New("browser-revision-regression", before)
		thread := s.AddThread(session.Anchor{StartLine: 2, EndLine: 3}, "revision placement comment")
		proposal := s.CreateSectionProposal(session.SectionProposalInput{
			ThreadID: thread.ID, Anchor: thread.Anchor, AppliedAnchor: thread.Anchor,
			AppliedHunks:      []session.AppliedHunk{{Anchor: thread.Anchor, Result: thread.Anchor}},
			ReplacementAnchor: thread.Anchor, OriginalSection: "old first\nold second",
			ProposedSection: "new first\nnew second", ProposedPlan: after,
			Summary: "Exercise accepted feedback placement.", IncludedThreadIDs: []string{thread.ID},
		})
		if _, ok := s.ApplyProposal(proposal.ID); !ok {
			t.Fatal("apply revision fixture proposal")
		}
		runBrowserRegression(t, s, "revision")
	})

	t.Run("comment state buckets", func(t *testing.T) {
		s := session.New("browser-comment-states", "one\ntwo\nthree\nfour")
		s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "active instruction")
		s.AddThreadWithIntent(session.Anchor{StartLine: 2, EndLine: 2}, "active private", "", session.ThreadIntentPrivate)
		detached := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "detached feedback")
		addressed := s.AddThread(session.Anchor{StartLine: 4, EndLine: 4}, "addressed feedback")
		_ = s.DetachThread(detached.ID)
		_ = s.AddressThread(addressed.ID, addressed.Anchor)
		s.AddExternalRevision("one\ntwo\nchanged\nfour", "External source change")
		runBrowserRegression(t, s, "states")
	})
}

func runBrowserRegression(t *testing.T, s *session.Session, mode string) {
	t.Helper()
	server := httptest.NewServer(review.NewServer(s).Handler())
	defer server.Close()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	if _, err := exec.LookPath("bun"); err != nil {
		t.Fatalf("browser regression requires Bun on PATH: %v", err)
	}
	command := exec.Command("bun", filepath.Join(root, "scripts", "e2e-browser.mjs"), server.URL, mode)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("browser regression (%s) failed: %v\n%s", mode, err, output)
	}
}
