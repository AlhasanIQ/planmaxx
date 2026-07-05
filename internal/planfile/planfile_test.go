package planfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsMarkdownPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(path, []byte("# Plan\n\n- Step one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Path != path {
		t.Fatalf("expected path %q, got %q", path, plan.Path)
	}
	if plan.Markdown != "# Plan\n\n- Step one\n" {
		t.Fatalf("unexpected markdown %q", plan.Markdown)
	}
}

func TestLoadRejectsEmptyPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, []byte("   \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected empty plan error")
	}
	if err.Error() != "plan file is empty" {
		t.Fatalf("unexpected error %q", err.Error())
	}
}
