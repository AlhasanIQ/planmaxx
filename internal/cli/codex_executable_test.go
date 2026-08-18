package cli

import (
	"bytes"
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestResolveCodexExecutableCandidatesHonorsExplicitOverride(t *testing.T) {
	const explicit = `C:\Tools\Codex\codex.exe`
	if got := resolveCodexExecutableCandidates(explicit); !reflect.DeepEqual(got, []string{explicit}) {
		t.Fatalf("candidates = %#v", got)
	}
}

func TestStartCodexAppServerFallsBackAfterLaunchFailure(t *testing.T) {
	oldCandidates := findCodexExecutableCandidates
	oldCommand := execCommandContext
	findCodexExecutableCandidates = func(string) []string { return []string{"blocked-codex", "working-codex"} }
	var attempts []string
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		attempts = append(attempts, name)
		if name == "blocked-codex" {
			return exec.CommandContext(ctx, "planmaxx-missing-codex-for-fallback-test")
		}
		return fakeAppServerCommand(ctx, "codex", args...)
	}
	t.Cleanup(func() {
		findCodexExecutableCandidates = oldCandidates
		execCommandContext = oldCommand
	})

	process, err := startCodexAppServer(context.Background(), t.TempDir(), "", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	stopAppServerProcess(process.cmd, process.stdin)
	if !reflect.DeepEqual(attempts, []string{"blocked-codex", "working-codex"}) {
		t.Fatalf("launch attempts = %#v", attempts)
	}
}

func TestAttachCodexPublishesActionableLaunchFailure(t *testing.T) {
	oldCandidates := findCodexExecutableCandidates
	oldCommand := execCommandContext
	findCodexExecutableCandidates = func(string) []string { return []string{"blocked-codex"} }
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "planmaxx-missing-codex-for-diagnostic-test")
	}
	t.Cleanup(func() {
		findCodexExecutableCandidates = oldCandidates
		execCommandContext = oldCommand
	})

	server := review.NewServer(session.New("plan-1", "# Plan"))
	selection := agentSelection{id: agentCodex, displayName: "Codex", sessionID: "thread-1"}
	if cleanup := attachCodexAppServer(context.Background(), &bytes.Buffer{}, server, selection, t.TempDir(), ""); cleanup != nil {
		cleanup()
		t.Fatal("failed launch unexpectedly returned cleanup")
	}
	state := readAgentState(t, server)
	for _, want := range []string{"planmaxx-missing-codex-for-diagnostic-test", codexBinaryEnvironment, "--codex-bin"} {
		if !strings.Contains(state.UnavailableReason, want) {
			t.Fatalf("unavailable reason %q does not contain %q", state.UnavailableReason, want)
		}
	}
}

func TestDiagnoseCodexIntegrationChecksVersionAppServerAndThread(t *testing.T) {
	oldCandidates := findCodexExecutableCandidates
	oldCommand := execCommandContext
	findCodexExecutableCandidates = func(string) []string { return []string{"codex"} }
	execCommandContext = fakeAppServerCommand
	t.Cleanup(func() {
		findCodexExecutableCandidates = oldCandidates
		execCommandContext = oldCommand
	})

	report := diagnoseCodexIntegration(context.Background(), t.TempDir(), "", "current-thread")
	if report.CLIError != nil || report.AppServerError != nil || report.ThreadError != nil {
		t.Fatalf("diagnostic failed: %+v", report)
	}
	if report.Version != "codex-cli test" || !report.AppServerInitialized || !report.ThreadReadable {
		t.Fatalf("incomplete diagnostic: %+v", report)
	}
}
