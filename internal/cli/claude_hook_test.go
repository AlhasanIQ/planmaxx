package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyClaudeSessionHookExportsValidatedSession(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "claude-env")
	const sessionID = "6211ea92-c582-4a27-b067-8ed5ba92348d"

	if err := writeClaudeSessionEnvironment(strings.NewReader(`{"session_id":"`+sessionID+`","source":"startup"}`), envFile); err != nil {
		t.Fatalf("write hook environment: %v", err)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "PLANMAXX_AGENT") {
		t.Fatalf("hook must not override explicit provider selection: %q", got)
	}
	if !strings.Contains(got, "export PLANMAXX_CLAUDE_SESSION_ID='"+sessionID+"'\n") {
		t.Fatalf("session export missing: %q", got)
	}
}

func TestLegacyClaudeSessionHookRejectsUntrustedSessionID(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "claude-env")

	err := writeClaudeSessionEnvironment(strings.NewReader(`{"session_id":"' ; touch /tmp/planmaxx-bad ; '"}`), envFile)

	if err == nil || !strings.Contains(err.Error(), "invalid session_id") {
		t.Fatalf("expected invalid session error, got %v", err)
	}
	if _, statErr := os.Stat(envFile); !os.IsNotExist(statErr) {
		t.Fatalf("environment file should not be created, stat error = %v", statErr)
	}
}
