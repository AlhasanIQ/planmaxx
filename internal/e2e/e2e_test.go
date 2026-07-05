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
	url    string
	stdout *lockedBuffer
	stderr *lockedBuffer
	done   chan error
	cancel context.CancelFunc
}

func TestFinalizeSimplePlanWritesHandoff(t *testing.T) {
	review := startReview(t, realisticPlan("Feature rollout"), nil)
	health := getHealth(t, review.url)
	if health.Status != "active" || health.SessionID != "session-1" {
		t.Fatalf("unexpected health response %+v", health)
	}

	finalize(t, review.url, digest("Approved simple review", nil, nil))
	waitSuccess(t, review)

	output := review.stdout.String()
	assertContains(t, output, "Continue from this approved PlanMaxx review.")
	assertContains(t, output, "Approved simple review")
}

func TestCancelReviewExitsNonZeroWithoutHandoff(t *testing.T) {
	review := startReview(t, realisticPlan("Cancel path"), nil)

	postJSON(t, review.url+"/api/cancel", `{}`, http.StatusOK)
	err := waitDone(t, review)
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("expected non-zero cancel exit, got %v", err)
	}
	if strings.Contains(review.stdout.String(), "Continue from this approved PlanMaxx review.") {
		t.Fatalf("expected no handoff on cancel, got %q", review.stdout.String())
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

func TestHandoffOutMirrorsStdout(t *testing.T) {
	dir := t.TempDir()
	handoffPath := filepath.Join(dir, "handoff.md")
	review := startReviewWithArgs(t, realisticPlan("Handoff out"), []string{"--no-browser", "--handoff-out", handoffPath}, nil)

	finalize(t, review.url, digest("Handoff file approved", nil, nil))
	waitSuccess(t, review)
	fileOutput, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("read handoff file: %v", err)
	}
	if string(fileOutput) != review.stdout.String() {
		t.Fatalf("expected file output to mirror stdout")
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

	basePath := t.TempDir() // empty by default — nothing resolves on it
	if installFakeOpenInPath {
		fakeOpen := installFakeOpen(t, 0)
		basePath = fakeOpen.pathEnv
		cmd.Env = mergeEnv(map[string]string{
			"PATH":            basePath,
			"CODEX_THREAD_ID": "",
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
			"PATH":            basePath,
			"CODEX_THREAD_ID": "",
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
		stdout: stdout,
		stderr: stderr,
		done:   make(chan error, 1),
		cancel: cancel,
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

	return fakeCommand{pathEnv: t.TempDir()}
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
            send({"id": request_id, "result": {"thread": {"id": "fork-1", "forkedFromId": "current-thread", "ephemeral": True, "cwd": "/repo", "status": {"type": "idle"}}, "cwd": "/repo"}})
        elif method == "turn/start":
            if slow:
                time.sleep(2)
            send({"id": request_id, "result": {"turn": {"id": "turn-1", "status": "inProgress"}}})
            send({"method": "item/completed", "params": {"threadId": "fork-1", "turnId": "turn-1", "item": {"type": "agentMessage", "text": answer}}})
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
