package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	reviewstore "github.com/AlhasanIQ/planmaxx/internal/review"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type reviewProcess struct {
	url      string
	planPath string
	stdout   *lockedBuffer
	stderr   *lockedBuffer
	done     chan error
	cancel   context.CancelFunc
}

func TestFinalizeSimplePlanWritesHandoff(t *testing.T) {
	review := startReview(t, realisticPlan("Feature rollout"), nil)
	health := getHealth(t, review.url)
	if health.Status != "active" || health.SessionID != "session-1" {
		t.Fatalf("unexpected health response %+v", health)
	}
	state := getState(t, review.url)
	if state.Agent.ID != "none" || state.Agent.Available {
		t.Fatalf("default auto mode should start a standalone review without agent context: %+v", state.Agent)
	}

	finalize(t, review.url, digest("Approved simple review", nil, nil))
	waitSuccess(t, review)

	output := review.stdout.String()
	assertContains(t, output, "Continue the user's approved plan work.")
	assertContains(t, output, "Approved simple review")
}

func TestStandaloneManualReviewNeedsNoAgent(t *testing.T) {
	review := startReviewWithArgs(
		t,
		realisticPlan("Standalone manual review"),
		[]string{"--agent", "none", "--no-browser"},
		map[string]string{
			"PLANMAXX_AGENT":             "codex",
			"CODEX_THREAD_ID":            "stale-codex-thread",
			"CLAUDE_CODE_SESSION_ID":     "not-a-session",
			"PLANMAXX_CLAUDE_SESSION_ID": "not-a-session",
			"GROK_SESSION_ID":            "not-a-session",
		},
	)

	state := getState(t, review.url)
	if state.Agent.ID != "none" || state.Agent.Available || state.Agent.ContextMode != "unavailable" {
		t.Fatalf("unexpected standalone agent state: %+v", state.Agent)
	}
	if state.Capabilities.CanIterate {
		t.Fatalf("standalone review advertised agent iteration: %+v", state.Capabilities)
	}

	thread := createThread(t, review.url, 3, 3, "Review this manually.")
	state = getState(t, review.url)
	if len(state.Threads) != 1 {
		t.Fatalf("expected standalone review thread, got %+v", state.Threads)
	}
	if state.Threads[0].Capabilities.CanAsk || state.Threads[0].Capabilities.CanIterate {
		t.Fatalf("standalone thread advertised assisted actions: %+v", state.Threads[0].Capabilities)
	}
	if !state.Threads[0].Capabilities.CanEdit || !state.Threads[0].Capabilities.CanReply {
		t.Fatalf("standalone thread lost ordinary review actions: %+v", state.Threads[0].Capabilities)
	}

	payload := fmt.Sprintf(`{"threadID":%q,"question":"Why?","planExcerpt":"manual"}`, thread.ID)
	postJSON(t, review.url+"/api/side-questions", payload, http.StatusServiceUnavailable)
	postJSON(t, review.url+"/api/threads/"+thread.ID+"/reply", `{"body":"Manual decision."}`, http.StatusOK)

	draft := draftDigest(t, review.url)
	if !containsString(draft.ReviewerDecisions, "Review this manually.") ||
		!containsString(draft.ReviewerDecisions, "Manual decision.") {
		t.Fatalf("standalone decisions missing from digest: %+v", draft.ReviewerDecisions)
	}
	finalize(t, review.url, draft)
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "Manual decision.")
}

func TestCancelReviewExitsNonZeroWithoutHandoff(t *testing.T) {
	review := startReview(t, realisticPlan("Cancel path"), nil)

	postJSON(t, review.url+"/api/cancel", `{}`, http.StatusOK)
	err := waitDone(t, review)
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("expected non-zero cancel exit, got %v", err)
	}
	if strings.Contains(review.stdout.String(), "Continue the user's approved plan work.") {
		t.Fatalf("expected no handoff on cancel, got %q", review.stdout.String())
	}
}

func TestBrowserPresenceKeepsReviewAliveThenOrphanCleanupStopsIt(t *testing.T) {
	const timeout = time.Second
	plan := realisticPlan("Browser presence")
	review := startReviewWithArgs(t, plan, []string{
		"--agent", "none",
		"--no-browser",
		"--local-bundle",
		"--orphan-timeout", timeout.String(),
	}, nil)

	wsURL := "ws" + strings.TrimPrefix(review.url, "http") + "/api/presence"
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{review.url},
	})
	if err != nil {
		if response != nil {
			t.Fatalf("connect browser presence: %v (status %d)", err, response.StatusCode)
		}
		t.Fatalf("connect browser presence: %v", err)
	}

	select {
	case err := <-review.done:
		_ = conn.Close()
		t.Fatalf("review exited while browser was connected: %v\nstderr:\n%s", err, review.stderr.String())
	case <-time.After(timeout + 250*time.Millisecond):
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close browser presence: %v", err)
	}
	err = waitDone(t, review)
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("expected non-zero orphan exit, got %v", err)
	}
	assertContains(t, review.stderr.String(), "orphan cleanup stopped the review automatically after 1s with no browser tabs connected")
	assertContains(t, review.stderr.String(), "--orphan-timeout 0")
	if got, err := os.ReadFile(review.planPath); err != nil || string(got) != plan {
		t.Fatalf("orphan cleanup changed source plan: %q, %v", got, err)
	}
	bundle, err := reviewstore.OpenBundleStore(review.planPath + ".planmaxx")
	if err != nil {
		t.Fatalf("open preserved review bundle: %v", err)
	}
	defer bundle.Close()
	saved, ok := bundle.Load()
	if !ok || saved.Status != "active" {
		t.Fatalf("expected resumable active bundle, got ok=%v status=%q", ok, saved.Status)
	}
	if strings.Contains(review.stdout.String(), "Continue the user's approved plan work.") {
		t.Fatalf("orphan cleanup printed an approval handoff: %q", review.stdout.String())
	}
}

func TestThreadRepliesBecomeReviewerDecisions(t *testing.T) {
	review := startReview(t, realisticPlan("Reply decisions"), nil)
	thread := createThread(t, review.url, 3, 3, "Clarify rollout order.")
	postJSON(t, review.url+"/api/threads/"+thread.ID+"/reply", `{"body":"Ship CLI before UI polish."}`, http.StatusOK)

	draft := draftDigest(t, review.url)
	if !containsString(draft.ReviewerDecisions, "Clarify rollout order.") || !containsString(draft.ReviewerDecisions, "Ship CLI before UI polish.") {
		t.Fatalf("expected comment and reply in reviewer decisions, got %+v", draft.ReviewerDecisions)
	}

	finalize(t, review.url, draft)
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "Ship CLI before UI polish.")
}

func TestAgentProposalApplyAndRestoreUseDurableRevisionHistory(t *testing.T) {
	fake := installFakeCodex(t, `<planmaxx_proposal version="1" revision="{{REVISION}}"><summary>Clarified the first step.</summary><replacement target="lines"><expected>1. Verify the CLI contract and localhost server behavior.</expected><content>1. Verify the CLI contract, localhost server behavior, and recovery path.</content></replacement></planmaxx_proposal>`)
	review := startReview(t, realisticPlan("Revision history"), map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "current-thread",
	})

	var proposal struct {
		ID string `json:"id"`
	}
	postJSONInto(t, review.url+"/api/revisions/propose-section", `{"anchor":{"startLine":10,"endLine":10},"instruction":"Clarify the first step."}`, http.StatusOK, &proposal)
	if proposal.ID == "" {
		t.Fatal("expected agent proposal")
	}
	postJSON(t, review.url+"/api/revisions/proposals/"+proposal.ID+"/apply", `{}`, http.StatusOK)

	state := getState(t, review.url)
	if state.CurrentRevisionID != "rev-2" || !strings.Contains(state.Plan, "recovery path") {
		t.Fatalf("expected applied Git-backed revision, got %+v", state)
	}
	postJSON(t, review.url+"/api/revisions/rev-1/restore", `{}`, http.StatusOK)
	state = getState(t, review.url)
	if state.CurrentRevisionID != "rev-3" || strings.Contains(state.Plan, "recovery path") {
		t.Fatalf("expected append-only restore to initial content, got %+v", state)
	}
	finalize(t, review.url, digest("Revision history approved", nil, nil))
	waitSuccess(t, review)
}

func TestFinalizeWritesAppliedPlanToSourceByDefault(t *testing.T) {
	fake := installFakeCodex(t, `<planmaxx_proposal version="1" revision="{{REVISION}}"><summary>Clarified the first step.</summary><replacement target="lines"><expected>1. Verify the CLI contract and localhost server behavior.</expected><content>1. Verify the CLI contract, localhost server behavior, and recovery path.</content></replacement></planmaxx_proposal>`)
	review := startReview(t, realisticPlan("Save source plan"), map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "current-thread",
	})

	var proposal struct {
		ID string `json:"id"`
	}
	postJSONInto(t, review.url+"/api/revisions/propose-section", `{"anchor":{"startLine":10,"endLine":10},"instruction":"Clarify the first step."}`, http.StatusOK, &proposal)
	postJSON(t, review.url+"/api/revisions/proposals/"+proposal.ID+"/apply", `{}`, http.StatusOK)

	state := getState(t, review.url)
	if !strings.Contains(state.Plan, "recovery path") {
		t.Fatalf("expected applied revision, got %q", state.Plan)
	}
	finalize(t, review.url, digest("Applied revision approved", nil, nil))
	waitSuccess(t, review)

	saved, err := os.ReadFile(review.planPath)
	if err != nil {
		t.Fatalf("read source plan: %v", err)
	}
	if got := string(saved); got != state.Plan {
		t.Fatalf("source plan = %q, want finalized plan %q", got, state.Plan)
	}
}

func TestMoveAndReanchorPersistUntilFinalize(t *testing.T) {
	review := startReview(t, realisticPlan("Move reanchor"), nil)
	thread := createThread(t, review.url, 3, 3, "Move me.")

	postJSON(t, review.url+"/api/threads/"+thread.ID+"/move", `{"x":144,"y":288}`, http.StatusOK)
	postJSON(t, review.url+"/api/threads/"+thread.ID+"/reanchor", `{"startLine":5,"endLine":6}`, http.StatusOK)

	state := getState(t, review.url)
	got := state.Threads[0]
	if got.Position.X != 144 || got.Position.Y != 288 {
		t.Fatalf("expected moved position, got %+v", got.Position)
	}
	if got.Anchor.StartLine != 5 || got.Anchor.EndLine != 6 {
		t.Fatalf("expected reanchor 5-6, got %+v", got.Anchor)
	}

	finalize(t, review.url, digest("Move reanchor approved", nil, nil))
	waitSuccess(t, review)
}

func TestAppServerSideQuestionCanBePromotedIntoHandoff(t *testing.T) {
	fake := installFakeCodex(t, "App-server answer from original context.")
	review := startReview(t, realisticPlan("App-server side question"), map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "current-thread",
	})
	thread := createThread(t, review.url, 5, 5, "Ask app-server.")

	answer := askSideQuestion(t, review.url, thread.ID, "Why this order?", "1. CLI\n2. UI")
	if answer.Answer != "App-server answer from original context." {
		t.Fatalf("unexpected side answer %q", answer.Answer)
	}
	postJSON(t, review.url+"/api/side-answers/"+answer.ID+"/promote", `{}`, http.StatusOK)

	draft := draftDigest(t, review.url)
	if len(draft.PromotedSideAnswers) != 1 ||
		!strings.Contains(draft.PromotedSideAnswers[0], "Question:\nWhy this order?") ||
		!strings.Contains(draft.PromotedSideAnswers[0], "Answer:\n"+answer.Answer) {
		t.Fatalf("expected promoted answer in digest, got %+v", draft.PromotedSideAnswers)
	}
	finalize(t, review.url, draft)
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "App-server answer from original context.")
	assertFileContains(t, fake.stdinPath, "Why this order?")
}

func TestClaudeCodeSkillInvocationUsesExactActiveSessionFork(t *testing.T) {
	const sessionID = "6211ea92-c582-4a27-b067-8ed5ba92348d"
	fake := installFakeClaude(t, "Claude answer from the active session.")
	review := startReviewWithArgs(t, realisticPlan("Claude side question"), []string{
		"--claude-session-id", sessionID,
	}, map[string]string{
		"PATH":                   fake.pathEnv,
		"CLAUDE_CODE_SESSION_ID": "9c6954d4-9180-4876-bbfe-8592bad9a6d8",
	})
	state := getState(t, review.url)
	if state.Agent.ID != "claude" || state.Agent.DisplayName != "Claude Code" || !state.Agent.Available || state.Agent.ContextMode != "current-session-fork" {
		t.Fatalf("unexpected attached Claude state: %+v", state.Agent)
	}
	thread := createThread(t, review.url, 5, 5, "Ask Claude.")

	answer := askSideQuestion(t, review.url, thread.ID, "Why this order?", "1. CLI\n2. UI")
	if answer.Answer != "Claude answer from the active session." {
		t.Fatalf("unexpected side answer %q", answer.Answer)
	}
	assertFileContains(t, fake.stdinPath, `"args": ["-p", "--resume", "`+sessionID+`", "--fork-session", "--safe-mode", "--no-session-persistence", "--output-format", "json", "--tools", "", "--permission-mode", "dontAsk"]`)
	assertFileContains(t, fake.stdinPath, "Why this order?")

	finalize(t, review.url, digest("Claude review approved", nil, nil))
	waitSuccess(t, review)
}

func TestGrokBuildSkillInvocationUsesExactActiveSessionFork(t *testing.T) {
	const sessionID = "019c0f29-e4f8-7c7b-bb88-b3f7a68e605d"
	fake := installFakeGrok(t, "Grok answer from the active session.")
	sourceGrokHome := installFakeGrokSession(t, sessionID, "The source conversation context.")
	review := startReviewWithArgs(t, realisticPlan("Grok side question"), []string{
		"--grok-session-id", sessionID,
	}, map[string]string{
		"PATH":            fake.pathEnv,
		"GROK_HOME":       sourceGrokHome,
		"GROK_SESSION_ID": "36ce14ad-f0b7-4544-97d7-e1097e344268",
	})
	state := getState(t, review.url)
	if state.Agent.ID != "grok" || state.Agent.DisplayName != "Grok Build" || !state.Agent.Available || state.Agent.ContextMode != "current-session-fork" {
		t.Fatalf("unexpected attached Grok state: %+v", state.Agent)
	}
	thread := createThread(t, review.url, 5, 5, "Ask Grok.")

	answer := askSideQuestion(t, review.url, thread.ID, "Why this order?", "1. CLI\n2. UI")
	if answer.Answer != "Grok answer from the active session." {
		t.Fatalf("unexpected side answer %q", answer.Answer)
	}
	for _, want := range []string{
		`"command": "fork"`,
		`"sourceSessionId": "` + sessionID + `"`,
		`"sessionKind": "fork"`,
		`"--cwd"`,
		`"--output-format", "json"`,
		`"--tools", "read_file,grep,list_dir"`,
		`"--allow", "Read(`,
		`"--allow", "Grep(`,
		`"--deny", "MCPTool"`,
		`"--permission-mode", "dontAsk"`,
		`"--sandbox", "planmaxx-isolated"`,
		`"--no-subagents"`,
		`"--no-memory"`,
		`"--disable-web-search"`,
		`"--no-plan"`,
		`"--max-turns", "2"`,
		`"auto_update_disabled": true`,
		`"isolated_home": true`,
		`"isolated_grok_home": true`,
		`"isolated_claude_config": true`,
		"provided read-only tools",
		"Do not edit files",
		"Why this order?",
		`"command": "delete"`,
	} {
		assertFileContains(t, fake.stdinPath, want)
	}
	if strings.Contains(readFileString(t, fake.stdinPath), `"--tools", ""`) {
		t.Fatal("Grok integration must never use an empty tool allowlist")
	}
	if strings.Contains(readFileString(t, fake.stdinPath), `"--fork-session"`) {
		t.Fatal("the relocated ACP child must not be forked a second time")
	}

	finalize(t, review.url, digest("Grok review approved", nil, nil))
	waitSuccess(t, review)
}

func TestGrokBuildSectionAndWholeReviewIterationParseAndApply(t *testing.T) {
	const sessionID = "019c0f29-e4f8-7c7b-bb88-b3f7a68e605d"
	fake := installFakeGrok(t, `<planmaxx_proposal version="1" revision="{{REVISION}}"><summary>Added recovery.</summary><replacement target="lines"><expected>1. Verify the CLI contract and localhost server behavior.</expected><content>1. Verify the CLI contract, localhost server behavior, and recovery path.</content></replacement></planmaxx_proposal>`)
	sourceGrokHome := installFakeGrokSession(t, sessionID, "The source conversation context.")
	review := startReviewWithArgs(t, realisticPlan("Grok iteration"), []string{
		"--grok-session-id", sessionID,
	}, map[string]string{
		"PATH":      fake.pathEnv,
		"GROK_HOME": sourceGrokHome,
	})

	var sectionProposal struct {
		ID           string `json:"id"`
		Kind         string `json:"kind"`
		ProposedPlan string `json:"proposedPlan"`
	}
	postJSONInto(
		t,
		review.url+"/api/revisions/propose-section",
		`{"anchor":{"startLine":10,"endLine":10},"instruction":"Add the recovery path."}`,
		http.StatusOK,
		&sectionProposal,
	)
	if sectionProposal.ID == "" || sectionProposal.Kind != "" ||
		!strings.Contains(sectionProposal.ProposedPlan, "recovery path") {
		t.Fatalf("unexpected Grok section proposal: %+v", sectionProposal)
	}
	postJSON(t, review.url+"/api/revisions/proposals/"+sectionProposal.ID+"/discard", `{}`, http.StatusOK)

	var reviewProposal struct {
		ID           string `json:"id"`
		Kind         string `json:"kind"`
		ProposedPlan string `json:"proposedPlan"`
	}
	postJSONInto(
		t,
		review.url+"/api/revisions/propose-review",
		`{"summary":"Add the recovery path before approval."}`,
		http.StatusOK,
		&reviewProposal,
	)
	if reviewProposal.ID == "" || reviewProposal.Kind != "review" ||
		!strings.Contains(reviewProposal.ProposedPlan, "recovery path") {
		t.Fatalf("unexpected Grok whole-review proposal: %+v", reviewProposal)
	}
	postJSON(t, review.url+"/api/revisions/proposals/"+reviewProposal.ID+"/apply", `{}`, http.StatusOK)
	if state := getState(t, review.url); state.CurrentRevisionID != "rev-2" ||
		!strings.Contains(state.Plan, "recovery path") {
		t.Fatalf("Grok whole-review proposal was not applied: %+v", state)
	}
	assertFileContains(t, fake.stdinPath, "Add the recovery path.")
	assertFileContains(t, fake.stdinPath, "Add the recovery path before approval.")

	finalize(t, review.url, digest("Grok iteration approved", nil, nil))
	waitSuccess(t, review)
}

func TestUnpromotedSideAnswerStaysOutOfHandoff(t *testing.T) {
	fake := installFakeCodex(t, "Temporary answer.")
	review := startReview(t, realisticPlan("Unpromote side answer"), map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "current-thread",
	})
	thread := createThread(t, review.url, 5, 5, "Ask then unpromote.")

	answer := askSideQuestion(t, review.url, thread.ID, "Should this stay?", "1. CLI")
	postJSON(t, review.url+"/api/side-answers/"+answer.ID+"/promote", `{}`, http.StatusOK)
	postJSON(t, review.url+"/api/side-answers/"+answer.ID+"/unpromote", `{}`, http.StatusOK)

	draft := draftDigest(t, review.url)
	if containsString(draft.PromotedSideAnswers, "Temporary answer.") {
		t.Fatalf("expected unpromoted answer to be excluded, got %+v", draft.PromotedSideAnswers)
	}
	finalize(t, review.url, draft)
	waitSuccess(t, review)
	if strings.Contains(review.stdout.String(), "Temporary answer.") {
		t.Fatalf("expected handoff to exclude unpromoted answer, got %q", review.stdout.String())
	}
}

func TestSideQuestionUnavailableWithoutOriginalThreadContext(t *testing.T) {
	fake := installFakeCodex(t, "This should not run.")
	review := startReview(t, realisticPlan("No original thread"), map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "",
	})
	thread := createThread(t, review.url, 5, 5, "Ask without original context.")

	payload := fmt.Sprintf(`{"threadID":%q,"question":"Why?","planExcerpt":"1. CLI"}`, thread.ID)
	postJSON(t, review.url+"/api/side-questions", payload, http.StatusServiceUnavailable)
	state := getState(t, review.url)
	if len(state.SideAnswers) != 0 {
		t.Fatalf("expected no side answers without original thread context, got %+v", state.SideAnswers)
	}
	if _, err := os.Stat(fake.stdinPath); !os.IsNotExist(err) {
		t.Fatalf("expected fake codex not to run without original thread context, stat err %v", err)
	}

	finalize(t, review.url, digest("No-context review approved", nil, nil))
	waitSuccess(t, review)
}

func TestSideQuestionTimeoutBoundsSlowAppServer(t *testing.T) {
	fake := installSlowFakeCodex(t)
	review := startReviewWithArgs(t, realisticPlan("Side question timeout"), []string{"--side-question-timeout", "50ms"}, map[string]string{
		"PATH":            fake.pathEnv,
		"CODEX_THREAD_ID": "current-thread",
	})
	thread := createThread(t, review.url, 5, 5, "Ask slow app-server.")

	payload := fmt.Sprintf(`{"threadID":%q,"question":"Slow?","planExcerpt":"1. CLI"}`, thread.ID)
	postJSON(t, review.url+"/api/side-questions", payload, http.StatusServiceUnavailable)
	state := getState(t, review.url)
	if len(state.SideAnswers) != 0 {
		t.Fatalf("expected no side answers after timeout, got %+v", state.SideAnswers)
	}

	finalize(t, review.url, digest("Timeout approved", nil, nil))
	waitSuccess(t, review)
}

func TestSaveToFileWritesPlanContent(t *testing.T) {
	dir := t.TempDir()
	savedPlanPath := filepath.Join(dir, "saved-plan.md")
	plan := realisticPlan("Save to file")
	review := startReviewWithArgs(t, plan, []string{"--no-browser", "--save-to-file", savedPlanPath}, nil)

	finalize(t, review.url, digest("Handoff file approved", nil, nil))
	waitSuccess(t, review)
	fileOutput, err := os.ReadFile(savedPlanPath)
	if err != nil {
		t.Fatalf("read saved plan: %v", err)
	}
	if got := string(fileOutput); got != plan {
		t.Fatalf("saved plan = %q, want %q", got, plan)
	}
	if strings.Contains(string(fileOutput), "Continue the user's approved plan work.") {
		t.Fatalf("saved plan must not contain the handoff prompt")
	}
}

func TestReviewUIServesThreadFilter(t *testing.T) {
	review := startReview(t, realisticPlan("Thread filter"), nil)

	res, err := http.Get(review.url)
	if err != nil {
		t.Fatalf("get review UI: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read review UI: %v", err)
	}
	// The UI is a React SPA, so the shell only contains the mount point and
	// the bundled JS/CSS. The thread-filter input is rendered client-side.
	if !bytes.Contains(body, []byte(`id="root"`)) {
		t.Fatalf("expected React mount point in UI, got %s", body)
	}
	if !bytes.Contains(body, []byte(`/assets/app.js`)) {
		t.Fatalf("expected bundled app.js reference in UI, got %s", body)
	}
	bundle, err := http.Get(review.url + "/assets/app.js")
	if err != nil {
		t.Fatalf("get app.js: %v", err)
	}
	defer bundle.Body.Close()
	bundleBody, err := io.ReadAll(bundle.Body)
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	if !bytes.Contains(bundleBody, []byte(`thread-filter`)) {
		t.Fatalf("expected thread-filter id in app bundle")
	}
	finalize(t, review.url, digest("Filter UI served", nil, nil))
	waitSuccess(t, review)
}

func TestLiveAppServerSideQuestionWhenAvailable(t *testing.T) {
	if os.Getenv("PLANMAXX_LIVE_APP_SERVER_E2E") != "1" {
		t.Skip("set PLANMAXX_LIVE_APP_SERVER_E2E=1 to run live app-server scenario")
	}
	threadID := os.Getenv("CODEX_THREAD_ID")
	if threadID == "" {
		t.Skip("CODEX_THREAD_ID is required for live app-server scenario")
	}

	review := startReview(t, realisticPlan("Live app-server"), map[string]string{"CODEX_THREAD_ID": threadID})
	thread := createThread(t, review.url, 5, 5, "Ask live app-server.")
	answer := askSideQuestion(t, review.url, thread.ID, "Answer exactly PLANMAXX_E2E_OK and nothing else.", "")
	assertContains(t, answer.Answer, "PLANMAXX_E2E_OK")
	postJSON(t, review.url+"/api/side-answers/"+answer.ID+"/promote", `{}`, http.StatusOK)
	finalize(t, review.url, draftDigest(t, review.url))
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "PLANMAXX_E2E_OK")
}

func TestMissingThreadSideQuestionDoesNotCreateOrphanAnswer(t *testing.T) {
	review := startReview(t, realisticPlan("Missing thread"), nil)

	postJSON(t, review.url+"/api/side-questions", `{"threadID":"thread-missing","question":"Why?","planExcerpt":"# Plan"}`, http.StatusNotFound)
	state := getState(t, review.url)
	if len(state.SideAnswers) != 0 {
		t.Fatalf("expected no orphan side answers, got %+v", state.SideAnswers)
	}

	finalize(t, review.url, digest("Missing thread guarded", nil, nil))
	waitSuccess(t, review)
}

func TestDeleteMistakenThreadExcludesItFromDigest(t *testing.T) {
	review := startReview(t, realisticPlan("Delete mistaken thread"), nil)
	mistake := createThread(t, review.url, 3, 3, "Remove this mistaken comment.")
	keep := createThread(t, review.url, 4, 4, "Keep this decision.")

	postJSON(t, review.url+"/api/threads/"+mistake.ID+"/delete", `{}`, http.StatusOK)
	draft := draftDigest(t, review.url)
	if containsString(draft.ReviewerDecisions, "Remove this mistaken comment.") {
		t.Fatalf("expected deleted thread to be excluded, got %+v", draft.ReviewerDecisions)
	}
	if !containsString(draft.ReviewerDecisions, "Keep this decision.") {
		t.Fatalf("expected kept thread in digest, got %+v", draft.ReviewerDecisions)
	}
	state := getState(t, review.url)
	if len(state.Threads) != 1 || state.Threads[0].ID != keep.ID {
		t.Fatalf("expected only kept thread, got %+v", state.Threads)
	}

	finalize(t, review.url, draft)
	waitSuccess(t, review)
}

func TestMalformedJSONReturnsJSONErrors(t *testing.T) {
	review := startReview(t, realisticPlan("Malformed JSON"), nil)

	res := postRaw(t, review.url+"/api/threads", `{"anchor":{"startLine":1`, "application/json")
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 400, got %d: %s", res.StatusCode, body)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON error content type, got %q", res.Header.Get("Content-Type"))
	}

	finalize(t, review.url, digest("Malformed JSON guarded", nil, nil))
	waitSuccess(t, review)
}

func TestPrintedReviewURLStillAllowsFinalize(t *testing.T) {
	review := startReviewWithoutFakeOpen(t, realisticPlan("Browser failure"), nil)

	assertContains(t, review.stderr.String(), "PlanMaxx review URL: "+review.url)
	finalize(t, review.url, digest("Browser failure tolerated", nil, nil))
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "Browser failure tolerated")
}

func TestNoBrowserFlagSkipsOpenerAndFinalizes(t *testing.T) {
	noOpen := installNoOpenPath(t)
	review := startReviewWithArgs(t, realisticPlan("No browser"), []string{"--no-browser"}, map[string]string{"PATH": noOpen.pathEnv})

	if strings.Contains(review.stderr.String(), "Open "+review.url+" in your browser") {
		t.Fatalf("expected no browser fallback message with --no-browser, got %q", review.stderr.String())
	}
	finalize(t, review.url, digest("No browser approved", nil, nil))
	waitSuccess(t, review)
	assertContains(t, review.stdout.String(), "No browser approved")
}

func TestFixedPortFlagUsesRequestedPort(t *testing.T) {
	port := freeTCPPort(t)
	review := startReviewWithArgs(t, realisticPlan("Fixed port"), []string{"--no-browser", "--port", fmt.Sprint(port)}, nil)

	if !strings.HasSuffix(review.url, fmt.Sprintf(":%d", port)) {
		t.Fatalf("expected URL to use port %d, got %s", port, review.url)
	}
	finalize(t, review.url, digest("Fixed port approved", nil, nil))
	waitSuccess(t, review)
}

func TestParallelReviewsUseIsolatedPorts(t *testing.T) {
	first := startReview(t, realisticPlan("Parallel A"), nil)
	second := startReview(t, realisticPlan("Parallel B"), nil)
	if first.url == second.url {
		t.Fatalf("expected different review URLs, got %q", first.url)
	}

	finalize(t, first.url, digest("First approved", nil, nil))
	finalize(t, second.url, digest("Second approved", nil, nil))
	waitSuccess(t, first)
	waitSuccess(t, second)
	assertContains(t, first.stdout.String(), "First approved")
	assertContains(t, second.stdout.String(), "Second approved")
}

func startReview(t *testing.T, plan string, env map[string]string) *reviewProcess {
	t.Helper()
	return startReviewWithArgs(t, plan, nil, env)
}

func startReviewWithArgs(t *testing.T, plan string, args []string, env map[string]string) *reviewProcess {
	t.Helper()
	return startReviewFull(t, plan, args, env, true)
}

// startReviewWithoutFakeOpen runs the review with no fake `open` prepended to
// PATH. Most tests should use startReview, which makes `open` succeed silently
// into a no-op fake instead of launching a real browser tab.
func startReviewWithoutFakeOpen(t *testing.T, plan string, env map[string]string) *reviewProcess {
	t.Helper()
	return startReviewFull(t, plan, nil, env, false)
}

func startReviewFull(t *testing.T, plan string, args []string, env map[string]string, installFakeOpenInPath bool) *reviewProcess {
	t.Helper()

	root := repoRoot(t)
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	commandArgs := []string{"run", "./cmd/planmaxx", "review"}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, planPath)
	cmd := exec.CommandContext(ctx, "go", commandArgs...)
	cmd.Dir = root

	basePath := installGitOnlyPath(t).pathEnv
	if installFakeOpenInPath {
		fakeOpen := installFakeOpen(t, 0)
		basePath = fakeOpen.pathEnv
		cmd.Env = mergeEnv(map[string]string{
			"PATH":                       basePath,
			"CODEX_THREAD_ID":            "",
			"CLAUDE_CODE_SESSION_ID":     "",
			"GROK_SESSION_ID":            "",
			"PLANMAXX_AGENT":             "",
			"PLANMAXX_CLAUDE_SESSION_ID": "",
		}, env)
		// Guard against env overrides (e.g. tests that supply a fake-codex
		// PATH) silently displacing the fake-open dir and letting the real
		// macOS `open` command launch a real browser tab. Re-prepend the
		// fake-open dir to whatever PATH the merge produced.
		cmd.Env = prependPath(cmd.Env, fakeOpen.dir)
	} else {
		// Tests that exercise the "open browser failed" code path want
		// LookPath("open") to fail. Keep PATH empty and ignore any caller
		// override that might re-introduce a system PATH with a real `open`.
		filtered := make(map[string]string, len(env))
		for k, v := range env {
			if k != "PATH" {
				filtered[k] = v
			}
		}
		cmd.Env = mergeEnv(map[string]string{
			"PATH":                       basePath,
			"CODEX_THREAD_ID":            "",
			"CLAUDE_CODE_SESSION_ID":     "",
			"GROK_SESSION_ID":            "",
			"PLANMAXX_AGENT":             "",
			"PLANMAXX_CLAUDE_SESSION_ID": "",
		}, filtered)
	}

	stdout := &lockedBuffer{}
	stderr := &lockedBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start review command: %v", err)
	}
	review := &reviewProcess{
		planPath: planPath,
		stdout:   stdout,
		stderr:   stderr,
		done:     make(chan error, 1),
		cancel:   cancel,
	}
	go func() {
		review.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-review.done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})
	review.url = waitForReviewURL(t, stderr)
	return review
}

func waitForReviewURL(t *testing.T, stderr *lockedBuffer) string {
	t.Helper()

	deadline := time.After(15 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for review URL in stderr:\n%s", stderr.String())
		case <-ticker.C:
			for _, line := range strings.Split(stderr.String(), "\n") {
				if url, ok := strings.CutPrefix(line, "PlanMaxx review URL: "); ok {
					return url
				}
			}
		}
	}
}

func waitSuccess(t *testing.T, review *reviewProcess) {
	t.Helper()

	if err := waitDone(t, review); err != nil {
		t.Fatalf("expected review command success, got %v\nstderr:\n%s\nstdout:\n%s", err, review.stderr.String(), review.stdout.String())
	}
}

func waitDone(t *testing.T, review *reviewProcess) error {
	t.Helper()

	select {
	case err := <-review.done:
		review.cancel()
		return err
	case <-time.After(90 * time.Second):
		review.cancel()
		t.Fatal("timed out waiting for review command")
		return nil
	}
}

type threadResponse struct {
	ID string `json:"id"`
}

func createThread(t *testing.T, url string, startLine int, endLine int, body string) threadResponse {
	t.Helper()

	payload := fmt.Sprintf(`{"anchor":{"startLine":%d,"endLine":%d},"body":%q}`, startLine, endLine, body)
	var thread threadResponse
	postJSONInto(t, url+"/api/threads", payload, http.StatusOK, &thread)
	if thread.ID == "" {
		t.Fatal("expected created thread id")
	}
	return thread
}

type sideAnswerResponse struct {
	ID       string `json:"id"`
	Answer   string `json:"answer"`
	Promoted bool   `json:"promoted"`
}

func askSideQuestion(t *testing.T, url string, threadID string, question string, excerpt string) sideAnswerResponse {
	t.Helper()

	payload := fmt.Sprintf(`{"threadID":%q,"question":%q,"planExcerpt":%q}`, threadID, question, excerpt)
	var answer sideAnswerResponse
	postJSONInto(t, url+"/api/side-questions", payload, http.StatusOK, &answer)
	return answer
}

type stateResponse struct {
	Plan              string `json:"plan"`
	CurrentRevisionID string `json:"currentRevisionId"`
	Capabilities      struct {
		CanIterate bool `json:"canIterate"`
	} `json:"capabilities"`
	Agent struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		ContextMode string `json:"contextMode"`
		Available   bool   `json:"available"`
	} `json:"agent"`
	Threads []struct {
		ID     string `json:"id"`
		Anchor struct {
			StartLine int `json:"startLine"`
			EndLine   int `json:"endLine"`
		} `json:"anchor"`
		Position struct {
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"position"`
		Capabilities struct {
			CanEdit    bool `json:"canEdit"`
			CanReply   bool `json:"canReply"`
			CanAsk     bool `json:"canAsk"`
			CanIterate bool `json:"canIterate"`
		} `json:"capabilities"`
	} `json:"threads"`
	SideAnswers []sideAnswerResponse `json:"sideAnswers"`
}

type healthResponse struct {
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
}

func getHealth(t *testing.T, url string) healthResponse {
	t.Helper()

	res, err := http.Get(url + "/api/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected health 200, got %d: %s", res.StatusCode, body)
	}
	var health healthResponse
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	return health
}

func getState(t *testing.T, url string) stateResponse {
	t.Helper()

	res, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected state 200, got %d: %s", res.StatusCode, body)
	}
	var state stateResponse
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return state
}

type digestBody struct {
	Summary             string   `json:"summary"`
	ReviewerDecisions   []string `json:"reviewerDecisions"`
	PromotedSideAnswers []string `json:"promotedSideAnswers"`
}

func digest(summary string, decisions []string, answers []string) digestBody {
	if decisions == nil {
		decisions = []string{}
	}
	if answers == nil {
		answers = []string{}
	}
	return digestBody{Summary: summary, ReviewerDecisions: decisions, PromotedSideAnswers: answers}
}

func draftDigest(t *testing.T, url string) digestBody {
	t.Helper()

	var draft digestBody
	postJSONInto(t, url+"/api/digest/draft", `{}`, http.StatusOK, &draft)
	return draft
}

func finalize(t *testing.T, url string, digest digestBody) {
	t.Helper()

	payload, err := json.Marshal(digest)
	if err != nil {
		t.Fatalf("marshal digest: %v", err)
	}
	postJSON(t, url+"/api/finalize", string(payload), http.StatusOK)
}

func postJSON(t *testing.T, url string, body string, wantStatus int) {
	t.Helper()

	res := postRaw(t, url, body, "application/json")
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status %d from %s, got %d: %s", wantStatus, url, res.StatusCode, data)
	}
}

func postJSONInto(t *testing.T, url string, body string, wantStatus int, out any) {
	t.Helper()

	res := postRaw(t, url, body, "application/json")
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status %d from %s, got %d: %s", wantStatus, url, res.StatusCode, data)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decode response from %s: %v", url, err)
	}
}

func postRaw(t *testing.T, url string, body string, contentType string) *http.Response {
	t.Helper()

	res, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return res
}

type fakeCommand struct {
	dir       string
	pathEnv   string
	stdinPath string
}

func installFakeOpen(t *testing.T, exitCode int) fakeCommand {
	t.Helper()

	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	for _, name := range []string{"open", "xdg-open", "rundll32"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	return fakeCommand{dir: dir, pathEnv: dir + string(os.PathListSeparator) + os.Getenv("PATH")}
}

func installNoOpenPath(t *testing.T) fakeCommand {
	t.Helper()
	return installGitOnlyPath(t)
}

func installGitOnlyPath(t *testing.T) fakeCommand {
	t.Helper()
	dir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find git for review storage: %v", err)
	}
	target := filepath.Join(dir, filepath.Base(gitPath))
	if err := os.Symlink(gitPath, target); err != nil {
		source, openErr := os.Open(gitPath)
		if openErr != nil {
			t.Fatalf("open git executable: %v", openErr)
		}
		defer source.Close()
		destination, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
		if createErr != nil {
			t.Fatalf("copy git executable: %v", createErr)
		}
		if _, copyErr := io.Copy(destination, source); copyErr != nil {
			_ = destination.Close()
			t.Fatalf("copy git executable: %v", copyErr)
		}
		if closeErr := destination.Close(); closeErr != nil {
			t.Fatalf("close copied git executable: %v", closeErr)
		}
	}
	return fakeCommand{dir: dir, pathEnv: dir}
}

func installFakeCodex(t *testing.T, answer string) fakeCommand {
	t.Helper()
	return installFakeCodexAppServer(t, answer, false)
}

func installSlowFakeCodex(t *testing.T) fakeCommand {
	t.Helper()
	return installFakeCodexAppServer(t, "slow answer", true)
}

func installFakeCodexAppServer(t *testing.T, answer string, slow bool) fakeCommand {
	t.Helper()
	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")
	pythonPath := filepath.Join(dir, "fake-codex.py")
	python := `#!/usr/bin/env python3
import json
import os
import re
import sys
import time

transcript_path = os.environ["PLANMAXX_FAKE_CODEX_TRANSCRIPT"]
answer = os.environ["PLANMAXX_FAKE_CODEX_ANSWER"]
slow = os.environ.get("PLANMAXX_FAKE_CODEX_SLOW") == "1"

def record(line):
    with open(transcript_path, "a", encoding="utf-8") as f:
        f.write(line)
        if not line.endswith("\n"):
            f.write("\n")

def send(message):
    print(json.dumps(message), flush=True)

if sys.argv[1:] == ["app-server", "--listen", "stdio://"]:
    print("fake app-server stderr", file=sys.stderr, flush=True)
    for line in sys.stdin:
        record(line)
        request = json.loads(line)
        method = request.get("method")
        request_id = request.get("id")
        if method == "initialize":
            send({"id": request_id, "result": {"userAgent": "Codex", "codexHome": "/tmp/codex"}})
        elif method == "initialized":
            continue
        elif method == "thread/read":
            send({"id": request_id, "result": {"thread": {"id": "current-thread", "status": {"type": "idle"}}}})
        elif method == "thread/fork":
            send({"id": request_id, "result": {"thread": {"id": "fork-1", "forkedFromId": "current-thread", "ephemeral": True, "cwd": "/repo", "status": {"type": "idle"}}, "cwd": "/repo", "approvalPolicy": "never", "sandbox": {"type": "readOnly"}}})
        elif method == "turn/start":
            if slow:
                time.sleep(2)
            send({"id": request_id, "result": {"turn": {"id": "turn-1", "status": "inProgress"}}})
            reply = answer
            if "{{REVISION}}" in reply:
                import re
                prompt = request["params"]["input"][0]["text"]
                match = re.search(r'<planmaxx_iteration\b[^>]*\brevision="([^"]+)"', prompt)
                if not match:
                    print("missing iteration revision", file=sys.stderr, flush=True)
                    sys.exit(2)
                reply = reply.replace("{{REVISION}}", match.group(1))
            send({"method": "item/completed", "params": {"threadId": "fork-1", "turnId": "turn-1", "item": {"type": "agentMessage", "text": reply}}})
            send({"method": "turn/completed", "params": {"threadId": "fork-1", "turn": {"id": "turn-1", "status": "completed"}}})
        else:
            print(f"unexpected method: {method}", file=sys.stderr, flush=True)
            sys.exit(2)
    sys.exit(0)

print("unexpected fake codex args: " + " ".join(sys.argv[1:]), file=sys.stderr, flush=True)
sys.exit(2)
`
	if err := os.WriteFile(pythonPath, []byte(python), 0o700); err != nil {
		t.Fatalf("write fake codex app-server: %v", err)
	}
	slowValue := "0"
	if slow {
		slowValue = "1"
	}
	script := fmt.Sprintf(`#!/bin/sh
PLANMAXX_FAKE_CODEX_ANSWER=%q PLANMAXX_FAKE_CODEX_TRANSCRIPT=%q PLANMAXX_FAKE_CODEX_SLOW=%q exec python3 %q "$@"
`, answer, stdinPath, slowValue, pythonPath)
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake codex wrapper: %v", err)
	}
	return fakeCommand{pathEnv: dir + string(os.PathListSeparator) + os.Getenv("PATH"), stdinPath: stdinPath}
}

func installFakeClaude(t *testing.T, answer string) fakeCommand {
	t.Helper()
	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")
	pythonPath := filepath.Join(dir, "fake-claude.py")
	python := `#!/usr/bin/env python3
import json
import os
import sys

if "--version" in sys.argv[1:]:
    print("2.1.215 (Claude Code)")
    sys.exit(0)

if "--help" in sys.argv[1:]:
    print("--fork-session --safe-mode --no-session-persistence --output-format --tools --permission-mode")
    sys.exit(0)

prompt = sys.stdin.read()
with open(os.environ["PLANMAXX_FAKE_CLAUDE_TRANSCRIPT"], "w", encoding="utf-8") as transcript:
    json.dump({"args": sys.argv[1:], "prompt": prompt}, transcript)

print(json.dumps({
    "type": "result",
    "subtype": "success",
    "is_error": False,
    "result": os.environ["PLANMAXX_FAKE_CLAUDE_ANSWER"],
    "session_id": "forked-claude-session"
}))
`
	if err := os.WriteFile(pythonPath, []byte(python), 0o700); err != nil {
		t.Fatalf("write fake Claude Code process: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
PLANMAXX_FAKE_CLAUDE_ANSWER=%q PLANMAXX_FAKE_CLAUDE_TRANSCRIPT=%q exec python3 %q "$@"
`, answer, stdinPath, pythonPath)
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Claude Code wrapper: %v", err)
	}
	return fakeCommand{pathEnv: dir + string(os.PathListSeparator) + os.Getenv("PATH"), stdinPath: stdinPath}
}

func installFakeGrok(t *testing.T, answer string) fakeCommand {
	t.Helper()
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	pythonPath := filepath.Join(dir, "fake-grok.py")
	python := `#!/usr/bin/env python3
import json
import os
import re
import shutil
import sys
import urllib.parse

args = sys.argv[1:]
if "--version" in args:
    print("grok 0.2.114 (test) [stable]")
    sys.exit(0)

if "--help" in args:
    print("--cwd --prompt-file --resume --fork-session --session-id --output-format --tools --allow --deny --permission-mode --sandbox --no-subagents --no-memory --disable-web-search --no-plan --max-turns --verbatim")
    sys.exit(0)

if args == ["agent", "--no-leader", "stdio"]:
    for line in sys.stdin:
        request = json.loads(line)
        request_id = request.get("id")
        method = request.get("method")
        if method == "initialize":
            print(json.dumps({
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {
                    "protocolVersion": 1,
                    "agentCapabilities": {"loadSession": True},
                    "_meta": {"grokShell": True}
                }
            }), flush=True)
        elif method == "_x.ai/session/fork":
            params = request["params"]
            source_id = params["sourceSessionId"]
            child_id = params["newSessionId"]
            new_cwd = params["newCwd"]
            source_dir = None
            sessions_root = os.path.join(os.environ["GROK_HOME"], "sessions")
            for namespace in os.listdir(sessions_root):
                candidate = os.path.join(sessions_root, namespace, source_id)
                if os.path.isdir(candidate):
                    source_dir = candidate
                    break
            if source_dir is None:
                print(json.dumps({"jsonrpc":"2.0","id":request_id,"error":{"code":-32002,"message":"source not found"}}), flush=True)
                continue
            child_dir = os.path.join(
                sessions_root,
                urllib.parse.quote(new_cwd, safe=""),
                child_id
            )
            os.makedirs(child_dir, exist_ok=True)
            with open(os.path.join(source_dir, "summary.json"), "r", encoding="utf-8") as source_summary_file:
                summary = json.load(source_summary_file)
            summary["info"]["id"] = child_id
            summary["info"]["cwd"] = new_cwd
            with open(os.path.join(child_dir, "summary.json"), "w", encoding="utf-8") as child_summary_file:
                json.dump(summary, child_summary_file)
            source_chat = os.path.join(source_dir, "chat_history.jsonl")
            if os.path.exists(source_chat):
                shutil.copyfile(source_chat, os.path.join(child_dir, "chat_history.jsonl"))
            with open(os.environ["PLANMAXX_FAKE_GROK_TRANSCRIPT"], "a", encoding="utf-8") as transcript:
                transcript.write(json.dumps({"command":"fork","params":params}) + "\n")
            print(json.dumps({
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {
                    "newSessionId": child_id,
                    "chatMessagesCopied": 1,
                    "updatesCopied": 0,
                    "planStateCopied": False,
                    "newCwd": new_cwd,
                    "parentSessionId": source_id
                }
            }), flush=True)
        else:
            print(json.dumps({"jsonrpc":"2.0","id":request_id,"error":{"code":-32601,"message":"unsupported"}}), flush=True)
    sys.exit(0)

if "inspect" in args and "--json" in args:
    print(json.dumps({
        "projectInstructions": [],
        "hooks": [],
        "skills": [],
        "plugins": [],
        "marketplaces": [],
        "mcpServers": [],
        "lspServers": [],
        "agents": [{"source": {"type": "builtin"}}],
        "permissions": {"managedSettingsActive": False},
        "configSources": {"layers": []}
    }))
    sys.exit(0)

if len(args) >= 3 and args[0:2] == ["sessions", "delete"]:
    sessions_root = os.path.join(os.environ["GROK_HOME"], "sessions")
    for namespace in os.listdir(sessions_root):
        candidate = os.path.join(sessions_root, namespace, args[2])
        if os.path.isdir(candidate):
            shutil.rmtree(candidate)
    with open(os.environ["PLANMAXX_FAKE_GROK_TRANSCRIPT"], "a", encoding="utf-8") as transcript:
        transcript.write(json.dumps({"command": "delete", "session_id": args[2]}) + "\n")
    sys.exit(0)

prompt_path = args[args.index("--prompt-file") + 1]
fork_id = args[args.index("--resume") + 1]
with open(prompt_path, "r", encoding="utf-8") as prompt_file:
    prompt = prompt_file.read()
answer = os.environ["PLANMAXX_FAKE_GROK_ANSWER"]
if "{{REVISION}}" in answer:
    match = re.search(r'<planmaxx_iteration\b[^>]*\brevision="([^"]+)"', prompt)
    if not match:
        print("missing iteration revision", file=sys.stderr)
        sys.exit(2)
    answer = answer.replace("{{REVISION}}", match.group(1))
with open(os.environ["PLANMAXX_FAKE_GROK_TRANSCRIPT"], "a", encoding="utf-8") as transcript:
    transcript.write(json.dumps({
        "command": "prompt",
        "args": args,
        "prompt": prompt,
        "auto_update_disabled": os.environ.get("GROK_DISABLE_AUTOUPDATER") == "1",
        "isolated_home": "planmaxx-grok-isolation-" in os.environ.get("HOME", ""),
        "isolated_grok_home": "planmaxx-grok-isolation-" in os.environ.get("GROK_HOME", ""),
        "isolated_claude_config": "planmaxx-grok-isolation-" in os.environ.get("CLAUDE_CONFIG_DIR", "")
    }) + "\n")

print(json.dumps({
    "text": answer,
    "stopReason": "EndTurn",
    "sessionId": fork_id
}))
`
	if err := os.WriteFile(pythonPath, []byte(python), 0o700); err != nil {
		t.Fatalf("write fake Grok Build process: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
PLANMAXX_FAKE_GROK_ANSWER=%q PLANMAXX_FAKE_GROK_TRANSCRIPT=%q exec python3 %q "$@"
`, answer, transcriptPath, pythonPath)
	if err := os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Grok Build wrapper: %v", err)
	}
	return fakeCommand{pathEnv: dir + string(os.PathListSeparator) + os.Getenv("PATH"), stdinPath: transcriptPath}
}

func installFakeGrokSession(t *testing.T, sessionID string, context string) string {
	t.Helper()
	grokHome := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "service.go"), []byte("package service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(grokHome, "sessions", "%2Frepo", sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	summary := fmt.Sprintf(`{"info":{"id":%q,"cwd":%q},"current_model_id":"grok-4.5"}`, sessionID, workspace)
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(summary), 0o600); err != nil {
		t.Fatal(err)
	}
	history, err := json.Marshal(map[string]string{"role": "user", "content": context})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "chat_history.jsonl"), append(history, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grokHome, "auth.json"), []byte(`{"test":"auth"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return grokHome
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// prependPath returns a copy of env with `dir` prepended to PATH, so a fake
// binary placed in dir always wins over anything a test override may have
// put in PATH. Without this, a test that sets PATH (e.g. to expose a fake
// `codex`) would displace the fake-open dir and the real macOS `open`
// command would launch a real browser tab.
func prependPath(env []string, dir string) []string {
	out := make([]string, len(env))
	saw := false
	for i, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == "PATH" {
			out[i] = "PATH=" + dir + string(os.PathListSeparator) + value
			saw = true
			continue
		}
		out[i] = item
	}
	if !saw {
		out = append(out, "PATH="+dir)
	}
	return out
}

func mergeEnv(base map[string]string, override map[string]string) []string {
	env := os.Environ()
	values := make(map[string]string)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range base {
		values[key] = value
	}
	for key, value := range override {
		values[key] = value
	}

	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("could not find repo root")
		}
		wd = parent
	}
}

func realisticPlan(title string) string {
	return fmt.Sprintf(`# %s

## Context

The team is preparing a staged rollout for PlanMaxx in a Codex workspace with
reviewers who need a browser-based approval loop.

## Steps

1. Verify the CLI contract and localhost server behavior.
2. Review anchored feedback on the generated plan.
3. Ask side questions only when safe context is available.
4. Promote useful side-question answers into the digest.
5. Finalize the handoff for Codex implementation.
`, title)
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()

	if !strings.Contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	assertContains(t, string(data), want)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
