package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

type fakeAttachedAgentClient struct{}

func (fakeAttachedAgentClient) Ask(context.Context, sidequestions.Request) (string, error) {
	return "answer", nil
}

func (fakeAttachedAgentClient) AskPrompt(context.Context, string) (string, error) {
	return "<planmaxx_proposal />", nil
}

type failingAttachedAgentClient struct {
	calls int
}

func (c *failingAttachedAgentClient) Ask(context.Context, sidequestions.Request) (string, error) {
	c.calls++
	return "", errors.New("provider session is invalid")
}

func (c *failingAttachedAgentClient) AskPrompt(context.Context, string) (string, error) {
	c.calls++
	return "", errors.New("provider session is invalid")
}

func TestResolveAgentSelectionUsesBareClaudeInvocationMarker(t *testing.T) {
	const officialSession = "6211ea92-c582-4a27-b067-8ed5ba92348d"
	const legacySession = "9c6954d4-9180-4876-bbfe-8592bad9a6d8"
	t.Setenv("PLANMAXX_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", officialSession)
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", legacySession)
	t.Setenv("CODEX_THREAD_ID", "codex-thread")

	selected, err := resolveAgentSelection(agentAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentClaude || selected.sessionID != officialSession {
		t.Fatalf("official Claude invocation marker should win automatic selection: %+v", selected)
	}
}

func TestResolveAgentSelectionPrefersExactInvocationSession(t *testing.T) {
	const invocationSession = "b3aa1215-0780-4342-88d2-49ad9f58587b"
	t.Setenv("PLANMAXX_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "6211ea92-c582-4a27-b067-8ed5ba92348d")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "9c6954d4-9180-4876-bbfe-8592bad9a6d8")
	t.Setenv("CODEX_THREAD_ID", "codex-thread")

	selected, err := resolveAgentSelection(agentAuto, invocationSession)
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentClaude || selected.sessionID != invocationSession {
		t.Fatalf("exact invocation session should win automatic selection: %+v", selected)
	}
}

func TestResolveAgentSelectionFallsBackToLegacyClaudeMarker(t *testing.T) {
	const legacySession = "9c6954d4-9180-4876-bbfe-8592bad9a6d8"
	t.Setenv("PLANMAXX_AGENT", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", legacySession)
	t.Setenv("CODEX_THREAD_ID", "codex-thread")

	selected, err := resolveAgentSelection(agentAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentClaude || selected.sessionID != legacySession {
		t.Fatalf("legacy Claude marker should remain above Codex automatic detection: %+v", selected)
	}
}

func TestResolveAgentSelectionPreservesExplicitProviderPrecedence(t *testing.T) {
	const officialSession = "6211ea92-c582-4a27-b067-8ed5ba92348d"
	t.Setenv("PLANMAXX_AGENT", agentCodex)
	t.Setenv("CLAUDE_CODE_SESSION_ID", officialSession)
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "9c6954d4-9180-4876-bbfe-8592bad9a6d8")
	t.Setenv("CODEX_THREAD_ID", "codex-thread")

	selected, err := resolveAgentSelection(agentAuto, "${CLAUDE_SESSION_ID}")
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentCodex || selected.sessionID != "codex-thread" {
		t.Fatalf("PLANMAXX_AGENT should override even an invalid invocation marker: %+v", selected)
	}

	const invocationSession = "b3aa1215-0780-4342-88d2-49ad9f58587b"
	selected, err = resolveAgentSelection(agentClaude, invocationSession)
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentClaude || selected.sessionID != invocationSession {
		t.Fatalf("explicit --agent should override PLANMAXX_AGENT: %+v", selected)
	}

	selected, err = resolveAgentSelection(agentNone, "${CLAUDE_SESSION_ID}")
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != agentNone {
		t.Fatalf("explicit none should override every environment marker: %+v", selected)
	}
}

func TestResolveAgentSelectionRejectsInvalidSelectedClaudeMarker(t *testing.T) {
	t.Run("unexpanded invocation placeholder", func(t *testing.T) {
		t.Setenv("PLANMAXX_AGENT", "")
		t.Setenv("CLAUDE_CODE_SESSION_ID", "6211ea92-c582-4a27-b067-8ed5ba92348d")
		if _, err := resolveAgentSelection(agentAuto, "${CLAUDE_SESSION_ID}"); err == nil || !strings.Contains(err.Error(), "--claude-session-id") {
			t.Fatalf("expected unexpanded invocation placeholder rejection, got %v", err)
		}
	})

	t.Run("official marker does not fall back to legacy", func(t *testing.T) {
		t.Setenv("PLANMAXX_AGENT", "")
		t.Setenv("CLAUDE_CODE_SESSION_ID", "not-a-claude-session")
		t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "9c6954d4-9180-4876-bbfe-8592bad9a6d8")
		if _, err := resolveAgentSelection(agentAuto, ""); err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_SESSION_ID") {
			t.Fatalf("expected invalid official marker rejection, got %v", err)
		}
	})

	t.Run("legacy marker", func(t *testing.T) {
		t.Setenv("PLANMAXX_AGENT", "")
		t.Setenv("CLAUDE_CODE_SESSION_ID", "")
		t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "not-a-claude-session")
		if _, err := resolveAgentSelection(agentAuto, ""); err == nil || !strings.Contains(err.Error(), "PLANMAXX_CLAUDE_SESSION_ID") {
			t.Fatalf("expected invalid legacy marker rejection, got %v", err)
		}
	})

	t.Run("explicit non-Claude ignores invalid marker", func(t *testing.T) {
		t.Setenv("PLANMAXX_AGENT", "")
		t.Setenv("CLAUDE_CODE_SESSION_ID", "not-a-claude-session")
		selected, err := resolveAgentSelection(agentNone, "")
		if err != nil || selected.id != agentNone {
			t.Fatalf("explicit non-Claude selection should ignore Claude markers: selected=%+v err=%v", selected, err)
		}
	})
}

func TestResolveAgentSelectionRejectsUnknownProvider(t *testing.T) {
	t.Setenv("PLANMAXX_AGENT", "")

	if _, err := resolveAgentSelection("mystery", ""); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestReviewCommandKeepsClaudeSessionFlagHidden(t *testing.T) {
	command := newReviewCommand(&bytes.Buffer{}, &bytes.Buffer{})
	flag := command.Flags().Lookup("claude-session-id")
	if flag == nil || !flag.Hidden {
		t.Fatalf("Claude invocation session flag must exist but remain hidden: %+v", flag)
	}
}

func TestRequireMinimumClaudeVersion(t *testing.T) {
	for _, supported := range []string{"2.1.214 (Claude Code)", "2.1.215", "v3.0.0"} {
		if err := requireMinimumClaudeVersion(supported); err != nil {
			t.Fatalf("expected %q to be supported: %v", supported, err)
		}
	}
	for _, unsupported := range []string{"", "Claude Code", "2.1.213", "1.9.999"} {
		if err := requireMinimumClaudeVersion(unsupported); err == nil {
			t.Fatalf("expected %q to be rejected", unsupported)
		}
	}
}

func TestAttachClaudeServicesPublishesServerAuthoritativeCapabilities(t *testing.T) {
	const sessionID = "6211ea92-c582-4a27-b067-8ed5ba92348d"
	t.Setenv("CLAUDE_CODE_SESSION_ID", "b3aa1215-0780-4342-88d2-49ad9f58587b")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "9c6954d4-9180-4876-bbfe-8592bad9a6d8")
	t.Setenv("PLANMAXX_AGENT", "")

	oldFactory := newClaudeClient
	oldLookPath := agentLookPath
	oldCapabilities := checkClaudeCapabilities
	var gotSessionID, gotCWD string
	agentLookPath = func(file string) (string, error) { return "/test/bin/" + file, nil }
	checkClaudeCapabilities = func(context.Context, string) error { return nil }
	newClaudeClient = func(sessionID, cwd string) attachedAgentClient {
		gotSessionID, gotCWD = sessionID, cwd
		return fakeAttachedAgentClient{}
	}
	t.Cleanup(func() {
		newClaudeClient = oldFactory
		agentLookPath = oldLookPath
		checkClaudeCapabilities = oldCapabilities
	})

	reviewSession := session.New("plan-1", "# Plan")
	reviewSession.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "improve")
	server := review.NewServer(reviewSession)
	cleanup, err := tryAttachAgentServices(context.Background(), &bytes.Buffer{}, server, agentAuto, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Fatal("Claude subprocess-per-request integration should not retain a cleanup process")
	}
	if gotSessionID != sessionID {
		t.Fatalf("session id = %q", gotSessionID)
	}
	if gotCWD == "" {
		t.Fatal("working directory was not passed to Claude client")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("state status = %d: %s", response.Code, response.Body.String())
	}
	var state struct {
		Agent   review.AgentInfo `json:"agent"`
		Threads []struct {
			Capabilities struct {
				CanAsk     bool `json:"canAsk"`
				CanIterate bool `json:"canIterate"`
			} `json:"capabilities"`
		} `json:"threads"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Agent.ID != agentClaude || state.Agent.DisplayName != "Claude Code" || !state.Agent.Available || state.Agent.ContextMode != "current-session-fork" {
		t.Fatalf("unexpected Claude state: %+v", state.Agent)
	}
	if len(state.Threads) != 1 || !state.Threads[0].Capabilities.CanAsk || !state.Threads[0].Capabilities.CanIterate {
		t.Fatalf("Claude actions not advertised: %+v", state.Threads)
	}
}

func TestAttachClaudeWithoutRequiredIsolationFlagsStaysUnavailable(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "6211ea92-c582-4a27-b067-8ed5ba92348d")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "")
	t.Setenv("PLANMAXX_AGENT", "")
	oldLookPath := agentLookPath
	oldCapabilities := checkClaudeCapabilities
	agentLookPath = func(string) (string, error) { return "/test/bin/claude", nil }
	checkClaudeCapabilities = func(context.Context, string) error { return errors.New("--safe-mode is unavailable") }
	t.Cleanup(func() {
		agentLookPath = oldLookPath
		checkClaudeCapabilities = oldCapabilities
	})

	server := review.NewServer(session.New("plan-1", "# Plan"))
	var stderr bytes.Buffer
	if cleanup, err := tryAttachAgentServices(context.Background(), &stderr, server, agentClaude, ""); err != nil || cleanup != nil {
		t.Fatalf("attach result hasCleanup=%t err=%v", cleanup != nil, err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var state struct {
		Agent review.AgentInfo `json:"agent"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Agent.Available || state.Agent.ContextMode != "unavailable" || state.Agent.UnavailableReason == "" {
		t.Fatalf("unsupported Claude CLI should stay unavailable: %+v", state.Agent)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("check Claude Code capabilities")) {
		t.Fatalf("missing capability diagnostic: %s", stderr.String())
	}
}

func TestAttachExplicitClaudeWithoutSessionMarkerStaysUnavailable(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "")
	t.Setenv("PLANMAXX_AGENT", "")

	server := review.NewServer(session.New("plan-1", "# Plan"))
	if cleanup, err := tryAttachAgentServices(context.Background(), &bytes.Buffer{}, server, agentClaude, ""); err != nil || cleanup != nil {
		t.Fatalf("attach result hasCleanup=%t err=%v", cleanup != nil, err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var state struct {
		Agent review.AgentInfo `json:"agent"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Agent.ID != agentClaude || state.Agent.Available || state.Agent.UnavailableReason == "" {
		t.Fatalf("unexpected unavailable state: %+v", state.Agent)
	}
}

func TestAttachClaudeWithMissingBinaryStaysUnavailable(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "6211ea92-c582-4a27-b067-8ed5ba92348d")
	t.Setenv("PLANMAXX_CLAUDE_SESSION_ID", "")
	t.Setenv("PLANMAXX_AGENT", "")
	oldLookPath := agentLookPath
	agentLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { agentLookPath = oldLookPath })

	server := review.NewServer(session.New("plan-1", "# Plan"))
	var stderr bytes.Buffer
	if cleanup, err := tryAttachAgentServices(context.Background(), &stderr, server, agentClaude, ""); err != nil || cleanup != nil {
		t.Fatalf("attach result hasCleanup=%t err=%v", cleanup != nil, err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var state struct {
		Agent review.AgentInfo `json:"agent"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Agent.Available || state.Agent.UnavailableReason != "The Claude Code executable is not available on PATH." {
		t.Fatalf("unexpected missing-binary state: %+v", state.Agent)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("find Claude Code")) {
		t.Fatalf("missing diagnostic: %s", stderr.String())
	}
}

func TestMonitoredAgentFailureDemotesServerCapabilitiesAndFailsClosed(t *testing.T) {
	reviewSession := session.New("plan-1", "# Plan")
	reviewSession.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "improve")
	server := review.NewServer(reviewSession).WithAgent(review.AgentInfo{
		ID: "claude", DisplayName: "Claude Code", ContextMode: "current-session-fork", Available: true,
	})
	failing := &failingAttachedAgentClient{}
	client := monitorAgentClient(failing, server, "Claude Code")

	if _, err := client.AskPrompt(context.Background(), "prompt"); err == nil {
		t.Fatal("expected provider failure")
	}
	if _, err := client.AskPrompt(context.Background(), "prompt again"); err == nil {
		t.Fatal("expected monitored client to stay unavailable")
	}
	if failing.calls != 1 {
		t.Fatalf("provider calls = %d, want one before fail-closed demotion", failing.calls)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var state struct {
		Agent        review.AgentInfo `json:"agent"`
		Capabilities struct {
			CanIterate bool `json:"canIterate"`
		} `json:"capabilities"`
		Threads []struct {
			Capabilities struct {
				CanAsk     bool `json:"canAsk"`
				CanIterate bool `json:"canIterate"`
			} `json:"capabilities"`
		} `json:"threads"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Agent.Available || state.Agent.ContextMode != "unavailable" || state.Agent.UnavailableReason == "" {
		t.Fatalf("agent was not demoted: %+v", state.Agent)
	}
	if state.Capabilities.CanIterate || state.Threads[0].Capabilities.CanAsk || state.Threads[0].Capabilities.CanIterate {
		t.Fatalf("assisted capabilities remained enabled: %+v", state)
	}
}

func TestMonitoredAgentFailureRunsProviderCleanupExactlyOnce(t *testing.T) {
	server := review.NewServer(session.New("plan-1", "# Plan")).WithAgent(review.AgentInfo{
		ID: "codex", DisplayName: "Codex", ContextMode: "current-session-fork", Available: true,
	})
	failing := &failingAttachedAgentClient{}
	cleanupCalls := 0
	client := monitorAgentClient(failing, server, "Codex", func() {
		cleanupCalls++
	})

	if _, err := client.AskPrompt(context.Background(), "first"); err == nil {
		t.Fatal("expected first provider failure")
	}
	if _, err := client.AskPrompt(context.Background(), "second"); err == nil {
		t.Fatal("expected fail-closed second request")
	}
	if failing.calls != 1 {
		t.Fatalf("provider calls = %d, want one", failing.calls)
	}
	if cleanupCalls != 1 {
		t.Fatalf("provider cleanup calls = %d, want one", cleanupCalls)
	}
}

func TestLegacyClaudeHookCommandIsHiddenFromRootHelp(t *testing.T) {
	var stdout bytes.Buffer
	cmd := NewRootCommand(&stdout, &bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("claude-session-hook")) {
		t.Fatalf("hook command should be hidden: %s", stdout.String())
	}
}
