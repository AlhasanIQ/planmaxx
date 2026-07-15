package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateDownloadsVerifiesAndReplacesExecutable(t *testing.T) {
	archiveName := fmt.Sprintf("planmaxx_0.3.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	wantBinary := []byte("new planmaxx binary")
	archive := releaseArchive(t, wantBinary)
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v0.3.0",
				URL:     server.URL + "/release/v0.3.0",
				Assets: []releaseAsset{
					{Name: archiveName, URL: server.URL + "/archive"},
					{Name: checksumsAsset, URL: server.URL + "/checksums"},
				},
			})
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksums":
			_, _ = io.WriteString(w, checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldURL := latestReleaseURL
	oldClient := httpClient
	oldExecutablePath := executablePath
	oldApply := applyExecutable
	latestReleaseURL = server.URL + "/latest"
	httpClient = server.Client()
	executablePath = func() (string, error) { return "/tmp/fake-planmaxx", nil }
	var applied []byte
	applyExecutable = func(source io.Reader, target string) error {
		if target != "/tmp/fake-planmaxx" {
			t.Fatalf("unexpected update target %q", target)
		}
		var err error
		applied, err = io.ReadAll(source)
		return err
	}
	t.Cleanup(func() {
		latestReleaseURL = oldURL
		httpClient = oldClient
		executablePath = oldExecutablePath
		applyExecutable = oldApply
	})

	status, updated, err := Update(context.Background(), "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !updated || status.LatestVersion != "0.3.0" {
		t.Fatalf("unexpected update result: %+v, updated=%v", status, updated)
	}
	if !bytes.Equal(applied, wantBinary) {
		t.Fatalf("applied binary = %q, want %q", applied, wantBinary)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	err := verifyChecksum("planmaxx.tar.gz", []byte("archive"), []byte(strings.Repeat("0", 64)+"  planmaxx.tar.gz\n"))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestReplaceExecutableSwapsFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "planmaxx")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(strings.NewReader("new"), target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("updated executable = %q, want %q", content, "new")
	}
}

func TestStatusFromVersions(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		latest    string
		available bool
	}{
		{name: "newer", current: "0.2.0", latest: "0.3.0", available: true},
		{name: "same", current: "v0.2.0", latest: "0.2.0", available: false},
		{name: "older release", current: "0.3.0", latest: "0.2.0", available: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := statusFromVersions(tt.current, tt.latest, "https://example.test/release")
			if err != nil {
				t.Fatal(err)
			}
			if status.UpdateAvailable != tt.available {
				t.Fatalf("UpdateAvailable = %v, want %v", status.UpdateAvailable, tt.available)
			}
		})
	}
}

func TestStatusFromVersionsRejectsDevelopmentBuild(t *testing.T) {
	_, err := statusFromVersions("dev", "0.3.0", "")
	if !errors.Is(err, ErrDevelopmentBuild) {
		t.Fatalf("expected ErrDevelopmentBuild, got %v", err)
	}
}

func TestCheckCachedUsesFreshResult(t *testing.T) {
	now := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := writeCache(path, cacheRecord{
		CheckedAt:     now.Add(-time.Hour),
		LatestVersion: "0.3.0",
		ReleaseURL:    "https://example.test/v0.3.0",
	}); err != nil {
		t.Fatal(err)
	}

	called := false
	status, err := checkCached(context.Background(), "0.2.0", path, 24*time.Hour, now, func(context.Context, string) (Status, error) {
		called = true
		return Status{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected fresh cache to avoid a network check")
	}
	if !status.UpdateAvailable || status.LatestVersion != "0.3.0" {
		t.Fatalf("unexpected cached status: %+v", status)
	}
}

func TestCheckCachedRefreshesStaleResult(t *testing.T) {
	now := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := writeCache(path, cacheRecord{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "0.2.0"}); err != nil {
		t.Fatal(err)
	}

	status, err := checkCached(context.Background(), "0.2.0", path, 24*time.Hour, now, func(context.Context, string) (Status, error) {
		return Status{
			CurrentVersion:  "0.2.0",
			LatestVersion:   "0.4.0",
			ReleaseURL:      "https://example.test/v0.4.0",
			UpdateAvailable: true,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.UpdateAvailable || status.LatestVersion != "0.4.0" {
		t.Fatalf("unexpected refreshed status: %+v", status)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("expected refreshed cache data")
	}
}

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	name := "planmaxx"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
