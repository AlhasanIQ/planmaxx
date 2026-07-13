package planformat

import "testing"

func TestDetect(t *testing.T) {
	tests := map[string]Format{
		"plan.md":       Markdown,
		"plan.markdown": Markdown,
		"plan.txt":      Markdown,
		"plan":          Markdown,
		"plan.html":     HTML,
		"plan.HTM":      HTML,
	}
	for path, want := range tests {
		if got := Detect(path); got != want {
			t.Errorf("Detect(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestNormalizeUsesKnownFormatOrPath(t *testing.T) {
	if got := Normalize(HTML, "plan.md"); got != HTML {
		t.Fatalf("expected explicit HTML, got %q", got)
	}
	if got := Normalize("", "plan.html"); got != HTML {
		t.Fatalf("expected inferred HTML, got %q", got)
	}
}
