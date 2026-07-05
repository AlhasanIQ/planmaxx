package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmokeAndRenderScriptsAreDocumented(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		"scripts/e2e-smoke.sh",
		"scripts/render-review.mjs",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scripts/e2e-smoke.sh", "scripts/render-review.mjs"} {
		if !strings.Contains(string(readme), want) {
			t.Fatalf("expected README to mention %s", want)
		}
	}
}
