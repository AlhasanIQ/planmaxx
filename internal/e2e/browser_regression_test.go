package e2e

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestBrowserDiffRegression(t *testing.T) {
	if os.Getenv("PLANMAXX_BROWSER_E2E") != "1" {
		t.Skip("set PLANMAXX_BROWSER_E2E=1 to run the Playwright regression")
	}
	t.Run("pending proposal comments and tables", func(t *testing.T) {
		before := "# Regression fixture\n\nold first\nold second\n\n| Capability | Status |\n| --- | --- |\n| Old row | no |\n| Keep row | yes |\n\nTail"
		after := "# Regression fixture\n\nnew first\nnew second\n\n| Capability | Status |\n| --- | --- |\n| New row | ready |\n| Added row | ready |\n| Keep row | yes |\n\nTail"
		s := session.New("browser-regression", before)
		first := s.AddThread(session.Anchor{StartLine: 3, EndLine: 4}, "replace both lines")
		second := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "overlapping replacement")
		s.CreateSectionProposal(session.SectionProposalInput{
			Kind: session.ProposalKindReview, Anchor: session.Anchor{StartLine: 1, EndLine: 11},
			OriginalSection: before, ProposedSection: after, ProposedPlan: after,
			Summary: "Exercise comments and added table rows.", IncludedThreadIDs: []string{first.ID, second.ID},
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
