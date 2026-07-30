package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveClaudeTestEnvironment = "PLANMAXX_RUN_LIVE_CLAUDE"

func TestLiveClaudeSonnetForkRetainsExactContext(t *testing.T) {
	if os.Getenv(liveClaudeTestEnvironment) != "1" {
		t.Skip("set " + liveClaudeTestEnvironment + "=1 to run the short live Claude Code check")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude executable is unavailable")
	}
	sessionID, err := newLiveClaudeUUID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeLiveClaudeSession(t, sessionID) })
	token := "JADE-" + strings.ToUpper(sessionID[:8])
	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	source := exec.CommandContext(
		ctx,
		"claude",
		"-p",
		"--model", "sonnet",
		"--session-id", sessionID,
		"--safe-mode",
		"--output-format", "json",
		"--tools", "",
		"--permission-mode", "dontAsk",
		"Remember token "+token+". Reply only READY.",
	)
	source.Dir = workspace
	output, err := source.CombinedOutput()
	if err != nil {
		t.Fatalf("create live Claude Sonnet session: %v: %s", err, output)
	}
	var created struct {
		Type       string                     `json:"type"`
		Subtype    string                     `json:"subtype"`
		IsError    bool                       `json:"is_error"`
		SessionID  string                     `json:"session_id"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if err := json.Unmarshal(output, &created); err != nil {
		t.Fatalf("decode live Claude source response: %v: %s", err, output)
	}
	if created.Type != "result" || created.Subtype != "success" || created.IsError ||
		created.SessionID != sessionID {
		t.Fatalf("unexpected live Claude source response: %+v", created)
	}
	usedSonnet := false
	for model := range created.ModelUsage {
		if strings.Contains(strings.ToLower(model), "sonnet") {
			usedSonnet = true
		}
	}
	if !usedSonnet {
		t.Fatalf("live Claude source did not report Sonnet usage: %v", created.ModelUsage)
	}

	answer, err := NewClient(sessionID, workspace).AskPrompt(
		ctx,
		"Reply only with the verification token the user asked you to remember in the previous turn.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(answer) != token {
		t.Fatalf("Claude fork lost source context: got %q want %q", answer, token)
	}
}

func newLiveClaudeUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 32)
	hex.Encode(encoded, value[:])
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		encoded[:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:],
	), nil
}

func removeLiveClaudeSession(t *testing.T, sessionID string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Errorf("locate Claude home for live-session cleanup: %v", err)
		return
	}
	projects := filepath.Join(home, ".claude", "projects")
	targetName := sessionID + ".jsonl"
	err = filepath.WalkDir(projects, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == targetName {
			return os.Remove(path)
		}
		return nil
	})
	if err != nil {
		t.Errorf("remove live Claude test session %s: %v", sessionID, err)
	}
}
