package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestMain(m *testing.M) {
	openBrowser = func(string) error {
		return nil
	}
	root, err := os.MkdirTemp("", "planmaxx-cli-test-*")
	if err != nil {
		panic(err)
	}
	userCacheDir = func() (string, error) { return filepath.Join(root, "cache"), nil }
	userDataDir = func() (string, error) { return filepath.Join(root, "data"), nil }
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func TestRootCommandSilencesErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)

	if !cmd.SilenceErrors {
		t.Fatal("expected root command to silence errors")
	}
}

func TestReviewRequiresPlanFile(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing plan file error")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("expected Cobra arg count error, got %q", err.Error())
	}
}

func TestReviewAcceptsHostFlag(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--host", "127.0.0.1", "plan.md"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected plan file read error")
	}
	if !strings.Contains(err.Error(), "read plan file") {
		t.Fatalf("expected file-read error, got %q", err.Error())
	}
}

func TestReviewServesPlanAndWritesHandoffOnFinalize(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	openedURLCh := make(chan string, 1)
	oldOpenBrowser := openBrowser
	openBrowser = func(url string) error {
		openedURLCh <- url
		return errors.New("browser unavailable")
	}
	t.Cleanup(func() {
		openBrowser = oldOpenBrowser
	})

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	var openedURL string
	select {
	case openedURL = <-openedURLCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for browser opener")
	}
	if openedURL != url {
		t.Fatalf("expected browser opener to receive %q, got %q", url, openedURL)
	}
	if !strings.Contains(stderr.String(), "Open "+url+" in your browser: browser unavailable") {
		t.Fatalf("expected browser fallback message, got %q", stderr.String())
	}

	res, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get review state: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected state status 200, got %d", res.StatusCode)
	}
	var state struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatalf("decode review state: %v", err)
	}
	if state.Plan != "# Test plan\n" {
		t.Fatalf("expected served plan body, got %q", state.Plan)
	}

	finalizeBody := strings.NewReader(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", finalizeBody)
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete after finalize, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}

	output := stdout.String()
	if !strings.Contains(output, "Continue from this approved PlanMaxx review.") {
		t.Fatalf("expected handoff output, got %q", output)
	}
	if !strings.Contains(output, `"summary": "Approved"`) {
		t.Fatalf("expected approved digest summary, got %q", output)
	}
}

func TestReviewWritesRejectionHandoffOnReject(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	handoffPath := t.TempDir() + "/handoff.md"

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", "--handoff-out", handoffPath, path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	rejectBody := strings.NewReader(`{"summary":"Rejected because migration order is unsafe","reviewerDecisions":["Revise before implementation"],"promotedSideAnswers":[]}`)
	rejectRes, err := http.Post(url+"/api/reject", "application/json", rejectBody)
	if err != nil {
		t.Fatalf("reject review: %v", err)
	}
	defer rejectRes.Body.Close()
	if rejectRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rejectRes.Body)
		t.Fatalf("expected reject status 200, got %d: %s", rejectRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to return rejection handoff, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}

	output := stdout.String()
	for _, want := range []string{
		"PlanMaxx review rejected.",
		"The user rejected this plan with comments.",
		"Address the comments in the rejection digest, then reiterate the plan until the user is satisfied.",
		`"summary": "Rejected because migration order is unsafe"`,
		"Revise before implementation",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rejection handoff to contain %q, got %q", want, output)
		}
	}
	if strings.Contains(output, "Continue from this approved PlanMaxx review.") {
		t.Fatalf("expected rejection handoff not to approve continuation, got %q", output)
	}
	if strings.Contains(output, "Do not continue implementation") {
		t.Fatalf("expected rejection handoff to avoid implementation-specific stop wording, got %q", output)
	}

	fileOutput, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("read handoff file: %v", err)
	}
	if string(fileOutput) != output {
		t.Fatalf("expected rejection file output to match stdout\nfile:\n%s\nstdout:\n%s", fileOutput, output)
	}
}

func TestReviewNoBrowserFlagSkipsOpener(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	oldOpenBrowser := openBrowser
	openBrowser = func(url string) error {
		t.Fatalf("expected --no-browser to skip opener, got %s", url)
		return nil
	}
	t.Cleanup(func() {
		openBrowser = oldOpenBrowser
	})

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	finalizeBody := strings.NewReader(`{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", finalizeBody)
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewPortFlagUsesRequestedPort(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	port := freeTCPPort(t)

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", "--port", fmt.Sprint(port), path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	if !strings.HasSuffix(url, fmt.Sprintf(":%d", port)) {
		t.Fatalf("expected review URL to use port %d, got %s", port, url)
	}

	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", strings.NewReader(`{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`))
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewPortFlagRejectsInvalidPort(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--port", "70000", path})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid port error")
	}
	if !strings.Contains(err.Error(), "port must be between 0 and 65535") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestReviewSideQuestionTimeoutFlagIsApplied(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "current-thread")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeSlowAppServerCommand
	t.Cleanup(func() {
		execCommandContext = oldExecCommandContext
	})
	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", "--side-question-timeout", "50ms", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	threadID := createReviewThread(t, url)
	sideQuestionBody := strings.NewReader(`{"threadID":"` + threadID + `","question":"Slow?","planExcerpt":"# Test plan"}`)
	sideQuestionRes, err := http.Post(url+"/api/side-questions", "application/json", sideQuestionBody)
	if err != nil {
		t.Fatalf("ask side question: %v", err)
	}
	defer sideQuestionRes.Body.Close()
	if sideQuestionRes.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(sideQuestionRes.Body)
		t.Fatalf("expected side-question status 503, got %d: %s", sideQuestionRes.StatusCode, body)
	}

	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", strings.NewReader(`{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`))
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewHandoffOutMirrorsStdoutOnFinalize(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	handoffPath := t.TempDir() + "/handoff.md"

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", "--handoff-out", handoffPath, path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", strings.NewReader(`{"summary":"Approved file","reviewerDecisions":[],"promotedSideAnswers":[]}`))
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
	fileOutput, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatalf("read handoff file: %v", err)
	}
	if string(fileOutput) != stdout.String() {
		t.Fatalf("expected file output to match stdout\nfile:\n%s\nstdout:\n%s", fileOutput, stdout.String())
	}
}

func TestReviewHandoffOutDoesNotWriteOnCancel(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer
	handoffPath := t.TempDir() + "/handoff.md"

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", "--handoff-out", handoffPath, path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	defer cancelRes.Body.Close()
	if cancelRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelRes.Body)
		t.Fatalf("expected cancel status 200, got %d: %s", cancelRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
	if _, err := os.Stat(handoffPath); !os.IsNotExist(err) {
		t.Fatalf("expected no handoff file on cancel, stat err %v", err)
	}
}

func TestReviewAutosavesCommentsBeforeCancel(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	body := strings.NewReader(`{"anchor":{"startLine":1,"endLine":1},"body":"Do not lose this comment"}`)
	threadRes, err := http.Post(url+"/api/threads", "application/json", body)
	if err != nil {
		t.Fatalf("create review thread: %v", err)
	}
	defer threadRes.Body.Close()
	if threadRes.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(threadRes.Body)
		t.Fatalf("expected thread create status 200, got %d: %s", threadRes.StatusCode, data)
	}

	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	defer cancelRes.Body.Close()
	if cancelRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelRes.Body)
		t.Fatalf("expected cancel status 200, got %d: %s", cancelRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}

	autosave, ok, err := review.LoadAutosave(path + ".planmaxx-review.json")
	if err != nil {
		t.Fatalf("load autosave: %v", err)
	}
	if !ok {
		t.Fatal("expected autosave file")
	}
	if autosave.Status != "canceled" {
		t.Fatalf("expected canceled autosave, got %q", autosave.Status)
	}
	if got := autosave.Session.Threads[0].Messages[0].Body; got != "Do not lose this comment" {
		t.Fatalf("expected autosaved comment, got %q", got)
	}
}

func TestReviewFallsBackToCacheAutosaveWhenDefaultPathCannotBeWritten(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	cacheDir := t.TempDir()
	oldUserCacheDir := userCacheDir
	userCacheDir = func() (string, error) {
		return cacheDir, nil
	}
	t.Cleanup(func() {
		userCacheDir = oldUserCacheDir
	})

	planDir := t.TempDir()
	path := planDir + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}
	if err := os.Chmod(planDir, 0o500); err != nil {
		t.Fatalf("make plan dir read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(planDir, 0o700)
	})

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	defer cancelRes.Body.Close()
	if cancelRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelRes.Body)
		t.Fatalf("expected cancel status 200, got %d: %s", cancelRes.StatusCode, body)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}

	fallbackPath, err := cacheAutosavePath(review.NewDocument(path, "# Test plan\n").CanonicalPath)
	if err != nil {
		t.Fatalf("cache autosave path: %v", err)
	}
	if !strings.Contains(stderr.String(), "PlanMaxx autosave fallback: "+fallbackPath) {
		t.Fatalf("expected autosave fallback message with %q, got %q", fallbackPath, stderr.String())
	}
	autosave, ok, err := review.LoadAutosave(fallbackPath)
	if err != nil {
		t.Fatalf("load fallback autosave: %v", err)
	}
	if !ok {
		t.Fatal("expected fallback autosave file")
	}
	if autosave.Status != "canceled" {
		t.Fatalf("expected canceled fallback autosave, got %q", autosave.Status)
	}
}

func TestReviewRestoresMatchingAutosave(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	plan := "# Test plan\n"
	if err := os.WriteFile(path, []byte(plan), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}
	savedSession := session.New("session-1", plan)
	savedSession.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Recovered comment")
	data, err := json.Marshal(review.Autosave{
		Version: 1,
		Status:  "canceled",
		Session: *savedSession,
	})
	if err != nil {
		t.Fatalf("marshal autosave: %v", err)
	}
	if err := os.WriteFile(path+".planmaxx-review.json", data, 0o600); err != nil {
		t.Fatalf("write autosave: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	stateRes, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get review state: %v", err)
	}
	defer stateRes.Body.Close()
	if stateRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(stateRes.Body)
		t.Fatalf("expected state status 200, got %d: %s", stateRes.StatusCode, body)
	}
	var state session.Session
	if err := json.NewDecoder(stateRes.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.Threads) != 1 {
		t.Fatalf("expected restored thread, got %+v", state.Threads)
	}
	if got := state.Threads[0].Messages[0].Body; got != "Recovered comment" {
		t.Fatalf("expected restored comment, got %q", got)
	}
	canonicalPath := review.NewDocument(path, plan).CanonicalPath
	if !strings.Contains(stderr.String(), "PlanMaxx restored autosave: "+canonicalPath+".planmaxx-review.json") {
		t.Fatalf("expected restored autosave message, got %q", stderr.String())
	}

	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	defer cancelRes.Body.Close()
	if cancelRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelRes.Body)
		t.Fatalf("expected cancel status 200, got %d: %s", cancelRes.StatusCode, body)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewRestoresCacheFallbackOnNextSession(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	cacheDir := t.TempDir()
	oldUserCacheDir := userCacheDir
	userCacheDir = func() (string, error) { return cacheDir, nil }
	t.Cleanup(func() { userCacheDir = oldUserCacheDir })

	path := filepath.Join(t.TempDir(), "plan.md")
	plan := "# Test plan\n"
	if err := os.WriteFile(path, []byte(plan), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	document := review.NewDocument(path, plan)
	fallbackPath, err := cacheAutosavePath(document.CanonicalPath)
	if err != nil {
		t.Fatalf("cache autosave path: %v", err)
	}
	savedSession := session.New("session-1", plan)
	savedSession.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Recovered from cache")
	payload, err := json.Marshal(review.Autosave{
		Version:  2,
		Format:   "planmaxx.review",
		Status:   "canceled",
		Document: document,
		Session:  *savedSession,
	})
	if err != nil {
		t.Fatalf("marshal fallback autosave: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o700); err != nil {
		t.Fatalf("create fallback directory: %v", err)
	}
	if err := os.WriteFile(fallbackPath, payload, 0o600); err != nil {
		t.Fatalf("write fallback autosave: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

	url := waitForReviewURL(t, &stderr)
	stateRes, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateRes.Body.Close()
	var state session.Session
	if err := json.NewDecoder(stateRes.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.Threads) != 1 || state.Threads[0].Messages[0].Body != "Recovered from cache" {
		t.Fatalf("expected fallback comment to be restored, got %+v", state.Threads)
	}
	if !strings.Contains(stderr.String(), "PlanMaxx restored autosave: "+fallbackPath) {
		t.Fatalf("expected fallback restore message, got %q", stderr.String())
	}
	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	cancelRes.Body.Close()
	if err := <-errCh; err == nil {
		t.Fatal("expected canceled review to return an error")
	}
}

func TestReviewKeepsInAppRevisionWhenSourceFileIsUnchanged(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	path := filepath.Join(t.TempDir(), "plan.md")
	sourcePlan := "# Test plan\n\n- Source\n"
	if err := os.WriteFile(path, []byte(sourcePlan), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	document := review.NewDocument(path, sourcePlan)
	savedSession := session.New("session-1", sourcePlan)
	workingPlan := "# Test plan\n\n- Edited in PlanMaxx\n"
	savedSession.AddTurnRevision(workingPlan, "Accepted proposal")
	payload, err := json.Marshal(review.Autosave{
		Version:  2,
		Format:   "planmaxx.review",
		Status:   "canceled",
		Document: document,
		Session:  *savedSession,
	})
	if err != nil {
		t.Fatalf("marshal autosave: %v", err)
	}
	if err := os.WriteFile(document.CanonicalPath+".planmaxx-review.json", payload, 0o600); err != nil {
		t.Fatalf("write autosave: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()
	url := waitForReviewURL(t, &stderr)
	stateRes, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateRes.Body.Close()
	var state session.Session
	if err := json.NewDecoder(stateRes.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Plan != workingPlan || len(state.Revisions) != 2 {
		t.Fatalf("expected working revision to survive, got plan=%q revisions=%+v", state.Plan, state.Revisions)
	}
	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	cancelRes.Body.Close()
	if err := <-errCh; err == nil {
		t.Fatal("expected canceled review to return an error")
	}
}

func TestLoadNewestAutosaveUsesFallbackWhenSidecarIsInvalid(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	plan := "# Plan\n"
	document := review.NewDocument(planPath, plan)
	sidecar := planPath + ".planmaxx-review.json"
	fallback := filepath.Join(dir, "fallback.json")
	if err := os.WriteFile(sidecar, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write invalid sidecar: %v", err)
	}
	s := session.New("session-1", plan)
	s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Keep fallback")
	payload, err := json.Marshal(review.Autosave{Version: 2, Format: "planmaxx.review", Document: document, Session: *s})
	if err != nil {
		t.Fatalf("marshal fallback: %v", err)
	}
	if err := os.WriteFile(fallback, payload, 0o600); err != nil {
		t.Fatalf("write fallback: %v", err)
	}

	saved, path, ok, err := loadNewestAutosave([]string{sidecar, fallback}, document)
	if err != nil || !ok || path != fallback {
		t.Fatalf("expected valid fallback, got saved=%+v path=%q ok=%v err=%v", saved, path, ok, err)
	}
	if len(saved.Session.Threads) != 1 || saved.Session.Threads[0].Messages[0].Body != "Keep fallback" {
		t.Fatalf("expected fallback state, got %+v", saved.Session)
	}
}

func TestLoadNewestAutosaveRefusesToOverwriteFutureSidecar(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	plan := "# Plan\n"
	document := review.NewDocument(planPath, plan)
	sidecar := planPath + ".planmaxx-review.json"
	fallback := filepath.Join(dir, "fallback.json")
	future := `{"version":99,"format":"planmaxx.review","session":{}}`
	if err := os.WriteFile(sidecar, []byte(future), 0o600); err != nil {
		t.Fatalf("write future sidecar: %v", err)
	}
	payload, err := json.Marshal(review.Autosave{Version: 2, Format: "planmaxx.review", Document: document, Session: *session.New("session-1", plan)})
	if err != nil {
		t.Fatalf("marshal fallback: %v", err)
	}
	if err := os.WriteFile(fallback, payload, 0o600); err != nil {
		t.Fatalf("write fallback: %v", err)
	}

	if _, _, _, err := loadNewestAutosave([]string{sidecar, fallback}, document); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected future-version error, got %v", err)
	}
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read future sidecar: %v", err)
	}
	if string(data) != future {
		t.Fatalf("future sidecar was modified: %q", data)
	}
}

func TestReviewRecordsChangedAutosavePlanAsTurnRevision(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	dir := t.TempDir()
	path := dir + "/plan.md"
	oldPlan := "# Test plan\n\n- Old\n"
	newPlan := "# Test plan\n\n- New\n"
	if err := os.WriteFile(path, []byte(newPlan), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}
	savedSession := session.New("session-1", oldPlan)
	savedSession.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Recovered comment")
	data, err := json.Marshal(review.Autosave{
		Version: 1,
		Status:  "active",
		Session: *savedSession,
	})
	if err != nil {
		t.Fatalf("marshal autosave: %v", err)
	}
	if err := os.WriteFile(path+".planmaxx-review.json", data, 0o600); err != nil {
		t.Fatalf("write autosave: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", "--no-browser", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	stateRes, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatalf("get review state: %v", err)
	}
	defer stateRes.Body.Close()
	if stateRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(stateRes.Body)
		t.Fatalf("expected state status 200, got %d: %s", stateRes.StatusCode, body)
	}
	var state session.Session
	if err := json.NewDecoder(stateRes.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Plan != newPlan {
		t.Fatalf("expected served plan to be new plan, got %q", state.Plan)
	}
	if state.CurrentRevisionID != "rev-2" || len(state.Revisions) != 2 {
		t.Fatalf("expected turn revision after changed autosave, got current=%q revisions=%+v", state.CurrentRevisionID, state.Revisions)
	}
	if state.Revisions[1].Source != session.RevisionSourceExternal || state.Revisions[1].ParentID != "rev-1" {
		t.Fatalf("expected second revision to be turn child, got %+v", state.Revisions[1])
	}
	if len(state.Threads) != 1 || state.Threads[0].Messages[0].Body != "Recovered comment" {
		t.Fatalf("expected restored thread to remain, got %+v", state.Threads)
	}

	cancelRes, err := http.Post(url+"/api/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("cancel review: %v", err)
	}
	defer cancelRes.Body.Close()
	if cancelRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelRes.Body)
		t.Fatalf("expected cancel status 200, got %d: %s", cancelRes.StatusCode, body)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewWiresAppServerSideQuestionsWhenThreadIDSet(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "current-thread")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeAppServerCommand
	t.Cleanup(func() {
		execCommandContext = oldExecCommandContext
	})

	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	threadID := createReviewThread(t, url)

	sideQuestionBody := strings.NewReader(`{"threadID":"` + threadID + `","question":"What should move first?","planExcerpt":"1. CLI"}`)
	sideQuestionRes, err := http.Post(url+"/api/side-questions", "application/json", sideQuestionBody)
	if err != nil {
		t.Fatalf("ask side question: %v", err)
	}
	defer sideQuestionRes.Body.Close()
	if sideQuestionRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(sideQuestionRes.Body)
		t.Fatalf("expected side-question status 200, got %d: %s", sideQuestionRes.StatusCode, body)
	}
	var sideAnswer struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(sideQuestionRes.Body).Decode(&sideAnswer); err != nil {
		t.Fatalf("decode side answer: %v", err)
	}
	if sideAnswer.Answer != "Use the CLI first." {
		t.Fatalf("unexpected side answer %q", sideAnswer.Answer)
	}

	finalizeBody := strings.NewReader(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", finalizeBody)
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete after finalize, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
	if !strings.Contains(stderr.String(), "fake app-server stderr") {
		t.Fatalf("expected app-server stderr to be forwarded, got %q", stderr.String())
	}
}

func TestReviewRejectsSideQuestionsWithoutThreadID(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	threadID := createReviewThread(t, url)

	sideQuestionBody := strings.NewReader(`{"threadID":"` + threadID + `","question":"What should move first?","planExcerpt":"1. CLI"}`)
	sideQuestionRes, err := http.Post(url+"/api/side-questions", "application/json", sideQuestionBody)
	if err != nil {
		t.Fatalf("ask side question: %v", err)
	}
	defer sideQuestionRes.Body.Close()
	if sideQuestionRes.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(sideQuestionRes.Body)
		t.Fatalf("expected side-question status 503, got %d: %s", sideQuestionRes.StatusCode, body)
	}

	finalizeBody := strings.NewReader(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", finalizeBody)
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete after finalize, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
}

func TestReviewRejectsSideQuestionsWhenAppServerCannotStart(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "current-thread")
	oldExecCommandContext := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "planmaxx-missing-codex-for-test")
	}
	t.Cleanup(func() {
		execCommandContext = oldExecCommandContext
	})

	var stdout lockedBuffer
	var stderr lockedBuffer

	path := t.TempDir() + "/plan.md"
	if err := os.WriteFile(path, []byte("# Test plan\n"), 0o600); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}

	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"review", path})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	url := waitForReviewURL(t, &stderr)
	threadID := createReviewThread(t, url)

	sideQuestionBody := strings.NewReader(`{"threadID":"` + threadID + `","question":"What should move first?","planExcerpt":"1. CLI"}`)
	sideQuestionRes, err := http.Post(url+"/api/side-questions", "application/json", sideQuestionBody)
	if err != nil {
		t.Fatalf("ask side question: %v", err)
	}
	defer sideQuestionRes.Body.Close()
	if sideQuestionRes.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(sideQuestionRes.Body)
		t.Fatalf("expected side-question status 503, got %d: %s", sideQuestionRes.StatusCode, body)
	}

	finalizeBody := strings.NewReader(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	finalizeRes, err := http.Post(url+"/api/finalize", "application/json", finalizeBody)
	if err != nil {
		t.Fatalf("finalize review: %v", err)
	}
	defer finalizeRes.Body.Close()
	if finalizeRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeRes.Body)
		t.Fatalf("expected finalize status 200, got %d: %s", finalizeRes.StatusCode, body)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected review command to complete after finalize, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for review command")
	}
	if !strings.Contains(stderr.String(), "PlanMaxx side questions unavailable") {
		t.Fatalf("expected app-server warning, got %q", stderr.String())
	}
}

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

func waitForReviewURL(t *testing.T, stderr *lockedBuffer) string {
	t.Helper()

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for review URL in stderr: %q", stderr.String())
		case <-ticker.C:
			for _, line := range strings.Split(stderr.String(), "\n") {
				url, ok := strings.CutPrefix(line, "PlanMaxx review URL: ")
				if ok {
					return url
				}
			}
		}
	}
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

func createReviewThread(t *testing.T, url string) string {
	t.Helper()

	body := strings.NewReader(`{"anchor":{"startLine":1,"endLine":1},"body":"Clarify"}`)
	res, err := http.Post(url+"/api/threads", "application/json", body)
	if err != nil {
		t.Fatalf("create review thread: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("expected thread create status 200, got %d: %s", res.StatusCode, data)
	}
	var thread struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&thread); err != nil {
		t.Fatalf("decode created thread: %v", err)
	}
	if thread.ID == "" {
		t.Fatal("expected created thread id")
	}
	return thread.ID
}

func fakeAppServerCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestFakeAppServerProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "PLANMAXX_FAKE_APP_SERVER=1")
	return cmd
}

func fakeSlowAppServerCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestFakeAppServerProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "PLANMAXX_FAKE_APP_SERVER=1", "PLANMAXX_FAKE_APP_SERVER_SLOW_TURN=1")
	return cmd
}

func TestFakeAppServerProcess(t *testing.T) {
	if os.Getenv("PLANMAXX_FAKE_APP_SERVER") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	separator := 0
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == 0 || strings.Join(args[separator+1:], " ") != "codex app-server --listen stdio://" {
		fmt.Fprintf(os.Stderr, "unexpected args: %v\n", args)
		os.Exit(2)
	}

	fmt.Fprintln(os.Stderr, "fake app-server stderr")
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			fmt.Fprintf(os.Stderr, "bad request: %v\n", err)
			os.Exit(2)
		}
		method, _ := req["method"].(string)
		id := req["id"]
		switch method {
		case "initialize":
			writeFakeResponse(encoder, id, map[string]any{"userAgent": "Codex", "codexHome": "/tmp/codex"})
		case "initialized":
		case "thread/read":
			writeFakeResponse(encoder, id, map[string]any{"thread": map[string]any{"id": "current-thread", "status": map[string]any{"type": "idle"}}})
		case "thread/fork":
			writeFakeResponse(encoder, id, map[string]any{"thread": map[string]any{"id": "fork-1", "forkedFromId": "current-thread", "ephemeral": true, "cwd": "/repo", "status": map[string]any{"type": "idle"}}, "cwd": "/repo"})
		case "turn/start":
			if os.Getenv("PLANMAXX_FAKE_APP_SERVER_SLOW_TURN") == "1" {
				time.Sleep(2 * time.Second)
			}
			writeFakeResponse(encoder, id, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}})
			writeFakeNotification(encoder, "item/completed", map[string]any{"threadId": "fork-1", "turnId": "turn-1", "item": map[string]any{"type": "agentMessage", "text": "Use the CLI first."}})
			writeFakeNotification(encoder, "turn/completed", map[string]any{"threadId": "fork-1", "turn": map[string]any{"id": "turn-1", "status": "completed"}})
		default:
			fmt.Fprintf(os.Stderr, "unexpected method: %s\n", method)
			os.Exit(2)
		}
	}
}

func writeFakeResponse(encoder *json.Encoder, id any, result map[string]any) {
	if err := encoder.Encode(map[string]any{"id": id, "result": result}); err != nil {
		fmt.Fprintf(os.Stderr, "write response: %v\n", err)
		os.Exit(2)
	}
}

func writeFakeNotification(encoder *json.Encoder, method string, params map[string]any) {
	if err := encoder.Encode(map[string]any{"method": method, "params": params}); err != nil {
		fmt.Fprintf(os.Stderr, "write notification: %v\n", err)
		os.Exit(2)
	}
}
