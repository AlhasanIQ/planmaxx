package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommandPrintsVersionMetadata(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"planmaxx", "version", "commit", "date"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected version output to contain %q, got %q", want, output)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestVersionFlagPrintsVersionMetadata(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version flag failed: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"planmaxx", "version", "commit", "date"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected version output to contain %q, got %q", want, output)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}
