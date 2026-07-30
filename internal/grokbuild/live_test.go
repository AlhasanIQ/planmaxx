package grokbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const liveGrokTestEnvironment = "PLANMAXX_RUN_LIVE_GROK"

type liveGrokResponse struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	Text       string `json:"text"`
	StopReason string `json:"stopReason"`
	SessionID  string `json:"sessionId"`
}

func TestLiveGrokBuildForkRetainsContextAndDeletesChild(t *testing.T) {
	requireLiveGrok(t)
	sourceSessionID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer deleteLiveSession(t, sourceSessionID)

	conversationToken := "COBALT-" + strings.ToUpper(strings.ReplaceAll(sourceSessionID[:8], "-", ""))
	fileToken := "AMBER-" + strings.ToUpper(strings.ReplaceAll(sourceSessionID[9:17], "-", ""))
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "context.txt"), []byte(fileToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := runLiveGrok(t, workspace, []string{
		"--session-id", sourceSessionID,
		"--output-format", "json",
		"--tools", "read_file,grep,list_dir",
		"--allow", "Read",
		"--allow", "Grep",
		"--deny", "MCPTool",
		"--permission-mode", "dontAsk",
		"--sandbox", "strict",
		"--no-subagents",
		"--no-memory",
		"--disable-web-search",
		"--no-plan",
		"--max-turns", "2",
		"--verbatim",
		"-p", "Remember this verification token: " + conversationToken + ". Reply only READY.",
	})
	if response.SessionID != sourceSessionID || response.StopReason != "EndTurn" {
		t.Fatalf("unexpected source response: %+v", response)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	answer, err := NewClient(sourceSessionID, "").AskPrompt(
		ctx,
		"Reply only with the verification token from the previous turn, then a pipe, then the exact contents of context.txt in the current project. Inspect the file; its contents were not provided in conversation.",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := conversationToken + "|" + fileToken
	if strings.TrimSpace(answer) != want {
		t.Fatalf("fork did not retain conversation and workspace context: got %q want %q", answer, want)
	}
}

func TestLiveGrokNativeSkillSubstitutesExactSessionAtInvocation(t *testing.T) {
	requireLiveGrok(t)
	workspace := t.TempDir()
	binaryDir := t.TempDir()
	realPlanMaxx := filepath.Join(binaryDir, "planmaxx-real")
	build := exec.Command("go", "build", "-o", realPlanMaxx, "./cmd/planmaxx")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build current PlanMaxx binary: %v: %s", err, output)
	}

	binDir := filepath.Join(workspace, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(workspace, "planmaxx-args.txt")
	reviewStderrPath := filepath.Join(workspace, "planmaxx-stderr.txt")
	reviewPIDPath := filepath.Join(workspace, "planmaxx-pid.txt")
	probePath := filepath.Join(binDir, "planmaxx-probe")
	probe := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + strconv.Quote(transcriptPath) + "\n" +
		"printf '%s\\n' \"$$\" > " + strconv.Quote(reviewPIDPath) + "\n" +
		"exec " + strconv.Quote(realPlanMaxx) + " \"$@\" --no-browser --local-bundle 2> " +
		strconv.Quote(reviewStderrPath) + "\n"
	if err := os.WriteFile(probePath, []byte(probe), 0o755); err != nil {
		t.Fatal(err)
	}
	installSkill := exec.Command(realPlanMaxx, "skill", "install", "--target", "grok", "--repo", workspace)
	installSkill.Env = liveGrokEnvironment(os.Environ())
	if output, err := installSkill.CombinedOutput(); err != nil {
		t.Fatalf("install native Grok skill: %v: %s", err, output)
	}
	skillPath := filepath.Join(workspace, ".grok", "skills", "planmaxx", "SKILL.md")
	template, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(template), "planmaxx review --grok-session-id ${SESSION_ID}") {
		t.Fatalf("installer produced the wrong Grok invocation:\n%s", template)
	}
	probeTemplate := strings.Replace(string(template), "planmaxx review", probePath+" review", 1)
	if probeTemplate == string(template) {
		t.Fatal("production Grok skill command was not found")
	}
	if err := os.WriteFile(skillPath, []byte(probeTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(workspace, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Tiny plan\n\n1. Finish.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inspect := exec.Command("grok", "inspect", "--json")
	inspect.Dir = workspace
	inspect.Env = liveGrokEnvironment(os.Environ())
	inspectOutput, err := inspect.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect native skill discovery: %v: %s", err, inspectOutput)
	}
	if !strings.Contains(string(inspectOutput), filepath.Join(".grok", "skills", "planmaxx")) {
		t.Fatalf("grok inspect did not discover the native PlanMaxx skill: %s", inspectOutput)
	}

	sessionID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer deleteLiveSession(t, sessionID)
	token := "VIOLET-" + strings.ToUpper(sessionID[:8])
	outerStdoutPath := filepath.Join(workspace, "grok-stdout.json")
	outerStderrPath := filepath.Join(workspace, "grok-stderr.txt")
	outerStdout, err := os.OpenFile(outerStdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	outerStderr, err := os.OpenFile(outerStderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = outerStdout.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "grok", []string{
		"--session-id", sessionID,
		"--output-format", "json",
		"--tools", "run_terminal_cmd",
		"--always-approve",
		"--no-subagents",
		"--no-memory",
		"--disable-web-search",
		"--no-plan",
		"--max-turns", "2",
		"-p", "/planmaxx " + planPath + "\nRemember token " + token +
			" for questions during this review. Do not pass the token to PlanMaxx. " +
			"After PlanMaxx exits, reply only DONE and do not retry.",
	}...)
	command.Dir = workspace
	command.Env = liveGrokEnvironmentWithoutSessionMarkers(os.Environ())
	command.Stdout = outerStdout
	command.Stderr = outerStderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = outerStdout.Close()
		_ = outerStderr.Close()
		killLiveReviewProcess(reviewPIDPath)
		cancel()
		if command.ProcessState == nil && command.Process != nil {
			_ = command.Process.Kill()
		}
	})

	args := waitForLiveFile(t, transcriptPath, 45*time.Second)
	lines := strings.Fields(string(args))
	wantPrefix := []string{"review", "--grok-session-id", sessionID}
	if len(lines) != 4 {
		t.Fatalf("unexpected PlanMaxx invocation: got %q", lines)
	}
	for index := range wantPrefix {
		if lines[index] != wantPrefix[index] {
			t.Fatalf("unexpected PlanMaxx invocation: got %q want prefix %q", lines, wantPrefix)
		}
	}
	gotPlanInfo, gotPlanErr := os.Stat(lines[3])
	wantPlanInfo, wantPlanErr := os.Stat(planPath)
	if gotPlanErr != nil || wantPlanErr != nil || !os.SameFile(gotPlanInfo, wantPlanInfo) {
		t.Fatalf("PlanMaxx received plan %q, want the same file as %q", lines[3], planPath)
	}
	select {
	case err := <-done:
		t.Fatalf("parent Grok exited before the blocking review interaction: %v", err)
	default:
	}

	reviewURL := waitForLiveReviewURL(t, reviewStderrPath, 45*time.Second)
	var state struct {
		Agent struct {
			ID          string `json:"id"`
			ContextMode string `json:"contextMode"`
			Available   bool   `json:"available"`
		} `json:"agent"`
	}
	liveGetJSON(t, reviewURL+"/api/state", &state)
	if state.Agent.ID != "grok" || state.Agent.ContextMode != "current-session-fork" || !state.Agent.Available {
		t.Fatalf("unexpected live Grok attachment: %+v", state.Agent)
	}
	var thread struct {
		ID string `json:"id"`
	}
	livePostJSON(t, reviewURL+"/api/threads", `{"anchor":{"startLine":1,"endLine":1},"body":"Verify active context."}`, &thread)
	if thread.ID == "" {
		t.Fatal("live review did not create a thread")
	}
	var answer struct {
		Answer string `json:"answer"`
	}
	question := fmt.Sprintf(
		`{"threadID":%q,"question":"What verification token did the user ask you to remember in the instruction that started this currently blocked review? Reply only with it.","planExcerpt":"# Tiny plan"}`,
		thread.ID,
	)
	livePostJSON(t, reviewURL+"/api/side-questions", question, &answer)
	if strings.TrimSpace(answer.Answer) != token {
		t.Fatalf("active-session fork lost the blocked parent context: got %q want %q", answer.Answer, token)
	}
	livePostJSON(t, reviewURL+"/api/cancel", `{}`, nil)

	parentFinished := false
	select {
	case <-time.After(10 * time.Second):
		cancel()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-done
	case <-done:
		parentFinished = true
	}
	_ = outerStdout.Close()
	_ = outerStderr.Close()
	if !parentFinished {
		return
	}
	var response liveGrokResponse
	content, err := os.ReadFile(outerStdoutPath)
	if err != nil || json.Unmarshal(content, &response) != nil {
		t.Fatalf("decode parent Grok result: content=%s err=%v", content, err)
	}
	if response.SessionID != sessionID {
		t.Fatalf("native skill ran in session %q, want %q", response.SessionID, sessionID)
	}
}

func killLiveReviewProcess(pidPath string) {
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(pidBytes)), "%d", &pid); err != nil || pid <= 0 {
		return
	}
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}

func waitForLiveFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if content, err := os.ReadFile(path); err == nil && len(content) != 0 {
			return content
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}

func waitForLiveReviewURL(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if content, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if url, ok := strings.CutPrefix(line, "PlanMaxx review URL: "); ok {
					return strings.TrimSpace(url)
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for PlanMaxx review URL in %s", path)
	return ""
}

func liveGetJSON(t *testing.T, url string, destination any) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s returned %d: %s", url, response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}

func livePostJSON(t *testing.T, url, body string, destination any) {
	t.Helper()
	response, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		content, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s returned %d: %s", url, response.StatusCode, content)
	}
	if destination != nil {
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			t.Fatal(err)
		}
	}
}

func requireLiveGrok(t *testing.T) {
	t.Helper()
	if os.Getenv(liveGrokTestEnvironment) != "1" {
		t.Skip("set " + liveGrokTestEnvironment + "=1 to run short live Grok Build checks")
	}
	if _, err := exec.LookPath("grok"); err != nil {
		t.Skip("grok executable is unavailable")
	}
}

func runLiveGrok(t *testing.T, cwd string, args []string) liveGrokResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "grok", args...)
	command.Dir = cwd
	command.Env = liveGrokEnvironment(os.Environ())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		t.Fatalf("run live Grok Build: %v: stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	var response liveGrokResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode live Grok Build response: %v: stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if response.Type == "error" || strings.TrimSpace(response.Text) == "" {
		t.Fatalf("live Grok Build returned an unsuccessful response: %+v", response)
	}
	return response
}

func deleteLiveSession(t *testing.T, sessionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "grok", "sessions", "delete", sessionID)
	command.Env = liveGrokEnvironment(os.Environ())
	if output, err := command.CombinedOutput(); err != nil {
		t.Errorf("delete live Grok Build session %s: %v: %s", sessionID, err, output)
	}
}

func liveGrokEnvironment(base []string) []string {
	return append(base, "GROK_DISABLE_AUTOUPDATER=1")
}

func liveGrokEnvironmentWithoutSessionMarkers(base []string) []string {
	filtered := make([]string, 0, len(base)+1)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && (key == "SESSION_ID" || key == "GROK_SESSION_ID") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "GROK_DISABLE_AUTOUPDATER=1")
}
