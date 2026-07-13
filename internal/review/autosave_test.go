package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestLoadAutosaveMigratesVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	s := session.New("session-1", "# Plan\n")
	s.AddThread(session.Anchor{StartLine: 1, EndLine: 1}, "Keep this")
	data, err := json.Marshal(struct {
		Version int             `json:"version"`
		Status  string          `json:"status"`
		Session session.Session `json:"session"`
	}{Version: 1, Status: "canceled", Session: *s})
	if err != nil {
		t.Fatalf("marshal v1 autosave: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write v1 autosave: %v", err)
	}

	saved, ok, err := LoadAutosave(path)
	if err != nil || !ok {
		t.Fatalf("load migrated autosave: ok=%v err=%v", ok, err)
	}
	if saved.Version != autosaveVersion || saved.Format != autosaveFormat {
		t.Fatalf("expected migrated version %d/%q, got %+v", autosaveVersion, autosaveFormat, saved)
	}
	if !saved.Document.SourceMatches("# Plan\n") {
		t.Fatalf("expected v1 plan to become source baseline, got %+v", saved.Document)
	}
	if saved.Document.PlanFormat != planformat.Markdown || saved.Session.PlanFormat != planformat.Markdown {
		t.Fatalf("expected migrated Markdown format, got document=%q session=%q", saved.Document.PlanFormat, saved.Session.PlanFormat)
	}
	if len(saved.Session.Threads) != 1 || saved.Session.Threads[0].ID != "thread-1" {
		t.Fatalf("expected comment to survive migration, got %+v", saved.Session.Threads)
	}
}

func TestLoadAutosaveMigratesVersionTwoHTMLFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	document := NewDocument(filepath.Join(t.TempDir(), "plan.html"), "<h1>Plan</h1>")
	document.PlanFormat = ""
	payload, err := json.Marshal(Autosave{
		Version:  2,
		Format:   autosaveFormat,
		Document: document,
		Session:  *session.New("session-1", "<h1>Plan</h1>"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	saved, ok, err := LoadAutosave(path)
	if err != nil || !ok {
		t.Fatalf("load migrated v2 autosave: ok=%v err=%v", ok, err)
	}
	if saved.Version != autosaveVersion || saved.Document.PlanFormat != planformat.HTML || saved.Session.PlanFormat != planformat.HTML {
		t.Fatalf("unexpected migrated HTML autosave %+v", saved)
	}
}

func TestLoadAutosaveRejectsFutureVersionWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	original := `{"version":99,"format":"planmaxx.review","session":{}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write future autosave: %v", err)
	}
	if _, _, err := LoadAutosave(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected future-version error, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read future autosave: %v", err)
	}
	if string(data) != original {
		t.Fatalf("future autosave was modified: %q", data)
	}
}

func TestLoadAutosaveRejectsMissingSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write incomplete autosave: %v", err)
	}
	if _, _, err := LoadAutosave(path); err == nil || !strings.Contains(err.Error(), "unsupported autosave schema") {
		t.Fatalf("expected missing-schema error, got %v", err)
	}
}

func TestLoadAutosaveAcceptsEmptySourceSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	document := NewDocument(filepath.Join(t.TempDir(), "plan.md"), "")
	payload, err := json.Marshal(Autosave{
		Version:  autosaveVersion,
		Format:   autosaveFormat,
		Document: document,
		Session:  *session.New("session-1", ""),
	})
	if err != nil {
		t.Fatalf("marshal empty-source autosave: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write empty-source autosave: %v", err)
	}
	saved, ok, err := LoadAutosave(path)
	if err != nil || !ok || !saved.Document.SourceMatches("") {
		t.Fatalf("expected empty source snapshot to load, got saved=%+v ok=%v err=%v", saved, ok, err)
	}
}
