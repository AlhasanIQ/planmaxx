package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
	"github.com/go-git/go-git/v5/plumbing"
)

type fakeSideQuestionClient struct {
	answer string
	err    error
	called bool
	req    sidequestions.Request
}

func (f *fakeSideQuestionClient) Ask(ctx context.Context, req sidequestions.Request) (string, error) {
	f.called = true
	f.req = req
	return f.answer, f.err
}

type fakeSectionPromptClient struct {
	answer string
	err    error
	prompt string
	called bool
}

func (f *fakeSectionPromptClient) AskPrompt(ctx context.Context, prompt string) (string, error) {
	f.called = true
	f.prompt = prompt
	return f.answer, f.err
}

type blockingSideQuestionClient struct {
	started chan struct{}
	unblock chan struct{}
}

func (c blockingSideQuestionClient) Ask(ctx context.Context, req sidequestions.Request) (string, error) {
	close(c.started)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-c.unblock:
		return "Answer after finalize", nil
	}
}

type blockingSectionPromptClient struct {
	started chan struct{}
	unblock chan string
}

func (c blockingSectionPromptClient) AskPrompt(ctx context.Context, prompt string) (string, error) {
	if c.started != nil {
		close(c.started)
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-c.unblock:
		return answer, nil
	}
}

func sectionProposalResponse(revision, target, expected, summary, replacement string) string {
	return fmt.Sprintf(`<planmaxx_proposal version="1" revision=%q><summary>%s</summary><replacement target=%q><expected>%s</expected><content>%s</content></replacement></planmaxx_proposal>`, revision, summary, target, expected, replacement)
}

func TestStateRouteReturnsSession(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var got session.Session
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan != "# Plan" {
		t.Fatalf("expected plan body, got %q", got.Plan)
	}
}

func TestServerPersistsRevisionContentInGitStore(t *testing.T) {
	store, err := revisions.Open(t.TempDir() + "/revisions.git")
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("plan-1", "# Plan\n- Old")
	autosavePath := t.TempDir() + "/review.json"
	server := NewServer(s).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := server.EnableAutosave(autosavePath); err != nil {
		t.Fatal(err)
	}
	if s.Revisions[0].CommitID == "" {
		t.Fatal("initial revision has no git commit")
	}
	s.AddTurnRevision("# Plan\n- New", "update")
	server.mu.Lock()
	err = server.persistLocked()
	server.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if s.Revisions[1].CommitID == "" || s.Revisions[1].CommitID == s.Revisions[0].CommitID {
		t.Fatalf("missing distinct commit IDs %+v", s.Revisions)
	}
	if got, err := store.Read(plumbing.NewHash(s.Revisions[1].CommitID)); err != nil || got != "# Plan\n- New" {
		t.Fatalf("stored revision %q, %v", got, err)
	}
	saved, ok, err := LoadAutosave(autosavePath)
	if err != nil || !ok || saved.Session.Revisions[0].Plan != "" || saved.Session.Revisions[1].Plan != "" {
		t.Fatalf("expected compact autosave revisions, %+v, %v", saved.Session.Revisions, err)
	}
	reloaded := NewServer(session.New("ignored", "ignored")).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := reloaded.EnableAutosave(autosavePath); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.session.Revisions[1].Plan; got != "# Plan\n- New" {
		t.Fatalf("rehydrated plan %q", got)
	}
}

func TestServerMigratesLegacyAutosaveRevisionsIntoGitStore(t *testing.T) {
	path := t.TempDir() + "/review.json"
	legacy := session.New("plan-1", "# Plan\n- Legacy")
	if err := NewServer(legacy).EnableAutosave(path); err != nil {
		t.Fatal(err)
	}
	store, err := revisions.Open(t.TempDir() + "/revisions.git")
	if err != nil {
		t.Fatal(err)
	}
	migrated := NewServer(session.New("ignored", "ignored")).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := migrated.EnableAutosave(path); err != nil {
		t.Fatal(err)
	}
	if migrated.session.Revisions[0].CommitID == "" {
		t.Fatal("legacy revision was not committed")
	}
	saved, ok, err := LoadAutosave(path)
	if err != nil || !ok || saved.Session.Revisions[0].CommitID == "" || saved.Session.Revisions[0].Plan != "" {
		t.Fatalf("legacy migration save %+v, %v", saved.Session.Revisions, err)
	}
}

func TestServerRecoversJournalAfterGitCommitBeforeAutosaveMetadata(t *testing.T) {
	store, err := revisions.Open(t.TempDir() + "/revisions.git")
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/review.json"
	s := session.New("plan-1", "# Plan\n- One")
	server := NewServer(s).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := server.EnableAutosave(path); err != nil {
		t.Fatal(err)
	}
	s.AddTurnRevision("# Plan\n- Two", "two")
	server.mu.Lock()
	err = server.syncRevisionStoreLocked()
	journal := revisionJournal{ExpectedGeneration: server.autosaveGeneration, Status: server.autosaveStatus, Document: server.autosaveDocument, Session: compactRevisionBodies(*s)}
	server.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRevisionJournal(path, journal); err != nil {
		t.Fatal(err)
	}

	recovered := NewServer(session.New("ignored", "ignored")).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := recovered.EnableAutosave(path); err != nil {
		t.Fatal(err)
	}
	if got := recovered.session.Plan; got != "# Plan\n- Two" || recovered.session.Revisions[1].CommitID == "" {
		t.Fatalf("journal recovery lost Git-backed revision: %+v", recovered.session.Revisions)
	}
	if _, ok, err := loadRevisionJournal(path); err != nil || ok {
		t.Fatalf("expected recovered journal to be removed, exists=%t err=%v", ok, err)
	}
}

func TestRevisionRestoreAppendsGitBackedContent(t *testing.T) {
	store, err := revisions.Open(t.TempDir() + "/revisions.git")
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("plan-1", "# Plan\n- One")
	server := NewServer(s).WithRevisionStore(store, revisions.PlanID("/repo/plan.md"))
	if err := server.EnableAutosave(t.TempDir() + "/review.json"); err != nil {
		t.Fatal(err)
	}
	s.AddTurnRevision("# Plan\n- Two", "two")
	server.mu.Lock()
	err = server.persistLocked()
	server.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/revisions/rev-1/restore", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || s.Plan != "# Plan\n- One" || len(s.Revisions) != 3 {
		t.Fatalf("restore %d plan=%q revisions=%+v", res.Code, s.Plan, s.Revisions)
	}
	if s.Revisions[2].CommitID == "" {
		t.Fatal("restore did not create commit")
	}
}

func TestServerAutosavesInitialAndMutatedState(t *testing.T) {
	path := t.TempDir() + "/review.json"
	s := session.New("plan-1", "# Plan\n")
	server := NewServer(s)

	if err := server.EnableAutosave(path); err != nil {
		t.Fatalf("enable autosave: %v", err)
	}

	saved, ok, err := LoadAutosave(path)
	if err != nil {
		t.Fatalf("load initial autosave: %v", err)
	}
	if !ok {
		t.Fatal("expected initial autosave file")
	}
	if saved.Status != "active" || saved.Session.Plan != "# Plan\n" {
		t.Fatalf("unexpected initial autosave %+v", saved)
	}

	res := serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Persist this comment"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected create thread 200, got %d", res.Code)
	}

	saved, ok, err = LoadAutosave(path)
	if err != nil {
		t.Fatalf("load mutated autosave: %v", err)
	}
	if !ok {
		t.Fatal("expected mutated autosave file")
	}
	if saved.Status != "active" {
		t.Fatalf("expected active autosave, got %q", saved.Status)
	}
	if len(saved.Session.Threads) != 1 {
		t.Fatalf("expected one autosaved thread, got %+v", saved.Session.Threads)
	}
	if got := saved.Session.Threads[0].Messages[0].Body; got != "Persist this comment" {
		t.Fatalf("expected autosaved comment body, got %q", got)
	}
}

func TestConcurrentServersRejectStaleWriteAndReloadLatestState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	first := NewServer(session.New("session-1", "# Plan\n"))
	second := NewServer(session.New("session-1", "# Plan\n"))
	if err := first.EnableAutosave(path); err != nil {
		t.Fatalf("enable first autosave: %v", err)
	}
	if err := second.EnableAutosave(path); err != nil {
		t.Fatalf("enable second autosave: %v", err)
	}

	if res := serveCreateThread(first, `{"anchor":{"startLine":1,"endLine":1},"body":"First server comment"}`); res.Code != http.StatusOK {
		t.Fatalf("expected first write to succeed, got %d: %s", res.Code, res.Body.String())
	}
	if res := serveCreateThread(second, `{"anchor":{"startLine":1,"endLine":1},"body":"Stale server comment"}`); res.Code != http.StatusConflict {
		t.Fatalf("expected stale write conflict, got %d: %s", res.Code, res.Body.String())
	}

	res := httptest.NewRecorder()
	second.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("expected state reload, got %d: %s", res.Code, res.Body.String())
	}
	var state session.Session
	if err := json.Unmarshal(res.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode reloaded state: %v", err)
	}
	if len(state.Threads) != 1 || state.Threads[0].Messages[0].Body != "First server comment" {
		t.Fatalf("expected latest persisted state, got %+v", state.Threads)
	}
}

func TestServerDetectsExternalSourceChangeBeforeMutation(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plan.md")
	previous := "# Plan\n\n- Original\n"
	next := "# Plan\n\n- Added by editor\n- Original\n"
	if err := os.WriteFile(planPath, []byte(previous), 0o600); err != nil {
		t.Fatalf("write source plan: %v", err)
	}
	s := session.New("plan-1", previous)
	s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Keep this comment")
	server := NewServer(s).WithAutosaveDocument(NewDocument(planPath, previous))
	if err := server.EnableAutosave(filepath.Join(t.TempDir(), "review.json")); err != nil {
		t.Fatalf("enable autosave: %v", err)
	}
	if err := os.WriteFile(planPath, []byte(next), 0o600); err != nil {
		t.Fatalf("edit source plan: %v", err)
	}

	res := serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Do not apply yet"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected source-change conflict, got %d: %s", res.Code, res.Body.String())
	}
	stateRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(stateRes, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	var state session.Session
	if err := json.Unmarshal(stateRes.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode reconciled state: %v", err)
	}
	if state.Plan != next || len(state.Revisions) != 2 || state.Revisions[1].Source != session.RevisionSourceExternal {
		t.Fatalf("expected persisted external revision, got %+v", state)
	}
	if got := state.Threads[0].Anchor; got != (session.Anchor{StartLine: 4, EndLine: 4}) {
		t.Fatalf("expected comment to re-anchor, got %+v", got)
	}
}

func TestServerAutosavesCanceledState(t *testing.T) {
	path := t.TempDir() + "/review.json"
	s := session.New("plan-1", "# Plan\n")
	server := NewServer(s)

	if err := server.EnableAutosave(path); err != nil {
		t.Fatalf("enable autosave: %v", err)
	}
	res := serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Keep even on cancel"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected create thread 200, got %d", res.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	cancelRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(cancelRes, req)
	if cancelRes.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", cancelRes.Code)
	}

	saved, ok, err := LoadAutosave(path)
	if err != nil {
		t.Fatalf("load canceled autosave: %v", err)
	}
	if !ok {
		t.Fatal("expected canceled autosave file")
	}
	if saved.Status != "canceled" {
		t.Fatalf("expected canceled status, got %q", saved.Status)
	}
	if got := saved.Session.Threads[0].Messages[0].Body; got != "Keep even on cancel" {
		t.Fatalf("expected autosaved comment body, got %q", got)
	}
}

func TestHealthRouteReturnsSessionStatus(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	var got struct {
		Status    string `json:"status"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.SessionID != "plan-1" {
		t.Fatalf("unexpected health response %+v", got)
	}
}

func TestHealthRouteWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "method not allowed")
}

func TestIndexRouteServesReviewUI(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !bytes.Contains(res.Body.Bytes(), []byte("PlanMaxx")) {
		t.Fatalf("expected PlanMaxx UI shell, got %s", res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`id="root"`)) {
		t.Fatalf("expected React mount point, got %s", res.Body.String())
	}
}

func TestStaticAssetsIncludeLazyLoadedReviewModules(t *testing.T) {
	assets, err := fs.ReadDir(staticFiles, "static/assets")
	if err != nil {
		t.Fatalf("review UI not built (run ./scripts/build-web.sh): %v", err)
	}
	var lazyAsset string
	for _, asset := range assets {
		if asset.Name() != "app.js" && asset.Name() != "app.css" && strings.HasSuffix(asset.Name(), ".js") {
			lazyAsset = asset.Name()
			break
		}
	}
	if lazyAsset == "" {
		t.Fatal("expected a lazy-loaded review UI asset")
	}

	s := session.New("plan-1", "# Plan")
	server := NewServer(s)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/assets/"+lazyAsset, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("expected lazy asset %q to be served, got %d", lazyAsset, res.Code)
	}
}

func TestReviewAppSupportsInlineCharacterCommentPrompt(t *testing.T) {
	app, err := staticFiles.ReadFile("static/assets/app.js")
	if err != nil {
		t.Fatalf("review UI not built (run ./scripts/build-web.sh): %v", err)
	}
	for _, want := range []string{
		"Move selection start",
		"Move selection end",
		"draft-boundary-handle",
		"startChar",
		"endLine",
		"inline-comment-composer",
	} {
		if !bytes.Contains(app, []byte(want)) {
			t.Fatalf("expected bundled app.js to contain %q", want)
		}
	}
}

func TestReviewCSSStacksPositionedThreadsOnMobile(t *testing.T) {
	css, err := staticFiles.ReadFile("static/assets/app.css")
	if err != nil {
		t.Fatalf("review UI not built (run ./scripts/build-web.sh): %v", err)
	}
	if !bytesContainsAny(css, []string{"@media (max-width: 820px)", "@media (max-width:820px)", "@media(max-width:820px)"}) {
		t.Fatal("expected bundled CSS to contain @media (max-width: 820px) rule")
	}
	if !bytes.Contains(css, []byte(".thread.is-positioned")) {
		t.Fatal("expected bundled CSS to contain .thread.is-positioned selector")
	}
	if !bytesContainsAny(css, []string{"position: static", "position:static"}) {
		t.Fatal("expected bundled CSS to contain position: static rule")
	}
}

func bytesContainsAny(haystack []byte, needles []string) bool {
	for _, n := range needles {
		if bytes.Contains(haystack, []byte(n)) {
			return true
		}
	}
	return false
}

func TestCreateThreadRouteAddsThread(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Clarify this step"}`)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if len(s.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(s.Threads))
	}
	var got session.Thread
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "thread-1" {
		t.Fatalf("expected thread-1, got %q", got.ID)
	}
	if got.Anchor != (session.Anchor{StartLine: 1, EndLine: 1}) {
		t.Fatalf("expected anchor 1-1, got %+v", got.Anchor)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].Body != "Clarify this step" {
		t.Fatalf("expected message body, got %q", got.Messages[0].Body)
	}
}

func TestCreateThreadRouteAddsCharacterAnchor(t *testing.T) {
	s := session.New("plan-1", "Use boring code\nShip it")
	server := NewServer(s)

	res := serveCreateThread(server, `{"anchor":{"startLine":1,"startChar":4,"endLine":1,"endChar":10},"body":"Clarify this word"}`)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var got session.Thread
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Anchor != (session.Anchor{StartLine: 1, StartChar: 4, EndLine: 1, EndChar: 10}) {
		t.Fatalf("expected char anchor 1:4-1:10, got %+v", got.Anchor)
	}
}

func TestCreateThreadWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestCreateThreadInvalidJSONReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveCreateThread(server, "{")

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.Threads) != 0 {
		t.Fatalf("expected no threads, got %d", len(s.Threads))
	}
}

func TestCreateThreadInvalidShapeOrValuesReturnsJSONError(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "empty body",
			body: `{"anchor":{"startLine":1,"endLine":1},"body":"   "}`,
		},
		{
			name: "zero start line",
			body: `{"anchor":{"startLine":0,"endLine":1},"body":"Clarify this step"}`,
		},
		{
			name: "end before start",
			body: `{"anchor":{"startLine":2,"endLine":1},"body":"Clarify this step"}`,
		},
		{
			name: "anchor beyond plan line count",
			body: `{"anchor":{"startLine":4,"endLine":4},"body":"Clarify this step"}`,
		},
		{
			name: "start char beyond line length",
			body: `{"anchor":{"startLine":1,"startChar":20,"endLine":1,"endChar":21},"body":"Clarify this step"}`,
		},
		{
			name: "end char before start char on same line",
			body: `{"anchor":{"startLine":1,"startChar":5,"endLine":1,"endChar":4},"body":"Clarify this step"}`,
		},
		{
			name: "end char beyond line length",
			body: `{"anchor":{"startLine":1,"startChar":1,"endLine":1,"endChar":20},"body":"Clarify this step"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := session.New("plan-1", "# Plan\n\n- Step")
			server := NewServer(s)

			res := serveCreateThread(server, tt.body)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", res.Code)
			}
			assertJSONContentType(t, res)
			if len(s.Threads) != 0 {
				t.Fatalf("expected no threads, got %d", len(s.Threads))
			}
		})
	}
}

func TestCreateThreadAfterFinalizeReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads) != 0 {
		t.Fatalf("expected no threads, got %d", len(s.Threads))
	}
}

func TestCreateThreadAfterCancelReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = serveCreateThread(server, `{"anchor":{"startLine":1,"endLine":1},"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads) != 0 {
		t.Fatalf("expected no threads, got %d", len(s.Threads))
	}
}

func TestMoveThreadRouteUpdatesPosition(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	body := bytes.NewBufferString(`{"x":240,"y":360}`)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/move", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if s.Threads[0].Position.X != 240 || s.Threads[0].Position.Y != 360 {
		t.Fatalf("unexpected position %+v", s.Threads[0].Position)
	}
}

func TestReanchorThreadRouteUpdatesAnchor(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline 2\nline 3\nline 4\nline 5")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	body := bytes.NewBufferString(`{"startLine":3,"endLine":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/reanchor", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if s.Threads[0].Anchor.StartLine != 3 || s.Threads[0].Anchor.EndLine != 5 {
		t.Fatalf("unexpected anchor %+v", s.Threads[0].Anchor)
	}
}

func TestEditThreadRouteUpdatesAnchorAndOriginalComment(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline two words\nline three")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Original")
	s.AddReply(thread.ID, "Keep reply")
	server := NewServer(s)

	res := serveEditThread(server, thread.ID, `{"anchor":{"startLine":2,"startChar":5,"endLine":2,"endChar":8},"body":" Updated original "}`)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if s.Threads[0].Anchor != (session.Anchor{StartLine: 2, StartChar: 5, EndLine: 2, EndChar: 8}) {
		t.Fatalf("unexpected anchor %+v", s.Threads[0].Anchor)
	}
	if got := s.Threads[0].Messages[0].Body; got != "Updated original" {
		t.Fatalf("expected edited original comment, got %q", got)
	}
	if got := s.Threads[0].Messages[1].Body; got != "Keep reply" {
		t.Fatalf("expected reply to remain, got %q", got)
	}
}

func TestReplyThreadRouteAddsMessage(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	res := serveReplyThread(server, thread.ID, `{"body":"Follow-up decision"}`)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if len(s.Threads[0].Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(s.Threads[0].Messages))
	}
	if s.Threads[0].Messages[1].Body != "Follow-up decision" {
		t.Fatalf("expected reply body, got %q", s.Threads[0].Messages[1].Body)
	}
}

func TestDeleteThreadRouteRemovesThreadAndSideAnswers(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Mistake")
	s.AddSideAnswer(thread.ID, "Why?", "Because.")
	server := NewServer(s)

	res := serveDeleteThread(server, thread.ID)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if len(s.Threads) != 0 {
		t.Fatalf("expected no threads, got %+v", s.Threads)
	}
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %+v", s.SideAnswers)
	}
}

func TestThreadActionWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/threads/"+thread.ID+"/move", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestThreadActionMissingThreadReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveMoveThread(server, "thread-missing", `{"x":240,"y":360}`)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestThreadActionInvalidRouteReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+thread.ID+"/rename", strings.NewReader(`{}`))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestReplyThreadInvalidJSONReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	res := serveReplyThread(server, thread.ID, `{"body":"   "}`)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.Threads[0].Messages) != 1 {
		t.Fatalf("expected one original message, got %d", len(s.Threads[0].Messages))
	}
}

func TestMoveThreadInvalidJSONReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)
	original := s.Threads[0].Position

	res := serveMoveThread(server, thread.ID, "{")

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if s.Threads[0].Position != original {
		t.Fatalf("expected position to stay %+v, got %+v", original, s.Threads[0].Position)
	}
}

func TestReanchorThreadInvalidAnchorReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline 2")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)
	original := s.Threads[0].Anchor

	res := serveReanchorThread(server, thread.ID, `{"startLine":3,"endLine":3}`)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if s.Threads[0].Anchor != original {
		t.Fatalf("expected anchor to stay %+v, got %+v", original, s.Threads[0].Anchor)
	}
}

func TestEditThreadInvalidJSONReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline 2")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)
	originalAnchor := s.Threads[0].Anchor
	originalBody := s.Threads[0].Messages[0].Body

	res := serveEditThread(server, thread.ID, `{"anchor":{"startLine":3,"endLine":3},"body":"Changed"}`)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if s.Threads[0].Anchor != originalAnchor {
		t.Fatalf("expected anchor to stay %+v, got %+v", originalAnchor, s.Threads[0].Anchor)
	}
	if got := s.Threads[0].Messages[0].Body; got != originalBody {
		t.Fatalf("expected body to stay %q, got %q", originalBody, got)
	}
}

func TestThreadActionAfterFinalizeReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline 2")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)
	originalPosition := s.Threads[0].Position
	originalAnchor := s.Threads[0].Anchor
	originalBody := s.Threads[0].Messages[0].Body
	originalMessages := len(s.Threads[0].Messages)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = serveMoveThread(server, thread.ID, `{"x":240,"y":360}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Position != originalPosition {
		t.Fatalf("expected position to stay %+v, got %+v", originalPosition, s.Threads[0].Position)
	}

	res = serveReanchorThread(server, thread.ID, `{"startLine":2,"endLine":2}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Anchor != originalAnchor {
		t.Fatalf("expected anchor to stay %+v, got %+v", originalAnchor, s.Threads[0].Anchor)
	}

	res = serveEditThread(server, thread.ID, `{"anchor":{"startLine":2,"endLine":2},"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Anchor != originalAnchor {
		t.Fatalf("expected anchor to stay %+v, got %+v", originalAnchor, s.Threads[0].Anchor)
	}
	if got := s.Threads[0].Messages[0].Body; got != originalBody {
		t.Fatalf("expected body to stay %q, got %q", originalBody, got)
	}

	res = serveReplyThread(server, thread.ID, `{"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads[0].Messages) != originalMessages {
		t.Fatalf("expected message count to stay %d, got %d", originalMessages, len(s.Threads[0].Messages))
	}

	res = serveDeleteThread(server, thread.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads) != 1 {
		t.Fatalf("expected thread to remain after rejected delete, got %+v", s.Threads)
	}
}

func TestThreadActionAfterCancelReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan\nline 2")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)
	originalPosition := s.Threads[0].Position
	originalAnchor := s.Threads[0].Anchor
	originalBody := s.Threads[0].Messages[0].Body
	originalMessages := len(s.Threads[0].Messages)

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = serveReanchorThread(server, thread.ID, `{"startLine":2,"endLine":2}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Anchor != originalAnchor {
		t.Fatalf("expected anchor to stay %+v, got %+v", originalAnchor, s.Threads[0].Anchor)
	}

	res = serveEditThread(server, thread.ID, `{"anchor":{"startLine":2,"endLine":2},"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Anchor != originalAnchor {
		t.Fatalf("expected anchor to stay %+v, got %+v", originalAnchor, s.Threads[0].Anchor)
	}
	if got := s.Threads[0].Messages[0].Body; got != originalBody {
		t.Fatalf("expected body to stay %q, got %q", originalBody, got)
	}

	res = serveMoveThread(server, thread.ID, `{"x":240,"y":360}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.Threads[0].Position != originalPosition {
		t.Fatalf("expected position to stay %+v, got %+v", originalPosition, s.Threads[0].Position)
	}

	res = serveReplyThread(server, thread.ID, `{"body":"Too late"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads[0].Messages) != originalMessages {
		t.Fatalf("expected message count to stay %d, got %d", originalMessages, len(s.Threads[0].Messages))
	}

	res = serveDeleteThread(server, thread.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.Threads) != 1 {
		t.Fatalf("expected thread to remain after rejected delete, got %+v", s.Threads)
	}
}

func TestSideQuestionRouteAddsSideAnswer(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Step")
	s.PlanPath = "/repo/plan.md"
	thread := s.AddThreadWithSelectedText(session.Anchor{StartLine: 3, StartChar: 2, EndLine: 3, EndChar: 6}, "Clarify", "Step")
	client := &fakeSideQuestionClient{
		answer: "Use the CLI milestone first.",
	}
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", client))

	res := serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why this order?"}`)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var got session.SideAnswer
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ThreadID != thread.ID {
		t.Fatalf("expected side answer thread %q, got %q", thread.ID, got.ThreadID)
	}
	if got.Question != "Why this order?" {
		t.Fatalf("unexpected question %q", got.Question)
	}
	if got.Answer != "Use the CLI milestone first." {
		t.Fatalf("unexpected answer %q", got.Answer)
	}
	if len(s.SideAnswers) != 1 {
		t.Fatalf("expected stored side answer, got %d", len(s.SideAnswers))
	}
	if client.req.FilePath != "/repo/plan.md" {
		t.Fatalf("expected file path context, got %+v", client.req)
	}
	if client.req.Reference != "/repo/plan.md:3:3-3:7" {
		t.Fatalf("expected line:char reference context, got %+v", client.req)
	}
	if client.req.SelectedText != "Step" {
		t.Fatalf("expected selected text context, got %+v", client.req)
	}
}

func TestPromoteSideAnswerRoutePromotesAnswer(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	server := NewServer(s)

	res := servePromoteSideAnswer(server, answer.ID)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to be promoted")
	}
	draft := digestDraft(t, serveDigestDraft(server, http.MethodPost))
	if len(draft.PromotedSideAnswers) != 1 ||
		!strings.Contains(draft.PromotedSideAnswers[0], "Question:\nWhy?") ||
		!strings.Contains(draft.PromotedSideAnswers[0], "Answer:\nUse Cobra.") {
		t.Fatalf("expected promoted answer in draft, got %+v", draft.PromotedSideAnswers)
	}
}

func TestUnpromoteSideAnswerRouteRemovesAnswerFromDigest(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	s.PromoteSideAnswer(answer.ID)
	server := NewServer(s)

	res := serveUnpromoteSideAnswer(server, answer.ID)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to be unpromoted")
	}
	draft := digestDraft(t, serveDigestDraft(server, http.MethodPost))
	if len(draft.PromotedSideAnswers) != 0 {
		t.Fatalf("expected no promoted answers in draft, got %+v", draft.PromotedSideAnswers)
	}
}

func TestPromoteSideAnswerWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/side-answers/"+answer.ID+"/promote", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestPromoteSideAnswerMissingAnswerReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := servePromoteSideAnswer(server, "side-missing")

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestUnpromoteSideAnswerMissingAnswerReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveUnpromoteSideAnswer(server, "side-missing")

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestPromoteSideAnswerAfterFinalizeReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = servePromoteSideAnswer(server, answer.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to stay unpromoted")
	}
}

func TestPromoteSideAnswerAfterCancelReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = servePromoteSideAnswer(server, answer.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to stay unpromoted")
	}
}

func TestUnpromoteSideAnswerAfterFinalizeReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	answer := s.AddSideAnswer(thread.ID, "Why?", "Use Cobra.")
	s.PromoteSideAnswer(answer.ID)
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":["Use Cobra."]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = serveUnpromoteSideAnswer(server, answer.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if !s.SideAnswers[0].Promoted {
		t.Fatal("expected side answer to stay promoted")
	}
}

func TestSideQuestionMissingThreadReturnsJSONErrorAndDoesNotCallService(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	client := &fakeSideQuestionClient{answer: "orphan answer"}
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", client))

	res := serveSideQuestion(server, `{"threadID":"thread-missing","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "thread not found")
	if client.called {
		t.Fatal("expected side-question service not to be called for missing thread")
	}
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionEmptyThreadReturnsJSONErrorAndDoesNotCallService(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	client := &fakeSideQuestionClient{answer: "orphan answer"}
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", client))

	res := serveSideQuestion(server, `{"threadID":"","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "thread not found")
	if client.called {
		t.Fatal("expected side-question service not to be called for empty thread")
	}
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionUnavailableReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s)

	res := serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionClientErrorReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", &fakeSideQuestionClient{
		err: errors.New("app-server failed"),
	}))

	res := serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionTimeoutReturnsJSONErrorAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	client := blockingSideQuestionClient{
		started: make(chan struct{}),
		unblock: make(chan struct{}),
	}
	server := NewServer(s).
		WithSideQuestions(sidequestions.NewService("codex-thread", client)).
		WithSideQuestionTimeout(10 * time.Millisecond)

	res := serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
	close(client.unblock)
}

func TestSideQuestionWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodGet, "/api/side-questions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestSideQuestionInvalidJSONReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveSideQuestion(server, "{")

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionAfterFinalizeReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", &fakeSideQuestionClient{
		answer: "Too late",
	}))

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestSideQuestionAfterCancelReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", &fakeSideQuestionClient{
		answer: "Too late",
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)

	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestDigestDraftRouteReturnsDraft(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Use Cobra for CLI.")
	answer := s.AddSideAnswer("thread-1", "Why?", "Cobra gives clean subcommands.")
	s.PromoteSideAnswer(answer.ID)
	server := NewServer(s)

	res := serveDigestDraft(server, http.MethodPost)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	var got session.Digest
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary == "" {
		t.Fatal("expected digest summary")
	}
	if len(got.ReviewerDecisions) != 1 || got.ReviewerDecisions[0] != "Use Cobra for CLI." {
		t.Fatalf("expected reviewer decisions from thread messages, got %+v", got.ReviewerDecisions)
	}
	if len(got.PromotedSideAnswers) != 1 ||
		!strings.Contains(got.PromotedSideAnswers[0], "Question:\nWhy?") ||
		!strings.Contains(got.PromotedSideAnswers[0], "Answer:\n"+answer.Answer) {
		t.Fatalf("expected promoted side answers, got %+v", got.PromotedSideAnswers)
	}
}

func TestDigestDraftWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveDigestDraft(server, http.MethodGet)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "method not allowed")
}

func TestDigestDraftDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Use Cobra for CLI.")
	server := NewServer(s)

	res := serveDigestDraft(server, http.MethodPost)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	got := getState(t, server)
	if got.Digest.Summary != "" {
		t.Fatalf("expected stored digest to stay empty, got %+v", got.Digest)
	}
	if len(got.Threads) != 1 || got.Threads[0].Messages[0].Body != "Use Cobra for CLI." {
		t.Fatalf("expected existing thread state to remain, got %+v", got.Threads)
	}
}

func TestDigestDraftAfterFinalizeReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}

	res = serveDigestDraft(server, http.MethodPost)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")

	got := getState(t, server)
	if got.Digest.Summary != "Approved" {
		t.Fatalf("expected digest summary to stay Approved, got %q", got.Digest.Summary)
	}
}

func TestDigestDraftAfterCancelReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Use Cobra for CLI.")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = serveDigestDraft(server, http.MethodPost)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")

	got := getState(t, server)
	if got.Digest.Summary != "" {
		t.Fatalf("expected digest to stay empty after canceled draft request, got %+v", got.Digest)
	}
	if len(got.Threads) != 1 || got.Threads[0].Messages[0].Body != "Use Cobra for CLI." {
		t.Fatalf("expected existing thread state to remain, got %+v", got.Threads)
	}
}

func TestThreadKindRouteUpdatesDigestInclusion(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Step")
	thread := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Keep this private")
	server := NewServer(s)

	res := serveThreadKind(server, thread.ID, `{"kind":"note"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected thread kind 200, got %d: %s", res.Code, res.Body.String())
	}
	if s.Threads[0].Kind != session.ThreadKindNote {
		t.Fatalf("expected thread kind note, got %+v", s.Threads[0])
	}
	draft := digestDraft(t, serveDigestDraft(server, http.MethodPost))
	if len(draft.ReviewerDecisions) != 0 {
		t.Fatalf("expected note thread to be excluded from digest, got %+v", draft.ReviewerDecisions)
	}
}

func TestThreadKindRouteRejectsInvalidKindWithoutMutation(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Decision")
	server := NewServer(s)

	res := serveThreadKind(server, thread.ID, `{"kind":"private"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid kind 400, got %d", res.Code)
	}
	if s.Threads[0].Kind != session.ThreadKindDecision {
		t.Fatalf("expected invalid kind not to mutate thread, got %+v", s.Threads[0])
	}
}

func TestRevisionRoutesListAndDiffAppliedRevisions(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old")
	s.AddTurnRevision("# Plan\n\n- New", "Codex turn")
	server := NewServer(s)

	listRes := serveRevisions(server)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected revisions 200, got %d: %s", listRes.Code, listRes.Body.String())
	}
	var listed struct {
		CurrentRevisionID string             `json:"currentRevisionId"`
		Revisions         []session.Revision `json:"revisions"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.CurrentRevisionID != "rev-2" || len(listed.Revisions) != 2 {
		t.Fatalf("unexpected revision list %+v", listed)
	}

	diffRes := serveRevisionDiff(server, "rev-1", "rev-2")
	if diffRes.Code != http.StatusOK {
		t.Fatalf("expected diff 200, got %d: %s", diffRes.Code, diffRes.Body.String())
	}
	var diffBody struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Lines []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(diffRes.Body.Bytes(), &diffBody); err != nil {
		t.Fatal(err)
	}
	if diffBody.From != "rev-1" || diffBody.To != "rev-2" {
		t.Fatalf("unexpected diff ids %+v", diffBody)
	}
	if !diffContains(diffBody.Lines, "remove", "- Old") || !diffContains(diffBody.Lines, "add", "- New") {
		t.Fatalf("expected revision diff to contain old removal and new addition, got %+v", diffBody.Lines)
	}
}

func TestProposeSectionRouteCreatesPendingProposal(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old\n- Keep")
	thread := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Clarify this")
	client := &fakeSectionPromptClient{answer: sectionProposalResponse("rev-1", "lines", "- Old", "Clarified wording.", "- New")}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))

	res := serveProposeSection(server, `{"threadId":"`+thread.ID+`","anchor":{"startLine":3,"endLine":3},"instruction":"Clarify this step"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected propose 200, got %d: %s", res.Code, res.Body.String())
	}
	var proposal session.SectionProposal
	if err := json.Unmarshal(res.Body.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.ID != "proposal-1" || proposal.ProposedPlan != "# Plan\n\n- New\n- Keep" {
		t.Fatalf("unexpected proposal %+v", proposal)
	}
	if s.PendingProposal == nil || s.PendingProposal.ID != proposal.ID {
		t.Fatalf("expected pending proposal to be stored, got %+v", s.PendingProposal)
	}
	if !client.called || !strings.Contains(client.prompt, "Clarify this") {
		t.Fatalf("expected section iteration prompt, got called=%v prompt=%q", client.called, client.prompt)
	}
}

func TestProposeSectionRouteAppliesDeclaredFullLineScopeWithoutCharacterSplice(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Product name: **From Zero to AI Engineer**")
	thread := s.AddThreadWithSelectedText(session.Anchor{StartLine: 3, StartChar: 20, EndLine: 3, EndChar: 24}, "Clarify audience", "om Z")
	client := &fakeSectionPromptClient{answer: sectionProposalResponse("rev-1", "lines", "- Product name: **From Zero to AI Engineer**", "Expanded audience.", "- Product name: **From Zero to AI Engineer**\n- Primary learner: **Software engineers**")}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))

	res := serveProposeSection(server, `{"threadId":"`+thread.ID+`","anchor":{"startLine":3,"startChar":20,"endLine":3,"endChar":24},"instruction":"Clarify audience"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected proposal, got %d: %s", res.Code, res.Body.String())
	}
	if s.PendingProposal == nil {
		t.Fatal("expected pending proposal")
	}
	if got, want := s.PendingProposal.ProposedPlan, "# Plan\n\n- Product name: **From Zero to AI Engineer**\n- Primary learner: **Software engineers**"; got != want {
		t.Fatalf("expected deterministic full-line replacement\nwant: %q\ngot:  %q", want, got)
	}
	if got := s.PendingProposal.AppliedAnchor; got != (session.Anchor{StartLine: 3, EndLine: 3}) {
		t.Fatalf("expected declared applied line range, got %+v", got)
	}
	if !strings.Contains(client.prompt, `<review_target target="selection" threads="thread-1">om Z</review_target>`) {
		t.Fatalf("expected exact in-place selection annotation\n%s", client.prompt)
	}
}

func TestProposeSectionRouteAllowsExplicitScopeOutsideFormerWindow(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- One\n- Two\n- Three\n- Four\n- Five\n- Six")
	client := &fakeSectionPromptClient{answer: sectionProposalResponse("rev-1", "lines", "- Four", "Wrong scope.", "- Changed")}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))

	res := serveProposeSection(server, `{"anchor":{"startLine":3,"startChar":2,"endLine":3,"endChar":5},"instruction":"Update"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected content change outside former window, got %d: %s", res.Code, res.Body.String())
	}
	if s.PendingProposal == nil || s.PendingProposal.ProposedPlan != "# Plan\n\n- One\n- Two\n- Three\n- Changed\n- Five\n- Six" {
		t.Fatalf("expected explicit full-line proposal, got proposal=%+v", s.PendingProposal)
	}
}

func TestCharacterRangeOverlapDoesNotConflateDisjointCommentsOnOneLine(t *testing.T) {
	left := session.Anchor{StartLine: 4, StartChar: 2, EndLine: 4, EndChar: 5}
	right := session.Anchor{StartLine: 4, StartChar: 8, EndLine: 4, EndChar: 12}
	fullLine := session.Anchor{StartLine: 4, EndLine: 4}
	if anchorsOverlap(left, right) {
		t.Fatalf("disjoint character ranges on one line must not overlap")
	}
	if !anchorsOverlap(left, fullLine) || !anchorsOverlap(fullLine, right) {
		t.Fatalf("a full-line anchor must overlap character ranges on that line")
	}
}

func TestProposeSectionRouteUnavailableWithoutAppServerDoesNotMutate(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old")
	server := NewServer(s)

	res := serveProposeSection(server, `{"anchor":{"startLine":3,"endLine":3},"instruction":"Clarify this step"}`)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected propose 503, got %d: %s", res.Code, res.Body.String())
	}
	assertJSONContentType(t, res)
	if s.PendingProposal != nil || s.Plan != "# Plan\n\n- Old" || len(s.Revisions) != 1 {
		t.Fatalf("expected unavailable proposal not to mutate state, got plan=%q revisions=%+v proposal=%+v", s.Plan, s.Revisions, s.PendingProposal)
	}
}

func TestProposeSectionRouteRefinesPendingProposalWhenAnchorMatches(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old\n- Keep")
	s.CreateSectionProposal(session.SectionProposalInput{
		Anchor:          session.Anchor{StartLine: 3, EndLine: 3},
		OriginalSection: "- Old",
		ProposedSection: "- New\n- Extra",
		ProposedPlan:    "# Plan\n\n- New\n- Extra\n- Keep",
		Summary:         "Clarified wording.",
		Instruction:     "Clarify this step",
		RawResponse:     "Summary: Clarified wording.\n\n```markdown\n- New\n- Extra\n```",
	})
	client := &fakeSectionPromptClient{answer: sectionProposalResponse("rev-1", "lines", "- New\n- Extra", "Sharpened wording.", "- Refined")}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))

	res := serveProposeSection(server, `{"anchor":{"startLine":3,"endLine":3},"instruction":"Make it sharper"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected propose 200, got %d: %s", res.Code, res.Body.String())
	}
	if s.PendingProposal == nil || s.PendingProposal.ProposedPlan != "# Plan\n\n- Refined\n- Keep" {
		t.Fatalf("expected refined proposal based on pending plan, got %+v", s.PendingProposal)
	}
	if !strings.Contains(client.prompt, "<selected_text>- New&#xA;- Extra</selected_text>") {
		t.Fatalf("expected prompt to use pending proposal section, got %q", client.prompt)
	}
}

func TestProposeSectionRouteRefinesCharacterRangeUsingStoredReplacementAnchor(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Use rough wording")
	s.CreateSectionProposal(session.SectionProposalInput{
		Anchor:            session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 11},
		ReplacementAnchor: session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 19},
		OriginalSection:   "rough",
		ProposedSection:   "crystal-clear",
		ProposedPlan:      "# Plan\n\n- Use crystal-clear wording",
		Summary:           "Clarified wording.",
		Instruction:       "Clarify this wording",
		RawResponse:       "Summary: Clarified wording.\n\n```markdown\ncrystal-clear\n```",
	})
	client := &fakeSectionPromptClient{answer: sectionProposalResponse("rev-1", "selection", "crystal-clear", "Sharpened wording.", "polished")}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))

	res := serveProposeSection(server, `{"anchor":{"startLine":3,"startChar":6,"endLine":3,"endChar":11},"instruction":"Make it shorter"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected propose 200, got %d: %s", res.Code, res.Body.String())
	}
	if s.PendingProposal == nil || s.PendingProposal.ProposedPlan != "# Plan\n\n- Use polished wording" {
		t.Fatalf("expected refined character range proposal, got %+v", s.PendingProposal)
	}
	wantAnchor := session.Anchor{StartLine: 3, StartChar: 6, EndLine: 3, EndChar: 14}
	if s.PendingProposal.ReplacementAnchor != wantAnchor {
		t.Fatalf("expected replacement anchor %+v, got %+v", wantAnchor, s.PendingProposal.ReplacementAnchor)
	}
	if !strings.Contains(client.prompt, "<selected_text>crystal-clear</selected_text>") {
		t.Fatalf("expected prompt to use pending proposal section, got %q", client.prompt)
	}
}

func TestProposeSectionRouteTimesOutWithoutMutating(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old")
	client := blockingSectionPromptClient{
		started: make(chan struct{}),
		unblock: make(chan string),
	}
	server := NewServer(s).
		WithSectionIterations(sectioniter.NewService("current-thread", client)).
		WithSideQuestionTimeout(10 * time.Millisecond)

	res := serveProposeSection(server, `{"anchor":{"startLine":3,"endLine":3},"instruction":"Clarify this step"}`)
	if res.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected propose 504, got %d: %s", res.Code, res.Body.String())
	}
	if s.PendingProposal != nil || s.Plan != "# Plan\n\n- Old" || len(s.Revisions) != 1 {
		t.Fatalf("expected timed out proposal not to mutate state, got plan=%q revisions=%+v proposal=%+v", s.Plan, s.Revisions, s.PendingProposal)
	}
}

func TestProposeSectionRouteRejectsStaleInFlightResult(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old")
	client := blockingSectionPromptClient{
		started: make(chan struct{}),
		unblock: make(chan string, 1),
	}
	server := NewServer(s).WithSectionIterations(sectioniter.NewService("current-thread", client))
	done := make(chan *httptest.ResponseRecorder, 1)

	go func() {
		done <- serveProposeSection(server, `{"anchor":{"startLine":3,"endLine":3},"instruction":"Clarify this step"}`)
	}()

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for section iteration service call")
	}

	server.mu.Lock()
	s.AddTurnRevision("# Plan\n\n- Changed", "Concurrent turn changed the plan")
	server.mu.Unlock()
	client.unblock <- sectionProposalResponse("rev-1", "lines", "- Old", "Clarified wording.", "- New")

	var res *httptest.ResponseRecorder
	select {
	case res = <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for section proposal response")
	}
	if res.Code != http.StatusConflict {
		t.Fatalf("expected stale proposal 409, got %d: %s", res.Code, res.Body.String())
	}
	assertJSONError(t, res, "plan changed while section iteration was running")
	if s.PendingProposal != nil {
		t.Fatalf("expected stale proposal not to store pending proposal, got %+v", s.PendingProposal)
	}
}

func TestApplyProposalRouteUpdatesPlanAndRevision(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old\n- Keep")
	proposal := s.CreateSectionProposal(session.SectionProposalInput{
		Anchor:            session.Anchor{StartLine: 3, EndLine: 3},
		OriginalSection:   "- Old",
		ProposedSection:   "- New",
		ProposedPlan:      "# Plan\n\n- New\n- Keep",
		Summary:           "Clarified wording.",
		Instruction:       "Clarify this step",
		RawResponse:       "Summary: Clarified wording.\n\n```markdown\n- New\n```",
		IncludedThreadIDs: nil,
	})
	server := NewServer(s)

	res := serveApplyProposal(server, proposal.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("expected apply 200, got %d: %s", res.Code, res.Body.String())
	}
	if s.Plan != "# Plan\n\n- New\n- Keep" {
		t.Fatalf("expected applied plan, got %q", s.Plan)
	}
	if s.CurrentRevisionID != "rev-2" || len(s.Revisions) != 2 {
		t.Fatalf("expected applied revision, got current=%q revisions=%+v", s.CurrentRevisionID, s.Revisions)
	}
	if s.PendingProposal != nil {
		t.Fatalf("expected proposal to clear, got %+v", s.PendingProposal)
	}
}

func TestDiscardProposalRouteLeavesPlanUnchanged(t *testing.T) {
	s := session.New("plan-1", "# Plan\n\n- Old")
	proposal := s.CreateSectionProposal(session.SectionProposalInput{
		Anchor:          session.Anchor{StartLine: 3, EndLine: 3},
		OriginalSection: "- Old",
		ProposedSection: "- New",
		ProposedPlan:    "# Plan\n\n- New",
		Summary:         "Clarified wording.",
		Instruction:     "Clarify this step",
		RawResponse:     "Summary: Clarified wording.\n\n```markdown\n- New\n```",
	})
	server := NewServer(s)

	res := serveDiscardProposal(server, proposal.ID)
	if res.Code != http.StatusOK {
		t.Fatalf("expected discard 200, got %d: %s", res.Code, res.Body.String())
	}
	if s.Plan != "# Plan\n\n- Old" {
		t.Fatalf("expected plan unchanged, got %q", s.Plan)
	}
	if s.PendingProposal != nil {
		t.Fatalf("expected proposal to clear, got %+v", s.PendingProposal)
	}
}

func TestInFlightSideQuestionAfterFinalizeReturnsJSONConflictAndDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	thread := s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Clarify")
	client := blockingSideQuestionClient{
		started: make(chan struct{}),
		unblock: make(chan struct{}),
	}
	server := NewServer(s).WithSideQuestions(sidequestions.NewService("codex-thread", client))
	sideQuestionDone := make(chan *httptest.ResponseRecorder, 1)

	go func() {
		sideQuestionDone <- serveSideQuestion(server, `{"threadID":"`+thread.ID+`","question":"Why?","planExcerpt":"# Plan"}`)
	}()

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for side-question service call")
	}

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d", res.Code)
	}
	close(client.unblock)

	select {
	case res = <-sideQuestionDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for side-question response")
	}
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
	assertJSONError(t, res, "review already completed")
	if len(s.SideAnswers) != 0 {
		t.Fatalf("expected no side answers, got %d", len(s.SideAnswers))
	}
}

func TestFinalizeCompletesServer(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	body := bytes.NewBufferString(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/finalize", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := server.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Canceled {
		t.Fatal("expected finalized result")
	}
	if result.Session.Digest.Summary != "Approved" {
		t.Fatalf("unexpected digest summary %q", result.Session.Digest.Summary)
	}
}

func TestRejectCompletesServerWithRejectionResult(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	body := bytes.NewBufferString(`{"summary":"Rejected: migration order is unsafe","reviewerDecisions":["Revise before implementation"],"promotedSideAnswers":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/reject", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := server.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Canceled {
		t.Fatal("expected reject not to mark review canceled")
	}
	if !result.Rejected {
		t.Fatal("expected rejected result")
	}
	if result.Session.Digest.Summary != "Rejected: migration order is unsafe" {
		t.Fatalf("unexpected digest summary %q", result.Session.Digest.Summary)
	}
}

func TestStateRouteWrongMethodReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/state", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestFinalizeInvalidJSONReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/finalize", strings.NewReader("{"))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestFinalizeTrailingGarbageReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"ok"} trailing`)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestDoubleFinalizeReturnsJSONError(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	body := bytes.NewBufferString(`{"summary":"Approved","reviewerDecisions":["Ship it"],"promotedSideAnswers":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/finalize", body)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected first finalize 200, got %d", res.Code)
	}

	body = bytes.NewBufferString(`{"summary":"Approved again","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/finalize", body)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.Code)
	}
	assertJSONContentType(t, res)
}

func TestRejectedSecondFinalizeDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveFinalize(server, `{"summary":"Approved","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected first finalize 200, got %d", res.Code)
	}

	res = serveFinalize(server, `{"summary":"Overwritten","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected second finalize 409, got %d", res.Code)
	}

	got := getState(t, server)
	if got.Digest.Summary != "Approved" {
		t.Fatalf("expected digest summary to stay Approved, got %q", got.Digest.Summary)
	}
}

func TestFinalizeAfterCancelDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/cancel", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d", res.Code)
	}

	res = serveFinalize(server, `{"summary":"After cancel","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected finalize after cancel 409, got %d", res.Code)
	}

	got := getState(t, server)
	if got.Digest.Summary == "After cancel" {
		t.Fatal("expected rejected finalize after cancel not to mutate digest summary")
	}
}

func TestFinalizeAfterRejectDoesNotMutateState(t *testing.T) {
	s := session.New("plan-1", "# Plan")
	server := NewServer(s)

	res := serveReject(server, `{"summary":"Rejected","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected reject 200, got %d", res.Code)
	}

	res = serveFinalize(server, `{"summary":"After reject","reviewerDecisions":[],"promotedSideAnswers":[]}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected finalize after reject 409, got %d", res.Code)
	}

	got := getState(t, server)
	if got.Digest.Summary != "Rejected" {
		t.Fatalf("expected digest summary to stay Rejected, got %q", got.Digest.Summary)
	}
}

func serveFinalize(server *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/finalize", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveReject(server *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/reject", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveCreateThread(server *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveMoveThread(server *Server, threadID string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/move", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveReanchorThread(server *Server, threadID string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/reanchor", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveEditThread(server *Server, threadID string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/edit", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveReplyThread(server *Server, threadID string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/reply", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveDeleteThread(server *Server, threadID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/delete", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveThreadKind(server *Server, threadID string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/kind", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveRevisions(server *Server) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/revisions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveRevisionDiff(server *Server, from string, to string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/revisions/"+from+"/diff/"+to, nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveProposeSection(server *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/revisions/propose-section", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveApplyProposal(server *Server, proposalID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/revisions/proposals/"+proposalID+"/apply", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveDiscardProposal(server *Server, proposalID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/revisions/proposals/"+proposalID+"/discard", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveSideQuestion(server *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/side-questions", strings.NewReader(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func servePromoteSideAnswer(server *Server, sideAnswerID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/side-answers/"+sideAnswerID+"/promote", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveUnpromoteSideAnswer(server *Server, sideAnswerID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/side-answers/"+sideAnswerID+"/unpromote", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func serveDigestDraft(server *Server, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/digest/draft", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	return res
}

func digestDraft(t *testing.T, res *httptest.ResponseRecorder) session.Digest {
	t.Helper()

	if res.Code != http.StatusOK {
		t.Fatalf("expected digest draft 200, got %d", res.Code)
	}
	var got session.Digest
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func getState(t *testing.T, server *Server) session.Session {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected state 200, got %d", res.Code)
	}
	var got session.Session
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func assertJSONContentType(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()

	contentType := res.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
}

func assertJSONError(t *testing.T, res *httptest.ResponseRecorder, want string) {
	t.Helper()

	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != want {
		t.Fatalf("expected error %q, got %q", want, got.Error)
	}
}

func diffContains(lines []struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}, kind string, text string) bool {
	for _, line := range lines {
		if line.Kind == kind && line.Text == text {
			return true
		}
	}
	return false
}
