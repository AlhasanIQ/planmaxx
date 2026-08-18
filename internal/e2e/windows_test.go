//go:build windows

package e2e

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsCodexExecutableOverrideEndToEnd(t *testing.T) {
	root := repoRoot(t)
	fakeCodex := filepath.Join(t.TempDir(), "codex.exe")
	build := exec.Command("go", "build", "-o", fakeCodex, "./internal/e2e/testdata/fakecodex")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Codex executable: %v\n%s", err, output)
	}

	review := startReviewWithArgs(t, realisticPlan("Windows Codex executable"), []string{
		"--no-browser",
		"--codex-bin", fakeCodex,
	}, map[string]string{
		"CODEX_THREAD_ID": "windows-thread",
	})
	state := getState(t, review.url)
	if state.Agent.ID != "codex" || !state.Agent.Available || state.Agent.ContextMode != "current-session-fork" {
		t.Fatalf("unexpected Windows Codex attachment: %+v\nstderr:\n%s", state.Agent, review.stderr.String())
	}
	thread := createThread(t, review.url, 5, 5, "Ask Codex on Windows.")
	answer := askSideQuestion(t, review.url, thread.ID, "Is app-server online?", "1. Windows")
	if answer.Answer != "Windows app-server answer." {
		t.Fatalf("unexpected Windows app-server answer %q", answer.Answer)
	}
	finalize(t, review.url, digest("Windows integration approved", nil, nil))
	waitSuccess(t, review)
}
