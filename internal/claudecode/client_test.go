package claudecode

import (
	"context"
	"errors"
	"fmt"
	"io"
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

const helperProcessEnvironment = "PLANMAXX_CLAUDECODE_HELPER"

type commandInvocation struct {
	name string
	args []string
	cmd  *exec.Cmd
}

func TestClientAskUsesConfiguredSessionAndSideQuestionPrompt(t *testing.T) {
	var invocation commandInvocation
	stdinPath := filepath.Join(t.TempDir(), "stdin")
	factory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		command := helperCommand(ctx,
			`{"type":"result","subtype":"success","is_error":false,"result":"Use the CLI first.","session_id":"forked-session"}`,
			"",
			0,
			0,
		)
		command.Env = append(command.Env, "PLANMAXX_CLAUDECODE_STDIN_PATH="+stdinPath)
		invocation = commandInvocation{name: name, args: append([]string(nil), args...), cmd: command}
		return command
	}
	workingDirectory := t.TempDir()
	client := NewClient("configured-session", workingDirectory, WithCommandFactory(factory))

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
	if invocation.name != "claude" {
		t.Fatalf("expected claude executable, got %q", invocation.name)
	}
	if invocation.cmd.Dir != workingDirectory {
		t.Fatalf("expected working directory %q, got %q", workingDirectory, invocation.cmd.Dir)
	}
	if len(invocation.args) != 12 {
		t.Fatalf("unexpected arguments: %#v", invocation.args)
	}
	promptBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read Claude stdin: %v", err)
	}
	prompt := string(promptBytes)
	for _, want := range []string{
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
	wantArgs := []string{
		"-p",
		"--resume", "configured-session",
		"--fork-session",
		"--safe-mode",
		"--no-session-persistence",
		"--output-format", "json",
		"--tools", "",
		"--permission-mode", "dontAsk",
	}
	if !reflect.DeepEqual(invocation.args, wantArgs) {
		t.Fatalf("unexpected arguments\nwant: %#v\n got: %#v", wantArgs, invocation.args)
	}
}

func TestClientAskPromptUsesConfiguredSession(t *testing.T) {
	var gotArgs []string
	stdinPath := filepath.Join(t.TempDir(), "stdin")
	client := NewClient("current-session", "", WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = append([]string(nil), args...)
		command := helperCommand(ctx,
			`{"type":"result","subtype":"success","is_error":false,"result":"<proposal />","session_id":"forked-session"}`,
			"",
			0,
			0,
		)
		command.Env = append(command.Env, "PLANMAXX_CLAUDECODE_STDIN_PATH="+stdinPath)
		return command
	}))

	answer, err := client.AskPrompt(context.Background(), "Rewrite the selected section.")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "<proposal />" {
		t.Fatalf("unexpected answer %q", answer)
	}
	promptBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read Claude stdin: %v", err)
	}
	if string(promptBytes) != "Rewrite the selected section." {
		t.Fatalf("expected raw section prompt, got %q", promptBytes)
	}
	if gotArgs[2] != "current-session" {
		t.Fatalf("expected configured session, got %q", gotArgs[2])
	}
}

func TestClientRejectsUnsuccessfulOrInvalidResponses(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		exit    int
		wantErr string
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
			wantErr: "decode Claude Code response",
		},
		{
			name:    "wrong response type",
			stdout:  `{"type":"system","subtype":"success","result":"answer"}`,
			wantErr: "response type",
		},
		{
			name:    "error result",
			stdout:  `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"partial"}`,
			stderr:  "tool failed",
			wantErr: "error_during_execution",
		},
		{
			name:    "empty result",
			stdout:  `{"type":"result","subtype":"success","is_error":false,"result":"  "}`,
			wantErr: "empty result",
		},
		{
			name:    "missing fork session",
			stdout:  `{"type":"result","subtype":"success","is_error":false,"result":"answer"}`,
			wantErr: "fork session id",
		},
		{
			name:    "source session reused",
			stdout:  `{"type":"result","subtype":"success","is_error":false,"result":"answer","session_id":"session"}`,
			wantErr: "reused the source session",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("session", "", WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return helperCommand(ctx, test.stdout, test.stderr, test.exit, 0)
			}))
			_, err := client.AskPrompt(context.Background(), "prompt")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
			if test.stderr != "" && !strings.Contains(err.Error(), test.stderr) {
				t.Fatalf("expected stderr %q in error, got %v", test.stderr, err)
			}
		})
	}
}

func TestClientCancellationWaitsForProcessCleanup(t *testing.T) {
	var command *exec.Cmd
	client := NewClient("session", "", WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		command = helperCommand(ctx, "", "", 0, 10*time.Second)
		return command
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.AskPrompt(ctx, "prompt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if command == nil || command.ProcessState == nil {
		t.Fatal("expected command.Run to wait and reap the canceled process")
	}
}

func TestClientSerializesOperationsAndCanceledWaiterStopsWaiting(t *testing.T) {
	invoked := make(chan int, 3)
	var mu sync.Mutex
	call := 0
	client := NewClient("session", "", WithCommandFactory(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		call++
		current := call
		mu.Unlock()
		invoked <- current
		if current == 1 {
			return helperCommand(ctx, "", "", 0, 10*time.Second)
		}
		return helperCommand(ctx,
			`{"type":"result","subtype":"success","is_error":false,"result":"second answer","session_id":"forked-session"}`,
			"",
			0,
			0,
		)
	}))

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

	secondDone := make(chan struct {
		answer string
		err    error
	}, 1)
	go func() {
		answer, err := client.AskPrompt(context.Background(), "second")
		secondDone <- struct {
			answer string
			err    error
		}{answer: answer, err: err}
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
	result := <-secondDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.answer != "second answer" {
		t.Fatalf("unexpected second answer %q", result.answer)
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

func helperCommand(ctx context.Context, stdout, stderr string, exitCode int, delay time.Duration) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestClaudeCodeHelperProcess", "--")
	command.Env = append(os.Environ(),
		helperProcessEnvironment+"=1",
		"PLANMAXX_CLAUDECODE_STDOUT="+stdout,
		"PLANMAXX_CLAUDECODE_STDERR="+stderr,
		"PLANMAXX_CLAUDECODE_EXIT="+strconv.Itoa(exitCode),
		"PLANMAXX_CLAUDECODE_DELAY="+delay.String(),
	)
	return command
}

func TestClaudeCodeHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnvironment) != "1" {
		return
	}
	if delay, err := time.ParseDuration(os.Getenv("PLANMAXX_CLAUDECODE_DELAY")); err == nil && delay > 0 {
		time.Sleep(delay)
	}
	input, _ := io.ReadAll(os.Stdin)
	if path := os.Getenv("PLANMAXX_CLAUDECODE_STDIN_PATH"); path != "" {
		_ = os.WriteFile(path, input, 0o600)
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("PLANMAXX_CLAUDECODE_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("PLANMAXX_CLAUDECODE_STDERR"))
	exitCode, _ := strconv.Atoi(os.Getenv("PLANMAXX_CLAUDECODE_EXIT"))
	os.Exit(exitCode)
}
