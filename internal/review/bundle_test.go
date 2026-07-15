package review

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestBundlePathIsStablePrivateAndPerPlan(t *testing.T) {
	root := t.TempDir()
	first := BundlePath(root, "/work/private/project/plan.md")
	if first != BundlePath(root, "/work/private/project/plan.md") {
		t.Fatal("bundle path is not stable")
	}
	if first == BundlePath(root, "/work/private/project/other.md") {
		t.Fatal("different plans shared a bundle path")
	}
	if filepath.Ext(first) != bundleExtension || strings.Contains(filepath.Base(first), "plan.md") {
		t.Fatalf("bundle path exposes the source name or has the wrong extension: %q", first)
	}
}

func TestLocalBundlePathIsBesidePlan(t *testing.T) {
	planPath := filepath.Join("work", "private", "project", "plan.md")
	want := planPath + ".planmaxx"
	if got := LocalBundlePath(planPath); got != want {
		t.Fatalf("LocalBundlePath() = %q, want %q", got, want)
	}
}

func TestBundleRoundTripStoresGitConcepts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.planmaxx")
	store, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	s := session.New("session-1", "# Plan\n\n- Old\n")
	thread := s.AddThread(session.Anchor{StartLine: 3, EndLine: 3}, "Make it concrete")
	firstProposal := s.CreateSectionProposal(session.SectionProposalInput{
		ThreadID: thread.ID, Anchor: thread.Anchor,
		OriginalSection: "- Old", ProposedSection: "- New", ProposedPlan: "# Plan\n\n- New\n",
		Summary: "Improve the step", Instruction: "Make it concrete", IncludedThreadIDs: []string{thread.ID},
	})
	_, ok := s.ApplyProposal(firstProposal.ID)
	if !ok {
		t.Fatal("apply proposal")
	}
	pending := s.CreateSectionProposal(session.SectionProposalInput{
		Anchor: session.Anchor{StartLine: 1, EndLine: 3}, OriginalSection: s.Plan,
		ProposedSection: "# Plan\n\n- Newer\n", ProposedPlan: "# Plan\n\n- Newer\n",
		Summary: "Pending improvement", Instruction: "Polish the plan",
	})
	document := NewDocument("/work/plan.md", "# Plan\n\n- Old\n")

	working, generation, err := store.Save("active", document, *s)
	if err != nil {
		t.Fatal(err)
	}
	if generation != 1 || working.Revisions[0].CommitID == "" || working.Revisions[1].CommitID == "" {
		t.Fatalf("revision commits were not assigned: generation=%d revisions=%+v", generation, working.Revisions)
	}
	if got, err := refOID(store.repoDir, revisionsRef); err != nil || got != working.Revisions[1].CommitID {
		t.Fatalf("revisions ref = %q, %v", got, err)
	}
	proposalOID, err := refOID(store.repoDir, proposalRef)
	if err != nil || proposalOID == "" {
		t.Fatalf("proposal ref = %q, %v", proposalOID, err)
	}
	parents, err := gitOutput(store.repoDir, nil, "rev-list", "--parents", "-n", "1", proposalOID)
	if err != nil || !strings.Contains(string(parents), working.Revisions[1].CommitID) {
		t.Fatalf("proposal does not descend from current revision: %s (%v)", parents, err)
	}
	stateJSON, err := gitOutput(store.repoDir, nil, "show", stateRef+":"+stateFileName)
	if err != nil || !strings.Contains(string(stateJSON), `"plan": ""`) || !strings.Contains(string(stateJSON), `"proposedPlan": ""`) || !strings.Contains(string(stateJSON), `"sourceText": ""`) {
		t.Fatalf("Git-owned bodies leaked into metadata: %s (%v)", stateJSON, err)
	}
	note, err := gitOutput(store.repoDir, nil, "notes", "--ref="+feedbackNotesRef, "show", working.Revisions[1].CommitID)
	if err != nil || !strings.Contains(string(note), "Make it concrete") {
		t.Fatalf("feedback note missing: %s (%v)", note, err)
	}
	if _, err := gitOutput(store.repoDir, nil, "bundle", "verify", path); err != nil {
		t.Fatalf("bundle verification failed: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "review.planmaxx" {
		t.Fatalf("durable directory contains more than the one protocol file: %+v (%v)", entries, err)
	}

	reopened, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, ok := reopened.Load()
	if !ok || saved.Generation != 1 || saved.Session.Revisions[0].Plan != "# Plan\n\n- Old\n" || saved.Session.Revisions[1].Plan != "# Plan\n\n- New\n" {
		t.Fatalf("bundle did not hydrate revision bodies: ok=%v saved=%+v", ok, saved)
	}
	if saved.Session.PendingProposal == nil || saved.Session.PendingProposal.ID != pending.ID || saved.Session.PendingProposal.ProposedPlan != "# Plan\n\n- Newer\n" {
		t.Fatalf("pending proposal did not round trip: %+v", saved.Session.PendingProposal)
	}
}

func TestBundleDetectsConcurrentWriterAndRefreshes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.planmaxx")
	first, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	document := NewDocument("/work/plan.md", "# Plan\n")
	s := session.New("session-1", "# Plan\n")
	if _, _, err := first.Save("active", document, *s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := second.Save("active", document, *s); !errors.Is(err, ErrBundleConflict) {
		t.Fatalf("expected bundle conflict, got %v", err)
	}
	saved, changed, err := second.Refresh()
	if err != nil || !changed || saved.Generation != 1 {
		t.Fatalf("refresh = changed %v generation %d err %v", changed, saved.Generation, err)
	}
	saved.Session.AddTurnRevision("# Plan\n\n- Two\n", "Second")
	if _, generation, err := second.Save("active", document, saved.Session); err != nil || generation != 2 {
		t.Fatalf("save after refresh = generation %d err %v", generation, err)
	}
}

func TestBundleDeletionIsAConflictNotANewEmptyHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.planmaxx")
	store, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := session.New("session-1", "# Plan\n")
	if _, _, err := store.Save("active", NewDocument("/work/plan.md", s.Plan), *s); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Refresh(); !errors.Is(err, ErrBundleConflict) {
		t.Fatalf("expected deleted bundle conflict, got %v", err)
	}
	if _, _, err := store.Save("active", NewDocument("/work/plan.md", s.Plan), *s); !errors.Is(err, ErrBundleConflict) {
		t.Fatalf("expected deleted bundle save conflict, got %v", err)
	}
}

func TestProbeBundleLockDistinguishesHeldLockFromMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.planmaxx")
	held, marker, err := ProbeBundleLock(path)
	if err != nil || held || marker == "" {
		t.Fatalf("unheld probe = held %v marker %q err %v", held, marker, err)
	}
	release, err := acquireAutosaveLock(bundleLockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	held, _, err = ProbeBundleLock(path)
	if err != nil || !held {
		t.Fatalf("held probe = held %v err %v", held, err)
	}
}

func TestBundleFinalizationCreatesAnnotatedTagAndStateHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.planmaxx")
	store, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := session.New("session-1", "# Plan\n")
	document := NewDocument("/work/plan.md", s.Plan)
	working, _, err := store.Save("active", document, *s)
	if err != nil {
		t.Fatal(err)
	}
	working.SetDigest(session.Digest{Summary: "Approved"})
	if _, generation, err := store.Save("finalized", document, working); err != nil || generation != 2 {
		t.Fatalf("finalize generation %d: %v", generation, err)
	}
	if _, generation, err := store.Save("finalized", document, working); err != nil || generation != 3 {
		t.Fatalf("post-finalize baseline generation %d: %v", generation, err)
	}
	count, err := gitOutput(store.repoDir, nil, "rev-list", "--count", stateRef)
	if err != nil || strings.TrimSpace(string(count)) != "3" {
		t.Fatalf("state history count = %q, %v", count, err)
	}
	tags, err := gitOutput(store.repoDir, nil, "for-each-ref", "--format=%(objecttype) %(refname)", "refs/tags/finalized")
	if err != nil || !strings.Contains(string(tags), "tag refs/tags/finalized/") {
		t.Fatalf("annotated finalization tag missing: %s (%v)", tags, err)
	}
	if strings.Count(strings.TrimSpace(string(tags)), "\n") != 0 {
		t.Fatalf("one finalization created multiple tags: %s", tags)
	}
}

func TestBundleLegacyImportPreservesCommitIDs(t *testing.T) {
	legacy, err := revisions.Open(filepath.Join(t.TempDir(), "revisions.git"))
	if err != nil {
		t.Fatal(err)
	}
	planID := revisions.PlanID("/work/plan.md")
	commit, err := legacy.Commit(planID, plumbing.ZeroHash, "# Legacy\n", "Initial")
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("session-1", "# Legacy\n")
	saved := Autosave{Version: autosaveVersion, Format: autosaveFormat, Generation: 7, Status: "canceled", Document: NewDocument("/work/plan.md", "# Legacy\n"), Session: *s}

	store, err := OpenBundleStore(filepath.Join(t.TempDir(), "review.planmaxx"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.WithLegacyAutosave(saved).WithLegacyImport(legacy.Path(), revisions.PlanRef(planID))
	server := NewServer(session.New("ignored", "ignored")).WithAutosaveDocument(saved.Document)
	if err := server.EnableBundle(store); err != nil {
		t.Fatal(err)
	}
	loaded, ok := store.Load()
	if !ok || loaded.Generation != 8 || loaded.Session.Revisions[0].CommitID != commit.String() || loaded.Session.Revisions[0].Plan != "# Legacy\n" {
		t.Fatalf("legacy identity/content not preserved: ok=%v saved=%+v", ok, loaded)
	}
	if oid, err := refOID(store.repoDir, "refs/legacy/planmaxx/revisions"); err != nil || oid != "" {
		t.Fatalf("transient import ref leaked into final bundle: %q, %v", oid, err)
	}
}

func TestBundleBackedServerUsesGitForDiffRestoreAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.planmaxx")
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\n- Old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstStore, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	s := session.New("session-1", "# Plan\n\n- Old\n")
	s.AddTurnRevision("# Plan\n\n- New\n", "Update")
	first := NewServer(s).WithAutosaveDocument(NewDocument(planPath, "# Plan\n\n- Old\n"))
	if err := first.EnableBundle(firstStore); err != nil {
		t.Fatal(err)
	}

	diff := serveRevisionDiff(first, "rev-1", "rev-2")
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), "Old") || !strings.Contains(diff.Body.String(), "New") {
		t.Fatalf("bundle-backed diff failed: %d %s", diff.Code, diff.Body.String())
	}
	restore := httptest.NewRecorder()
	first.Handler().ServeHTTP(restore, httptest.NewRequest(http.MethodPost, "/api/revisions/rev-1/restore", nil))
	if restore.Code != http.StatusOK {
		t.Fatalf("bundle-backed restore failed: %d %s", restore.Code, restore.Body.String())
	}
	if first.session.Plan != "# Plan\n\n- Old\n" || len(first.session.Revisions) != 3 || first.session.Revisions[2].CommitID == "" {
		t.Fatalf("restore did not append a persisted Git revision: %+v", first.session.Revisions)
	}

	secondStore, err := OpenBundleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	second := NewServer(session.New("ignored", "ignored")).WithAutosaveDocument(NewDocument(planPath, "# Plan\n\n- Old\n"))
	if err := second.EnableBundle(secondStore); err != nil {
		t.Fatal(err)
	}
	first.session.AddTurnRevision("# Plan\n\n- Latest\n", "Concurrent update")
	first.mu.Lock()
	err = first.persistLocked()
	first.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	state := httptest.NewRecorder()
	second.Handler().ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), "Latest") {
		t.Fatalf("bundle-backed reload failed: %d %s", state.Code, state.Body.String())
	}
}
