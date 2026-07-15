package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestInspectStorageFileKinds(t *testing.T) {
	dir := t.TempDir()
	if info, err := InspectStorageFile(filepath.Join(dir, "missing")); err != nil || info.Kind != StorageFileMissing {
		t.Fatalf("missing = %+v, %v", info, err)
	}
	jsonPath := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(jsonPath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, err := InspectStorageFile(jsonPath); err != nil || info.Kind != StorageFileLegacyJSON {
		t.Fatalf("json = %+v, %v", info, err)
	}
	invalidPath := filepath.Join(dir, "invalid")
	if err := os.WriteFile(invalidPath, []byte("not storage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, err := InspectStorageFile(invalidPath); err != nil || info.Kind != StorageFileInvalid {
		t.Fatalf("invalid = %+v, %v", info, err)
	}

	legacyBundle, _, ref := createLegacyBundle(t, dir)
	if info, err := InspectStorageFile(legacyBundle); err != nil || info.Kind != StorageFileLegacyBundle || !info.HasRef(ref) {
		t.Fatalf("legacy bundle = %+v, %v", info, err)
	}
	currentPath := filepath.Join(dir, "current.planmaxx")
	store, err := OpenBundleStore(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("session-1", "# Current\n")
	if _, _, err := store.Save("active", NewDocument("/work/current.md", s.Plan), *s); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if info, err := InspectStorageFile(currentPath); err != nil || info.Kind != StorageFileBundle || !info.HasRef(stateRef) {
		t.Fatalf("current bundle = %+v, %v", info, err)
	}
}

func TestImportLegacyRevisionSessionPreservesVisibleHistory(t *testing.T) {
	bundle, commits, ref := createLegacyBundle(t, t.TempDir())
	seed := *session.New("session-1", "seed")
	imported, ok, err := ImportLegacyRevisionHistory(bundle, ref, seed)
	if err != nil || !ok {
		t.Fatalf("import = ok %v err %v", ok, err)
	}
	if len(imported.Revisions) != 2 || imported.Revisions[0].CommitID != commits[0] || imported.Revisions[1].CommitID != commits[1] {
		t.Fatalf("commit identity was not preserved: %+v", imported.Revisions)
	}
	if imported.Revisions[1].ParentID != "rev-1" || imported.Plan != "# Legacy\n\n- Two\n" {
		t.Fatalf("legacy history was not reconstructed: %+v", imported)
	}
}

func TestSnapshotBundleCopiesVerifiedSingleFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "current.planmaxx")
	store, err := OpenBundleStore(source)
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("session-1", "# Plan\n")
	if _, _, err := store.Save("active", NewDocument("/work/plan.md", s.Plan), *s); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	destination := filepath.Join(dir, "snapshots", "review.planmaxx")
	if err := SnapshotBundle(source, destination, false); err != nil {
		t.Fatal(err)
	}
	if info, err := InspectStorageFile(destination); err != nil || info.Kind != StorageFileBundle {
		t.Fatalf("snapshot = %+v, %v", info, err)
	}
	sourceBytes, _ := os.ReadFile(source)
	destinationBytes, _ := os.ReadFile(destination)
	if string(sourceBytes) != string(destinationBytes) {
		t.Fatal("snapshot changed bundle bytes")
	}
	if err := SnapshotBundle(source, destination, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite protection, got %v", err)
	}
	if err := SnapshotBundle(source, destination, true); err != nil {
		t.Fatalf("force snapshot: %v", err)
	}
}

func createLegacyBundle(t *testing.T, dir string) (string, []string, string) {
	t.Helper()
	repository, err := revisions.Open(filepath.Join(dir, "legacy.git"))
	if err != nil {
		t.Fatal(err)
	}
	planID := revisions.PlanID("/work/legacy.md")
	first, err := repository.Commit(planID, plumbing.ZeroHash, "# Legacy\n\n- One\n", "Initial plan")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Commit(planID, first, "# Legacy\n\n- Two\n", "Update plan")
	if err != nil {
		t.Fatal(err)
	}
	ref := revisions.PlanRef(planID)
	bundle := filepath.Join(dir, "revisions.bundle")
	if _, err := gitOutput(repository.Path(), nil, "bundle", "create", bundle, ref); err != nil {
		t.Fatal(err)
	}
	return bundle, []string{first.String(), second.String()}, ref
}
