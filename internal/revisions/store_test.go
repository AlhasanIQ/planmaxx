package revisions

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestBareStoreCommitsReadsAndAdvancesNamespacedHead(t *testing.T) {
	store, err := Open(t.TempDir() + "/planmaxx.git")
	if err != nil {
		t.Fatal(err)
	}
	planID := PlanID("/repo/PLAN.md")
	first, err := store.Commit(planID, [20]byte{}, "# Plan\n- One\n", "initial")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Commit(planID, first, "# Plan\n- Two\n", "update")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.Head(planID); err != nil || got != second {
		t.Fatalf("head = %s, %v", got, err)
	}
	if got, err := store.Read(first); err != nil || got != "# Plan\n- One\n" {
		t.Fatalf("first content = %q, %v", got, err)
	}
	if got, err := store.Read(second); err != nil || got != "# Plan\n- Two\n" {
		t.Fatalf("second content = %q, %v", got, err)
	}
}

func TestReadSupportsLegacyPlanMarkdownBlob(t *testing.T) {
	store, err := Open(t.TempDir() + "/planmaxx.git")
	if err != nil {
		t.Fatal(err)
	}
	blob, err := store.writeBlob("legacy content")
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.writeTree(&object.Tree{Entries: []object.TreeEntry{{
		Name: legacyPlanFileName,
		Mode: filemode.Regular,
		Hash: blob,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1, 0).UTC()
	commitHash, err := store.writeCommit(&object.Commit{
		TreeHash:  treeHash,
		Author:    object.Signature{Name: "PlanMaxx", Email: "planmaxx@local", When: when},
		Committer: object.Signature{Name: "PlanMaxx", Email: "planmaxx@local", When: when},
		Message:   "legacy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(commitHash); err != nil || got != "legacy content" {
		t.Fatalf("legacy content = %q, %v", got, err)
	}
}

func TestRestoreAppendsHistoricalContentWithoutRewritingHead(t *testing.T) {
	store, err := Open(t.TempDir() + "/planmaxx.git")
	if err != nil {
		t.Fatal(err)
	}
	id := PlanID("/repo/PLAN.md")
	first, _ := store.Commit(id, [20]byte{}, "one", "one")
	second, _ := store.Commit(id, first, "two", "two")
	restored, err := store.Restore(id, second, first)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Read(restored); got != "one" {
		t.Fatalf("restore content %q", got)
	}
	if head, _ := store.Head(id); head != restored || restored == first {
		t.Fatalf("head %s restored %s", head, restored)
	}
}

func TestConcurrentStoresRejectStaleHeadInsteadOfOverwritingIt(t *testing.T) {
	path := t.TempDir() + "/planmaxx.git"
	firstStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	secondStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id := PlanID("/repo/PLAN.md")
	base, err := firstStore.Commit(id, plumbing.ZeroHash, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*Store{firstStore, secondStore} {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			_, err := store.Commit(id, base, "two", "two")
			results <- err
		}(store)
	}
	wg.Wait()
	close(results)
	successes, stale := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrHeadChanged) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("expected one winner and one stale writer, successes=%d stale=%d", successes, stale)
	}
}

func TestMigratePlanFromPreservesCommitIDsAndContent(t *testing.T) {
	legacy, err := Open(t.TempDir() + "/legacy.git")
	if err != nil {
		t.Fatal(err)
	}
	id := PlanID("/repo/PLAN.md")
	first, err := legacy.Commit(id, plumbing.ZeroHash, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	head, err := legacy.Commit(id, first, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	durable, err := Open(t.TempDir() + "/durable.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := durable.MigratePlanFrom(legacy, id); err != nil {
		t.Fatal(err)
	}
	if got, err := durable.Head(id); err != nil || got != head {
		t.Fatalf("migrated head %s, %v", got, err)
	}
	if got, err := durable.Read(first); err != nil || got != "one" {
		t.Fatalf("migrated ancestor %q, %v", got, err)
	}
}
