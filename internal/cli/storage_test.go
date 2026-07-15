package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestDoctorReportsBundleLegacyStorageAndProcesses(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath+".planmaxx-review.json", []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath+".planmaxx-review.20260715.json", []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldProcessList := processList
	processList = func() ([]byte, error) { return []byte("123 planmaxx review " + planPath + "\n"), nil }
	t.Cleanup(func() { processList = oldProcessList })
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"doctor", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bundle kind: missing", "Bundle write lock: free", "123 planmaxx review", "legacy-json", "manual JSON snapshot", "run planmaxx review"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestDoctorAndSnapshotReadCurrentBundle(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundlePath := reviewBundlePath(t, planPath)
	store, err := review.OpenBundleStore(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	s := session.New("session-1", "# Plan\n")
	if _, _, err := store.Save("active", review.NewDocument(planPath, s.Plan), *s); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	oldProcessList := processList
	processList = func() ([]byte, error) { return nil, nil }
	t.Cleanup(func() { processList = oldProcessList })
	var doctorOut, doctorErr bytes.Buffer
	doctor := NewRootCommand(&doctorOut, &doctorErr)
	doctor.SetArgs([]string{"doctor", planPath})
	if err := doctor.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bundle kind: bundle", "Bundle validity: valid", "Generation: 1", "Status: active"} {
		if !strings.Contains(doctorOut.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, doctorOut.String())
		}
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.planmaxx")
	var snapshotOut, snapshotErr bytes.Buffer
	snapshot := NewRootCommand(&snapshotOut, &snapshotErr)
	snapshot.SetArgs([]string{"snapshot", "--out", snapshotPath, planPath})
	if err := snapshot.Execute(); err != nil {
		t.Fatal(err)
	}
	if info, err := review.InspectStorageFile(snapshotPath); err != nil || info.Kind != review.StorageFileBundle {
		t.Fatalf("snapshot = %+v, %v", info, err)
	}
}
