//go:build windows

package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDurabilityHelpersSupportAtomicWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.planmaxx")
	if err := os.WriteFile(path, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncRegularFile(path); err != nil {
		t.Fatalf("sync writable file: %v", err)
	}
	if err := syncDirectory(dir); err != nil {
		t.Fatalf("Windows directory sync must be a supported no-op: %v", err)
	}
}
