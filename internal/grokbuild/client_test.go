package grokbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

const (
	helperProcessEnvironment = "PLANMAXX_GROKBUILD_HELPER"
	sourceSessionID          = "019c0f29-e4f8-7c7b-bb88-b3f7a68e605d"
	forkSessionID            = "36ce14ad-f0b7-4544-97d7-e1097e344268"
)

type commandInvocation struct {
	name string
	args []string
	cmd  *exec.Cmd
}

func TestClientAskUsesConfiguredSessionAndDisposableFork(t *testing.T) {
	var invocations []commandInvocation
	var prompt string
	var promptMode os.FileMode
	factory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		invocations = append(invocations, commandInvocation{name: name, args: append([]string(nil), args...)})
		if len(args) >= 2 && args[0] == "sessions" && args[1] == "delete" {
			command := helperCommand(ctx, "", "", 0, 0)
			invocations[len(invocations)-1].cmd = command
			return command
		}
		for index, arg := range args {
			if arg == "--prompt-file" && index+1 < len(args) {
				content, err := os.ReadFile(args[index+1])
				if err != nil {
					t.Fatalf("read prompt file: %v", err)
				}
				prompt = string(content)
				info, err := os.Stat(args[index+1])
				if err != nil {
					t.Fatalf("stat prompt file: %v", err)
				}
				promptMode = info.Mode().Perm()
			}
		}
		command := helperCommand(ctx,
			`{"text":"Use the CLI first.","stopReason":"EndTurn","sessionId":"`+forkSessionID+`"}`,
			"",
			0,
			0,
		)
		invocations[len(invocations)-1].cmd = command
		return command
	}
	workingDirectory := t.TempDir()
	client := NewClient(
		sourceSessionID,
		workingDirectory,
		WithCommandFactory(factory),
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(testIsolationFactory(t)),
	)

	answer, err := client.Ask(context.Background(), sidequestions.Request{
		ThreadID:     "question-session",
		Question:     "What should move first?",
		FilePath:     "/repo/plan.md",
		Reference:    "/repo/plan.md:10:1-10:4",
		SelectedText: "CLI",
		PlanExcerpt:  "1. CLI\n2. UI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Use the CLI first." {
		t.Fatalf("unexpected answer %q", answer)
	}
	if len(invocations) != 2 {
		t.Fatalf("invocations = %d, want prompt and cleanup: %#v", len(invocations), invocations)
	}
	isolatedCWD := argumentValue(t, invocations[0].args, "--cwd")
	isolatedRoot := isolatedCWD
	if invocations[0].name != "grok" || invocations[0].cmd.Dir != isolatedCWD || isolatedCWD == workingDirectory {
		t.Fatalf("unexpected prompt invocation: %+v", invocations[0])
	}
	wantArgs := []string{
		"--cwd", isolatedCWD,
		"--prompt-file", argumentValue(t, invocations[0].args, "--prompt-file"),
		"--resume", forkSessionID,
		"--output-format", "json",
		"--tools", "read_file,grep,list_dir",
		"--allow", "Read(" + filepath.ToSlash(filepath.Join(isolatedRoot, "**")) + ")",
		"--allow", "Grep(" + filepath.ToSlash(filepath.Join(isolatedRoot, "**")) + ")",
		"--deny", "MCPTool",
		"--permission-mode", "dontAsk",
		"--sandbox", isolatedSandboxProfile,
		"--no-subagents",
		"--no-memory",
		"--disable-web-search",
		"--no-plan",
		"--max-turns", "2",
		"--verbatim",
	}
	if !reflect.DeepEqual(invocations[0].args, wantArgs) {
		t.Fatalf("unexpected arguments\nwant: %#v\n got: %#v", wantArgs, invocations[0].args)
	}
	if !reflect.DeepEqual(invocations[1].args, []string{"sessions", "delete", forkSessionID}) {
		t.Fatalf("unexpected cleanup arguments: %#v", invocations[1].args)
	}
	if invocations[1].cmd.Dir != isolatedCWD {
		t.Fatalf("cleanup escaped isolated workspace: %q", invocations[1].cmd.Dir)
	}
	if promptMode != 0o600 {
		t.Fatalf("prompt file mode = %o, want 600", promptMode)
	}
	for _, want := range []string{
		"provided read-only tools",
		"Do not edit files",
		"What should move first?",
		"/repo/plan.md",
		"/repo/plan.md:10:1-10:4",
		"<selected_text>CLI</selected_text>",
		"1. CLI",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\n%s", want, prompt)
		}
	}
	if _, err := os.Stat(argumentValue(t, invocations[0].args, "--prompt-file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prompt file should be removed after use, got %v", err)
	}
}

func TestClientAskPromptUsesRawPrompt(t *testing.T) {
	var prompt string
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(testIsolationFactory(t)),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				return helperCommand(ctx, "", "", 0, 0)
			}
			content, err := os.ReadFile(argumentValue(t, args, "--prompt-file"))
			if err != nil {
				t.Fatal(err)
			}
			prompt = string(content)
			return helperCommand(ctx,
				`{"text":"<proposal />","stopReason":"EndTurn","sessionId":"`+forkSessionID+`"}`,
				"",
				0,
				0,
			)
		}),
	)

	answer, err := client.AskPrompt(context.Background(), "Rewrite the selected section.")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "<proposal />" ||
		!strings.Contains(prompt, "Rewrite the selected section.") ||
		!strings.Contains(prompt, "provided read-only tools") {
		t.Fatalf("answer=%q prompt=%q", answer, prompt)
	}
}

func TestClientRejectsUnsuccessfulOrInvalidResponsesAndCleansFork(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		stderr     string
		exit       int
		cleanupErr bool
		wantErr    string
	}{
		{
			name:    "process error",
			stderr:  "authentication required",
			exit:    7,
			wantErr: "authentication required",
		},
		{
			name:    "malformed json",
			stdout:  "{",
			stderr:  "unexpected provider response",
			wantErr: "decode Grok Build response",
		},
		{
			name:    "error response",
			stdout:  `{"type":"error","message":"tool failed"}`,
			wantErr: "tool failed",
		},
		{
			name:    "empty result",
			stdout:  `{"text":"  ","stopReason":"EndTurn","sessionId":"` + forkSessionID + `"}`,
			wantErr: "empty result",
		},
		{
			name:    "incomplete stop",
			stdout:  `{"text":"partial","stopReason":"MaxTurns","sessionId":"` + forkSessionID + `"}`,
			wantErr: "stopped without completing",
		},
		{
			name:    "missing fork session",
			stdout:  `{"text":"answer","stopReason":"EndTurn"}`,
			wantErr: "fork session id",
		},
		{
			name:    "source session reused",
			stdout:  `{"text":"answer","stopReason":"EndTurn","sessionId":"` + sourceSessionID + `"}`,
			wantErr: "unexpected fork session id",
		},
		{
			name:    "unexpected fork session",
			stdout:  `{"text":"answer","stopReason":"EndTurn","sessionId":"6211ea92-c582-4a27-b067-8ed5ba92348d"}`,
			wantErr: "unexpected fork session id",
		},
		{
			name:       "cleanup failure",
			stdout:     `{"text":"answer","stopReason":"EndTurn","sessionId":"` + forkSessionID + `"}`,
			cleanupErr: true,
			wantErr:    "remove disposable Grok Build fork",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanupCalls := 0
			client := NewClient(
				sourceSessionID,
				"",
				WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
				WithIsolationFactory(testIsolationFactory(t)),
				WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
					if len(args) > 0 && args[0] == "sessions" {
						cleanupCalls++
						if test.cleanupErr {
							return helperCommand(ctx, "", "cleanup failed", 9, 0)
						}
						return helperCommand(ctx, "", "", 0, 0)
					}
					return helperCommand(ctx, test.stdout, test.stderr, test.exit, 0)
				}),
			)
			_, err := client.AskPrompt(context.Background(), "prompt")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
			if cleanupCalls != 1 {
				t.Fatalf("cleanup calls = %d, want one", cleanupCalls)
			}
			if test.cleanupErr && !errors.Is(err, ErrClientUnusable) {
				t.Fatalf("cleanup failure must mark client unusable, got %v", err)
			}
			if test.cleanupErr && !strings.Contains(err.Error(), forkSessionID) {
				t.Fatalf("cleanup failure must identify the disposable fork: %v", err)
			}
		})
	}
}

func TestClientCleansIncompleteIsolation(t *testing.T) {
	cleaned := false
	client := NewClient(
		sourceSessionID,
		"",
		WithIsolationFactory(func(context.Context, string) (*Isolation, error) {
			return &Isolation{
				HomeDir:  "/isolated/home",
				GrokHome: "/isolated/home/.grok",
				Cleanup: func() error {
					cleaned = true
					return nil
				},
			}, nil
		}),
	)
	if _, err := client.AskPrompt(context.Background(), "prompt"); err == nil ||
		!strings.Contains(err.Error(), "incomplete environment") {
		t.Fatalf("expected incomplete-isolation error, got %v", err)
	}
	if !cleaned {
		t.Fatal("incomplete isolation was not cleaned")
	}
}

func TestClientDeletesForkWhenRelocationFailsAfterCreation(t *testing.T) {
	cleanupCalls := 0
	factory := testIsolationFactory(t)
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(func(ctx context.Context, sessionID string) (*Isolation, error) {
			isolation, err := factory(ctx, sessionID)
			if err != nil {
				return nil, err
			}
			isolation.Relocate = func(context.Context, string, string) (bool, error) {
				return true, errors.New("metadata rewrite failed")
			}
			return isolation, nil
		}),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				cleanupCalls++
			}
			return helperCommand(ctx, "", "", 0, 0)
		}),
	)
	if _, err := client.AskPrompt(context.Background(), "prompt"); err == nil ||
		!strings.Contains(err.Error(), "metadata rewrite failed") {
		t.Fatalf("expected relocation error, got %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want one", cleanupCalls)
	}
}

func TestClientRejectsSuccessfulDeleteThatLeavesForkStorage(t *testing.T) {
	factory := testIsolationFactory(t)
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(func(ctx context.Context, sessionID string) (*Isolation, error) {
			isolation, err := factory(ctx, sessionID)
			if err != nil {
				return nil, err
			}
			isolation.Relocate = func(_ context.Context, _, childID string) (bool, error) {
				child := filepath.Join(isolation.GrokHome, "sessions", "namespace", childID)
				if err := os.MkdirAll(child, 0o700); err != nil {
					return false, err
				}
				if err := os.WriteFile(filepath.Join(child, "summary.json"), []byte(`{}`), 0o600); err != nil {
					return false, err
				}
				return true, nil
			}
			return isolation, nil
		}),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				return helperCommand(ctx, "", "", 0, 0)
			}
			return helperCommand(
				ctx,
				`{"text":"answer","stopReason":"EndTurn","sessionId":"`+forkSessionID+`"}`,
				"",
				0,
				0,
			)
		}),
	)
	_, err := client.AskPrompt(context.Background(), "prompt")
	if !errors.Is(err, ErrClientUnusable) || !strings.Contains(err.Error(), "remains after deletion") {
		t.Fatalf("unverified deletion should mark the client unusable: %v", err)
	}
}

func TestClientCancellationDuringIsolationReleasesSerializedGate(t *testing.T) {
	started := make(chan struct{}, 1)
	client := NewClient(
		sourceSessionID,
		"",
		WithIsolationFactory(func(ctx context.Context, _ string) (*Isolation, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.AskPrompt(ctx, "first")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled staging, got %v", err)
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer secondCancel()
	if _, err := client.AskPrompt(secondCtx, "second"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("serialized gate remained held after staging cancellation: %v", err)
	}
}

func TestBoundedBufferStopsAtLimit(t *testing.T) {
	buffer := newBoundedBuffer(4)
	if _, err := buffer.Write([]byte("abcdef")); !errors.Is(err, errProcessOutputLimit) {
		t.Fatalf("expected output limit error, got %v", err)
	}
	if got := buffer.String(); got != "abcd" {
		t.Fatalf("bounded output = %q, want %q", got, "abcd")
	}
}

func TestClientCancellationAndCleanupFailureMarksClientUnusable(t *testing.T) {
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(testIsolationFactory(t)),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				return helperCommand(ctx, "", "cleanup failed", 9, 0)
			}
			return helperCommand(ctx, "", "", 0, 10*time.Second)
		}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.AskPrompt(ctx, "prompt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error to remain discoverable, got %v", err)
	}
	if !errors.Is(err, ErrClientUnusable) {
		t.Fatalf("expected failed cleanup to mark client unusable, got %v", err)
	}
}

func TestClientValidatesInputsAndForkIDGeneration(t *testing.T) {
	if _, err := (*Client)(nil).AskPrompt(context.Background(), "prompt"); err == nil {
		t.Fatal("expected nil-client error")
	}
	if _, err := NewClient("", "").AskPrompt(context.Background(), "prompt"); err == nil {
		t.Fatal("expected missing-session error")
	}
	if _, err := NewClient(sourceSessionID, "").AskPrompt(context.Background(), " "); err == nil {
		t.Fatal("expected empty-prompt error")
	}
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return "", errors.New("entropy unavailable") }),
	)
	if _, err := client.AskPrompt(context.Background(), "prompt"); err == nil || !strings.Contains(err.Error(), "entropy unavailable") {
		t.Fatalf("expected fork-id error, got %v", err)
	}

	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '4' || !strings.Contains("89ab", string(id[19])) {
		t.Fatalf("invalid UUIDv4 %q", id)
	}
}

func TestClientCancellationWaitsForProcessCleanup(t *testing.T) {
	var command *exec.Cmd
	cleanupCalls := 0
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(testIsolationFactory(t)),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				cleanupCalls++
				return helperCommand(ctx, "", "", 0, 0)
			}
			command = helperCommand(ctx, "", "", 0, 10*time.Second)
			return command
		}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.AskPrompt(ctx, "prompt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if command == nil || command.ProcessState == nil {
		t.Fatal("expected command.Run to wait and reap the canceled process")
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want one", cleanupCalls)
	}
}

func TestClientSerializesOperationsAndCanceledWaiterStopsWaiting(t *testing.T) {
	invoked := make(chan int, 3)
	var mu sync.Mutex
	call := 0
	client := NewClient(
		sourceSessionID,
		"",
		WithForkIDGenerator(func() (string, error) { return forkSessionID, nil }),
		WithIsolationFactory(testIsolationFactory(t)),
		WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if len(args) > 0 && args[0] == "sessions" {
				return helperCommand(ctx, "", "", 0, 0)
			}
			mu.Lock()
			call++
			current := call
			mu.Unlock()
			invoked <- current
			if current == 1 {
				return helperCommand(ctx, "", "", 0, 10*time.Second)
			}
			return helperCommand(ctx,
				`{"text":"second answer","stopReason":"EndTurn","sessionId":"`+forkSessionID+`"}`,
				"",
				0,
				0,
			)
		}),
	)

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.AskPrompt(firstCtx, "first")
		firstDone <- err
	}()
	if got := receiveInvocation(t, invoked); got != 1 {
		t.Fatalf("expected first invocation, got %d", got)
	}

	waitingCtx, cancelWaiting := context.WithCancel(context.Background())
	waitingDone := make(chan error, 1)
	go func() {
		_, err := client.AskPrompt(waitingCtx, "canceled waiter")
		waitingDone <- err
	}()
	select {
	case got := <-invoked:
		t.Fatalf("operation %d started while the first was still running", got)
	case <-time.After(40 * time.Millisecond):
	}
	cancelWaiting()
	if err := <-waitingDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected waiting operation cancellation, got %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		answer, err := client.AskPrompt(context.Background(), "second")
		if err == nil && answer != "second answer" {
			err = fmt.Errorf("unexpected second answer %q", answer)
		}
		secondDone <- err
	}()
	select {
	case got := <-invoked:
		t.Fatalf("operation %d started while the first was still running", got)
	case <-time.After(40 * time.Millisecond):
	}

	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected first operation cancellation, got %v", err)
	}
	if got := receiveInvocation(t, invoked); got != 2 {
		t.Fatalf("expected second invocation after first cleanup, got %d", got)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func receiveInvocation(t *testing.T, invoked <-chan int) int {
	t.Helper()
	select {
	case got := <-invoked:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command invocation")
		return 0
	}
}

func TestStageIsolatedSessionCopiesOnlyConversationState(t *testing.T) {
	sourceHome := t.TempDir()
	t.Setenv("GROK_HOME", sourceHome)
	sourceWorkspaceRoot := t.TempDir()
	if output, err := exec.Command("git", "-C", sourceWorkspaceRoot, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize source workspace: %v: %s", err, output)
	}
	sourceWorkspace := filepath.Join(sourceWorkspaceRoot, "services", "api")
	if err := os.MkdirAll(sourceWorkspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceWorkspace, "service.go"), []byte("package service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceWorkspace, ".grok", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceWorkspace, ".grok", "hooks", "unsafe.json"), []byte(`{"hooks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := filepath.Join(sourceHome, "sessions", "%2Frepo", sourceSessionID)
	if err := os.MkdirAll(sourceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	summary := `{"info":{"id":"` + sourceSessionID + `","cwd":` + strconv.Quote(sourceWorkspace) + `},"current_model_id":"grok-4.5"}`
	for name, content := range map[string]string{
		"summary.json":       summary,
		"chat_history.jsonl": `{"role":"user","content":"context"}` + "\n",
		"events.jsonl.lock":  "stale lock",
	} {
		if err := os.WriteFile(filepath.Join(sourceDirectory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sourceHome, "auth.json"), []byte(`{"scope":{"token":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceHome, "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, "hooks", "unsafe.json"), []byte(`{"hooks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	isolation, err := stageIsolatedSession(context.Background(), sourceSessionID)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(isolation.HomeDir)
	t.Cleanup(func() { _ = isolation.Cleanup() })
	staged := filepath.Join(isolation.GrokHome, "sessions", "%2Frepo", sourceSessionID)
	if content, err := os.ReadFile(filepath.Join(staged, "chat_history.jsonl")); err != nil ||
		!strings.Contains(string(content), "context") {
		t.Fatalf("conversation context was not staged: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(staged, "events.jsonl.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source lock must not be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(isolation.GrokHome, "hooks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hooks leaked into isolated Grok home: %v", err)
	}
	if profile, err := os.ReadFile(filepath.Join(isolation.GrokHome, "sandbox.toml")); err != nil ||
		!strings.Contains(string(profile), "[profiles."+isolatedSandboxProfile+"]") ||
		!strings.Contains(string(profile), isolation.SourceRoot) {
		t.Fatalf("fail-closed sandbox profile missing: content=%q err=%v", profile, err)
	}
	if _, err := os.Stat(filepath.Join(isolation.WorkingDirectory, ".grok")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project hooks leaked into isolated workspace: %v", err)
	}
	if filepath.Base(isolation.WorkingDirectory) != "api" ||
		filepath.Base(filepath.Dir(isolation.WorkingDirectory)) != "services" {
		t.Fatalf("isolated cwd %q did not preserve the parent session's relative subdirectory", isolation.WorkingDirectory)
	}
	if content, err := os.ReadFile(filepath.Join(isolation.WorkingDirectory, "service.go")); err != nil ||
		string(content) != "package service\n" {
		t.Fatalf("workspace source was not snapshotted: content=%q err=%v", content, err)
	}
	if err := os.WriteFile(filepath.Join(isolation.WorkingDirectory, "service.go"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(sourceWorkspace, "service.go")); err != nil ||
		string(content) != "package service\n" {
		t.Fatalf("isolated workspace mutation reached the parent: content=%q err=%v", content, err)
	}
	var stagedSummary map[string]any
	content, err := os.ReadFile(filepath.Join(staged, "summary.json"))
	if err != nil || json.Unmarshal(content, &stagedSummary) != nil {
		t.Fatalf("read staged summary: content=%q err=%v", content, err)
	}
	info := stagedSummary["info"].(map[string]any)
	if info["cwd"] != sourceWorkspace {
		t.Fatalf("raw staged source cwd=%q want parent workspace %q", info["cwd"], sourceWorkspace)
	}
	if auth, err := os.ReadFile(isolation.AuthPath); err != nil || !strings.Contains(string(auth), "token") {
		t.Fatalf("authentication state was not isolated: content=%q err=%v", auth, err)
	}
	if err := isolation.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("isolated home remains after cleanup: %v", err)
	}
}

func TestSnapshotWorkspaceExcludesAgentConfigurationAndEscapingSymlinks(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	for _, relative := range []string{
		filepath.Join(".grok", "hooks", "unsafe.json"),
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".agents", "skills", "unsafe.md"),
		filepath.Join(".cursor", "rules", "unsafe.md"),
		filepath.Join(".direnv", "environment"),
		".mcp.json",
		".envrc",
		"AGENTS.md",
		"CLAUDE.local.md",
	} {
		path := filepath.Join(source, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("must not copy"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "plan.md"), []byte("# Plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "escape")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	if _, _, err := snapshotWorkspace(context.Background(), source, destination); err == nil ||
		!strings.Contains(err.Error(), "symlinks are not copied") {
		t.Fatalf("expected workspace symlink rejection, got %v", err)
	}
	if err := os.Remove(filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	destination = t.TempDir()
	isolatedCWD, _, err := snapshotWorkspace(context.Background(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(isolatedCWD, "plan.md")); err != nil ||
		string(content) != "# Plan\n" {
		t.Fatalf("ordinary source missing from snapshot: content=%q err=%v", content, err)
	}
	for _, name := range []string{
		".grok", ".claude", ".agents", ".cursor", ".direnv", ".mcp.json", ".envrc", "AGENTS.md", "CLAUDE.local.md",
	} {
		if _, err := os.Stat(filepath.Join(isolatedCWD, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s configuration leaked into snapshot: %v", name, err)
		}
	}
}

func TestStageIsolatedSessionRejectsMissingSessionsAndSymlinks(t *testing.T) {
	sourceHome := t.TempDir()
	t.Setenv("GROK_HOME", sourceHome)
	if err := os.MkdirAll(filepath.Join(sourceHome, "sessions", "%2Frepo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := stageIsolatedSession(context.Background(), sourceSessionID); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected missing-session error, got %v", err)
	}

	actual := filepath.Join(sourceHome, "actual")
	if err := os.MkdirAll(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actual, "summary.json"), []byte(`{"info":{"cwd":"/repo"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sourceHome, "sessions", "%2Frepo", sourceSessionID)
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := stageIsolatedSession(context.Background(), sourceSessionID); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected symlinked-session rejection, got %v", err)
	}
}

func TestSessionDirectorySignatureDetectsConcurrentChange(t *testing.T) {
	session := t.TempDir()
	history := filepath.Join(session, "chat_history.jsonl")
	if err := os.WriteFile(history, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := sessionDirectorySignature(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history, []byte("second turn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := sessionDirectorySignature(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if sessionSignaturesEqual(before, after) {
		t.Fatal("session mutation was not detected")
	}
}

func TestIsolatedEnvironmentScrubsHostConfiguration(t *testing.T) {
	isolation := &Isolation{
		HomeDir:          "/isolated/home",
		GrokHome:         "/isolated/home/.grok",
		WorkspaceRoot:    "/isolated/workspace",
		WorkingDirectory: "/isolated/workspace",
		SourceRoot:       "/real/workspace",
		AuthPath:         "/isolated/home/.grok/auth.json",
	}
	environment := isolatedEnvironment([]string{
		"HOME=/real/home",
		"USERPROFILE=C:\\real",
		"XDG_CONFIG_HOME=/real/config",
		"CLAUDE_CONFIG_DIR=/real/claude",
		"GROK_HOME=/real/grok",
		"GROK_AUTH_PATH=/real/grok/auth.json",
		"GROK_AGENT=/real/agent",
		"GROK_AUTH_PROVIDER_COMMAND=/real/auth-helper",
		"GROK_LOG_FILE=/real/grok.log",
		"GROK_WORKSPACE_HOME=/real/workspace",
		"DYLD_INSERT_LIBRARIES=/real/inject.dylib",
		"XAI_API_KEY=test-key",
	}, isolation)
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{
		"/real/home", "C:\\real", "/real/config", "/real/claude", "/real/grok",
		"/real/agent", "/real/auth-helper", "/real/workspace", "/real/inject.dylib",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("host configuration leaked into isolated environment: %s", joined)
		}
	}
	for _, required := range []string{
		"HOME=/isolated/home",
		"GROK_HOME=/isolated/home/.grok",
		"GROK_AUTH_PATH=/isolated/home/.grok/auth.json",
		"CLAUDE_CONFIG_DIR=/isolated/home/.claude",
		"XAI_API_KEY=test-key",
		"GROK_DISABLE_AUTOUPDATER=1",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("isolated environment missing %q: %s", required, joined)
		}
	}
}

func TestValidateIsolatedInspectionRejectsExecutableConfiguration(t *testing.T) {
	clean := []byte(`{
		"projectInstructions": [],
		"hooks": [],
		"skills": [],
		"plugins": [],
		"marketplaces": [],
		"mcpServers": [],
		"lspServers": [],
		"agents": [{"source":{"type":"builtin"}}],
		"permissions": {"managedSettingsActive":false},
		"configSources": {"layers":[]}
	}`)
	if err := validateIsolatedInspection(clean); err != nil {
		t.Fatalf("clean isolated inspection rejected: %v", err)
	}
	for _, field := range []string{
		"projectInstructions", "hooks", "skills", "plugins", "marketplaces", "mcpServers", "lspServers",
	} {
		hostile := bytes.Replace(clean, []byte(`"`+field+`": []`), []byte(`"`+field+`": [{}]`), 1)
		if err := validateIsolatedInspection(hostile); err == nil {
			t.Fatalf("inspection with %s was accepted", field)
		}
	}
	customAgent := bytes.Replace(clean, []byte(`"type":"builtin"`), []byte(`"type":"user"`), 1)
	if err := validateIsolatedInspection(customAgent); err == nil {
		t.Fatal("inspection with a custom agent was accepted")
	}
	managed := bytes.Replace(clean, []byte(`"managedSettingsActive":false`), []byte(`"managedSettingsActive":true`), 1)
	if err := validateIsolatedInspection(managed); err == nil {
		t.Fatal("inspection with managed settings was accepted")
	}
	if err := validateIsolatedInspection([]byte(`not-json`)); err == nil {
		t.Fatal("malformed inspection was accepted")
	}
	for _, incomplete := range []string{
		`null`,
		`{}`,
		`{"projectInstructions":[]}`,
		`{"projectInstructions":[],"hooks":[],"skills":[],"plugins":[],"marketplaces":[],"mcpServers":[],"lspServers":[],"agents":[],"permissions":{},"configSources":{"layers":[]}}`,
		`{"projectInstructions":[],"hooks":[],"skills":[],"plugins":[],"marketplaces":[],"mcpServers":[],"lspServers":[],"agents":[],"permissions":{"managedSettingsActive":false},"configSources":{}}`,
	} {
		if err := validateIsolatedInspection([]byte(incomplete)); err == nil {
			t.Fatalf("incomplete inspection was accepted: %s", incomplete)
		}
	}
}

func TestIsolatedPromptRewritesOriginalAbsolutePaths(t *testing.T) {
	isolation := &Isolation{
		WorkspaceRoot:    "/tmp/isolated/workspace",
		WorkingDirectory: "/tmp/isolated/workspace/services/api",
		SourceRoot:       "/repo",
	}
	prompt := isolatedPrompt(isolation, "Read /repo/services/api/service.go.")
	if strings.Contains(prompt, "/repo/services") ||
		!strings.Contains(prompt, "/tmp/isolated/workspace/services/api/service.go") {
		t.Fatalf("original workspace path was not rewritten:\n%s", prompt)
	}
}

func TestRewriteChildSessionPathsIncludesRepositoryRootOutsideNestedCWD(t *testing.T) {
	session := t.TempDir()
	summary := filepath.Join(session, "summary.json")
	history := filepath.Join(session, "chat_history.jsonl")
	if err := os.WriteFile(
		summary,
		[]byte(`{"info":{"cwd":"/repo/services/api"},"git_root_dir":"/repo/","turn":9007199254740993}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		history,
		[]byte("{\"role\":\"user\",\"content\":\"Compare /repo/README.md with /repo/services/api/service.go\"}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := rewriteChildSessionPaths(
		context.Background(),
		session,
		"/repo",
		"/isolated/workspace",
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{summary, history} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "/repo/") ||
			!strings.Contains(string(content), "/isolated/workspace/") {
			t.Fatalf("repository-root path was not relocated in %s:\n%s", path, content)
		}
	}
	content, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "9007199254740993") {
		t.Fatalf("JSON number precision changed: %s", content)
	}
}

func testIsolationFactory(t *testing.T) IsolationFactory {
	t.Helper()
	parent := t.TempDir()
	return func(context.Context, string) (*Isolation, error) {
		root, err := os.MkdirTemp(parent, "run-*")
		if err != nil {
			return nil, err
		}
		home := filepath.Join(root, "home")
		grokHome := filepath.Join(home, ".grok")
		workspace := filepath.Join(root, "workspace")
		for _, path := range []string{grokHome, workspace, filepath.Join(root, "tmp")} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				_ = os.RemoveAll(root)
				return nil, err
			}
		}
		return &Isolation{
			HomeDir:                home,
			GrokHome:               grokHome,
			WorkspaceRoot:          workspace,
			WorkingDirectory:       workspace,
			SourceRoot:             "/source/workspace",
			SourceWorkingDirectory: "/source/workspace",
			Relocate: func(context.Context, string, string) (bool, error) {
				return true, nil
			},
			Cleanup: func() error { return os.RemoveAll(root) },
		}, nil
	}
}

func argumentValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for index, argument := range args {
		if argument == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	t.Fatalf("missing %s in %q", flag, args)
	return ""
}

func helperCommand(ctx context.Context, stdout, stderr string, exitCode int, delay time.Duration) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestGrokBuildHelperProcess", "--")
	command.Env = append(os.Environ(),
		helperProcessEnvironment+"=1",
		"PLANMAXX_GROKBUILD_STDOUT="+stdout,
		"PLANMAXX_GROKBUILD_STDERR="+stderr,
		"PLANMAXX_GROKBUILD_EXIT="+strconv.Itoa(exitCode),
		"PLANMAXX_GROKBUILD_DELAY="+delay.String(),
	)
	return command
}

func TestGrokBuildHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnvironment) != "1" {
		return
	}
	if delay, err := time.ParseDuration(os.Getenv("PLANMAXX_GROKBUILD_DELAY")); err == nil && delay > 0 {
		time.Sleep(delay)
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("PLANMAXX_GROKBUILD_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("PLANMAXX_GROKBUILD_STDERR"))
	exitCode, _ := strconv.Atoi(os.Getenv("PLANMAXX_GROKBUILD_EXIT"))
	os.Exit(exitCode)
}
