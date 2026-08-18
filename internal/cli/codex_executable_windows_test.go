//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCodexDesktopCandidatesFindUserOwnedRuntime(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	want := filepath.Join(localAppData, "OpenAI", "Codex", "bin", "runtime-id", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("fake"), 0o700); err != nil {
		t.Fatal(err)
	}

	candidates := codexDesktopExecutableCandidates()
	if len(candidates) != 1 || candidates[0] != want {
		t.Fatalf("desktop candidates = %#v, want %q", candidates, want)
	}
}
