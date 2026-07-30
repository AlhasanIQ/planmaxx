package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

type browserAgent struct {
	responses []string
}

func (a *browserAgent) Ask(context.Context, sidequestions.Request) (string, error) {
	return "Keep the rollback owner explicit and verify the fallback path before rollout.", nil
}

func (a *browserAgent) AskPrompt(context.Context, string) (string, error) {
	if len(a.responses) == 0 {
		return "", sectioniter.ErrUnavailable
	}
	response := a.responses[0]
	a.responses = a.responses[1:]
	return response, nil
}

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

	t.Run("html preview annotations and outline navigation", func(t *testing.T) {
		plan := `<main>
<h1>Launch &amp; rollback plan</h1>
<p>Owner: <strong>Platform</strong> 🚀</p>
<section aria-label="Rollout controls">
<h2>Safety checks</h2>
<ol>
<li>Verify backups</li>
<li>Ship &amp; iterate carefully.</li>
</ol>
<table>
<caption>Rollout gates</caption>
<thead><tr><th>Gate</th><th>Status</th></tr></thead>
<tbody><tr><td>Error budget</td><td>Ready</td></tr><tr><td>Rollback</td><td>Required</td></tr></tbody>
</table>
<svg viewBox="0 0 240 80" role="img" aria-label="Rollout flow">
<rect x="5" y="15" width="70" height="40"></rect><text x="18" y="40">Build</text>
<path d="M80 35 L145 45" stroke="currentColor"></path>
<rect x="150" y="15" width="80" height="40"></rect><text x="162" y="40">Deploy</text>
</svg>
<details><summary>Fallback</summary><p>Restore the previous revision.</p></details>
<section aria-label="Operational appendix">
<p>Confirm the primary owner.</p>
<p>Confirm the backup owner.</p>
<p>Record the change window.</p>
<p>Record the rollback window.</p>
<p>Verify regional capacity.</p>
<p>Verify queue depth.</p>
<p>Verify database replicas.</p>
<p>Verify alert routing.</p>
<p>Verify support coverage.</p>
<p>Verify status messaging.</p>
<p>Verify audit retention.</p>
<p>Verify the final checkpoint.</p>
<p style="max-width:260px">Review <span>the deliberately long inline planning constraint that wraps across multiple rendered lines before approval</span>.</p>
</section>
<pre><code>planmaxx review launch.html</code></pre>
</section>
</main>`
		s := session.NewWithFormat("browser-html-outline", plan, planformat.HTML)
		s.AddThread(session.Anchor{StartLine: 5, EndLine: 5}, "Existing heading element comment")
		s.AddThread(session.Anchor{StartLine: 8, StartChar: 9, EndLine: 8, EndChar: 16}, "Existing list comment")
		s.AddThread(session.Anchor{StartLine: 13, StartChar: 44, EndLine: 13, EndChar: 52}, "Existing table comment")
		runBrowserRegression(t, s, "html",
			`<planmaxx_proposal version="1" revision="rev-1"><summary>Safer rollout wording.</summary><replacement target="selection"><expected>iterate</expected><content>iterate safely</content></replacement></planmaxx_proposal>`,
			`<planmaxx_proposal version="1" revision="rev-1"><summary>Added rollback emphasis.</summary><replacement target="selection"><expected>iterate safely</expected><content>iterate safely with rollback</content></replacement></planmaxx_proposal>`,
		)
	})

	t.Run("markdown comments side questions and repeated iteration", func(t *testing.T) {
		plan := `# Launch plan

Owner: **Platform** 🚀

## Rollout

1. Verify backups
2. Ship carefully
   - Watch error budget
   - Keep rollback ready

| Gate | Status |
| --- | --- |
| Error budget | Ready |
| Rollback | Required |

` + "```mermaid\nflowchart LR\n  Build --> Deploy\n```\n\n> Stop if the error budget is exhausted.\n"
		s := session.New("browser-markdown-workflows", plan)
		s.AddThread(session.Anchor{StartLine: 9, StartChar: 5, EndLine: 9, EndChar: 23}, "Existing nested-list comment")
		s.AddThread(session.Anchor{StartLine: 14, EndLine: 14}, "Existing table-row comment")
		runBrowserRegression(t, s, "markdown-workflows",
			`<planmaxx_proposal version="1" revision="rev-1"><summary>Clarified rollout action.</summary><replacement target="selection"><expected>Ship carefully</expected><content>Ship with a canary</content></replacement></planmaxx_proposal>`,
			`<planmaxx_proposal version="1" revision="rev-1"><summary>Added rollback wording.</summary><replacement target="selection"><expected>Ship with a canary</expected><content>Ship with a canary and rollback gate</content></replacement></planmaxx_proposal>`,
		)
	})
}

func runBrowserRegression(t *testing.T, s *session.Session, mode string, responses ...string) {
	t.Helper()
	// This browser fixture exercises the assisted-action controls but never
	// submits an agent request. Advertise the same server-authoritative
	// attachment state that a validated provider would publish in production.
	agent := &browserAgent{responses: responses}
	reviewServer := review.NewServer(s).WithAgent(review.AgentInfo{
		ID: "test-agent", DisplayName: "Test Agent", ContextMode: "current-session-fork", Available: true,
	}).WithSideQuestions(sidequestions.NewService("test-thread", agent)).
		WithSectionIterations(sectioniter.NewService("test-thread", agent))
	server := httptest.NewServer(reviewServer.Handler())
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
