package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const updateTestVersion = "1.0.0"

type updateReleaseScenario struct {
	latest       string
	status       int
	archive      []byte
	checksums    string
	omitArchive  bool
	omitChecksum bool
}

type updateReleaseFixture struct {
	server *httptest.Server

	mu       sync.Mutex
	scenario updateReleaseScenario
	hits     map[string]int
}

func newUpdateReleaseFixture(t *testing.T) *updateReleaseFixture {
	t.Helper()

	fixture := &updateReleaseFixture{hits: make(map[string]int)}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *updateReleaseFixture) set(scenario updateReleaseScenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenario = scenario
	f.hits = make(map[string]int)
}

func (f *updateReleaseFixture) hitCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *updateReleaseFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits[r.URL.Path]++
	scenario := f.scenario
	f.mu.Unlock()

	switch r.URL.Path {
	case "/latest":
		if scenario.status != 0 && scenario.status != http.StatusOK {
			http.Error(w, "release service unavailable", scenario.status)
			return
		}
		archiveName := releaseArchiveName(scenario.latest)
		assets := make([]map[string]string, 0, 2)
		if !scenario.omitArchive {
			assets = append(assets, map[string]string{"name": archiveName, "browser_download_url": f.server.URL + "/archive"})
		}
		if !scenario.omitChecksum {
			assets = append(assets, map[string]string{"name": "checksums.txt", "browser_download_url": f.server.URL + "/checksums"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": scenario.latest,
			"html_url": f.server.URL + "/releases/" + scenario.latest,
			"assets":   assets,
		})
	case "/archive":
		_, _ = w.Write(scenario.archive)
	case "/checksums":
		checksums := scenario.checksums
		if checksums == "" {
			checksums = fmt.Sprintf("%x  %s\n", sha256.Sum256(scenario.archive), releaseArchiveName(scenario.latest))
		}
		_, _ = io.WriteString(w, checksums)
	default:
		http.NotFound(w, r)
	}
}

func TestUpdateCommandEndToEnd(t *testing.T) {
	fixture := newUpdateReleaseFixture(t)
	built := buildPlanMaxxForUpdateE2E(t, updateTestVersion, fixture.server.URL+"/latest")

	t.Run("installs a newer verified release", func(t *testing.T) {
		want := []byte("replacement planmaxx executable")
		fixture.set(updateReleaseScenario{latest: "v1.1.0", archive: updateReleaseArchive(t, "planmaxx", want)})
		executable, _ := copyExecutableForUpdateE2E(t, built)

		stdout, stderr, err := runPlanMaxxForUpdateE2E(t, executable, nil, "update")
		if err != nil {
			t.Fatalf("update failed: %v\nstderr:\n%s", err, stderr)
		}
		if stdout != "Updated PlanMaxx from 1.0.0 to 1.1.0.\n" {
			t.Fatalf("unexpected stdout %q", stdout)
		}
		got, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("installed executable = %q, want %q", got, want)
		}
		info, err := os.Stat(executable)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("installed executable mode %o is not executable", info.Mode().Perm())
		}
		if fixture.hitCount("/latest") != 1 || fixture.hitCount("/checksums") != 1 || fixture.hitCount("/archive") != 1 {
			t.Fatalf("unexpected release requests: latest=%d checksums=%d archive=%d", fixture.hitCount("/latest"), fixture.hitCount("/checksums"), fixture.hitCount("/archive"))
		}
	})

	if runtime.GOOS != "windows" {
		t.Run("updates the target of an invoked symlink", func(t *testing.T) {
			want := []byte("replacement behind symlink")
			fixture.set(updateReleaseScenario{latest: "v1.1.0", archive: updateReleaseArchive(t, "planmaxx", want)})
			executable, _ := copyExecutableForUpdateE2E(t, built)
			link := filepath.Join(t.TempDir(), "planmaxx")
			if err := os.Symlink(executable, link); err != nil {
				t.Fatal(err)
			}

			_, stderr, err := runPlanMaxxForUpdateE2E(t, link, nil, "update")
			if err != nil {
				t.Fatalf("symlinked update failed: %v\nstderr:\n%s", err, stderr)
			}
			got, err := os.ReadFile(executable)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("symlink target = %q, want %q", got, want)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("invocation path stopped being a symlink: info=%v err=%v", info, err)
			}
		})
	}

	for _, test := range []struct {
		name   string
		latest string
	}{
		{name: "already current", latest: "v1.0.0"},
		{name: "does not downgrade", latest: "v0.9.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.set(updateReleaseScenario{latest: test.latest, archive: updateReleaseArchive(t, "planmaxx", []byte("unused"))})
			executable, original := copyExecutableForUpdateE2E(t, built)

			stdout, stderr, err := runPlanMaxxForUpdateE2E(t, executable, nil, "update")
			if err != nil {
				t.Fatalf("update check failed: %v\nstderr:\n%s", err, stderr)
			}
			if stdout != "PlanMaxx 1.0.0 is already up to date.\n" {
				t.Fatalf("unexpected stdout %q", stdout)
			}
			assertExecutableUnchanged(t, executable, original)
			if fixture.hitCount("/archive") != 0 || fixture.hitCount("/checksums") != 0 {
				t.Fatalf("an unavailable update should not download assets: checksums=%d archive=%d", fixture.hitCount("/checksums"), fixture.hitCount("/archive"))
			}
		})
	}

	for _, test := range []struct {
		name     string
		scenario updateReleaseScenario
		wantErr  string
	}{
		{
			name:     "rejects a checksum mismatch",
			scenario: updateReleaseScenario{latest: "v1.1.0", archive: updateReleaseArchive(t, "planmaxx", []byte("untrusted")), checksums: strings.Repeat("0", 64) + "  " + releaseArchiveName("v1.1.0") + "\n"},
			wantErr:  "checksum mismatch",
		},
		{
			name:     "rejects a corrupt archive",
			scenario: updateReleaseScenario{latest: "v1.1.0", archive: []byte("not a gzip archive")},
			wantErr:  "extract",
		},
		{
			name:     "rejects an archive without the executable",
			scenario: updateReleaseScenario{latest: "v1.1.0", archive: updateReleaseArchive(t, "README.md", []byte("missing executable"))},
			wantErr:  "archive does not contain",
		},
		{
			name:     "rejects a release missing the platform archive",
			scenario: updateReleaseScenario{latest: "v1.1.0", omitArchive: true},
			wantErr:  "does not contain " + releaseArchiveName("v1.1.0"),
		},
		{
			name:     "rejects a release missing checksums",
			scenario: updateReleaseScenario{latest: "v1.1.0", omitChecksum: true},
			wantErr:  "does not contain checksums.txt",
		},
		{
			name:     "reports release service failures",
			scenario: updateReleaseScenario{latest: "v1.1.0", status: http.StatusServiceUnavailable},
			wantErr:  "HTTP 503",
		},
		{
			name:     "rejects an invalid release version",
			scenario: updateReleaseScenario{latest: "latest", archive: updateReleaseArchive(t, "planmaxx", []byte("unused"))},
			wantErr:  "invalid release version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.set(test.scenario)
			executable, original := copyExecutableForUpdateE2E(t, built)

			stdout, stderr, err := runPlanMaxxForUpdateE2E(t, executable, nil, "update")
			if err == nil {
				t.Fatalf("expected update failure, stdout=%q", stdout)
			}
			if !strings.Contains(stderr, test.wantErr) {
				t.Fatalf("stderr %q does not contain %q", stderr, test.wantErr)
			}
			assertExecutableUnchanged(t, executable, original)
		})
	}
}

func TestUpdateCommandRejectsDevelopmentBuildEndToEnd(t *testing.T) {
	fixture := newUpdateReleaseFixture(t)
	built := buildPlanMaxxForUpdateE2E(t, "dev", fixture.server.URL+"/latest")
	fixture.set(updateReleaseScenario{latest: "v1.1.0"})

	stdout, stderr, err := runPlanMaxxForUpdateE2E(t, built, nil, "update")
	if err == nil {
		t.Fatalf("expected development build to reject updates, stdout=%q", stdout)
	}
	assertContains(t, stderr, "cannot update a development build")
	if fixture.hitCount("/latest") != 0 {
		t.Fatalf("development build made %d release requests", fixture.hitCount("/latest"))
	}
}

func TestReviewUpdateCheckEndToEnd(t *testing.T) {
	fixture := newUpdateReleaseFixture(t)
	built := buildPlanMaxxForUpdateE2E(t, updateTestVersion, fixture.server.URL+"/latest")
	validRelease := updateReleaseScenario{latest: "v1.1.0", archive: updateReleaseArchive(t, "planmaxx", []byte("unused"))}

	t.Run("adds an agent notice to the finalized handoff", func(t *testing.T) {
		fixture.set(validRelease)
		review := startBuiltReviewForUpdateE2E(t, built, nil)
		finalize(t, review.url, digest("Update notice approved", nil, nil))
		waitSuccess(t, review)

		output := review.stdout.String()
		assertContains(t, output, "A PlanMaxx update is available (1.0.0 -> 1.1.0).")
		assertContains(t, output, "Tell the user briefly that an update exists for PlanMaxx.")
		assertContains(t, output, "run `planmaxx update`")
	})

	t.Run("disabled checks make no request and add no notice", func(t *testing.T) {
		fixture.set(validRelease)
		review := startBuiltReviewForUpdateE2E(t, built, map[string]string{"PLANMAXX_NO_UPDATE_CHECK": "1"})
		finalize(t, review.url, digest("Disabled update check approved", nil, nil))
		waitSuccess(t, review)

		if strings.Contains(review.stdout.String(), "Agent maintenance notice") {
			t.Fatalf("disabled update check added a notice: %q", review.stdout.String())
		}
		if fixture.hitCount("/latest") != 0 {
			t.Fatalf("disabled update check made %d release requests", fixture.hitCount("/latest"))
		}
	})

	t.Run("release service failure stays silent and review succeeds", func(t *testing.T) {
		fixture.set(updateReleaseScenario{latest: "v1.1.0", status: http.StatusServiceUnavailable})
		review := startBuiltReviewForUpdateE2E(t, built, nil)
		finalize(t, review.url, digest("Failed update check approved", nil, nil))
		waitSuccess(t, review)

		if strings.Contains(review.stdout.String(), "Agent maintenance notice") {
			t.Fatalf("failed update check added a notice: %q", review.stdout.String())
		}
		if fixture.hitCount("/latest") != 1 {
			t.Fatalf("failed update check made %d release requests, want 1", fixture.hitCount("/latest"))
		}
	})
}

func buildPlanMaxxForUpdateE2E(t *testing.T, version string, latestURL string) string {
	t.Helper()

	output := filepath.Join(t.TempDir(), "planmaxx")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	ldflags := fmt.Sprintf(
		"-X github.com/AlhasanIQ/planmaxx/internal/version.Version=%s -X github.com/AlhasanIQ/planmaxx/internal/updater.latestReleaseURL=%s",
		version,
		latestURL,
	)
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", output, "./cmd/planmaxx")
	cmd.Dir = repoRoot(t)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build PlanMaxx update e2e binary: %v\n%s", err, data)
	}
	return output
}

func copyExecutableForUpdateE2E(t *testing.T, source string) (string, []byte) {
	t.Helper()

	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), filepath.Base(source))
	if err := os.WriteFile(target, content, 0o755); err != nil {
		t.Fatal(err)
	}
	return target, content
}

func runPlanMaxxForUpdateE2E(t *testing.T, executable string, env map[string]string, args ...string) (string, string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = mergeEnv(nil, env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("PlanMaxx command timed out: %v", ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

func startBuiltReviewForUpdateE2E(t *testing.T, executable string, env map[string]string) *reviewProcess {
	t.Helper()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte(realisticPlan("Updater startup check")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	cmd := exec.CommandContext(ctx, executable, "review", "--no-browser", planPath)
	isolatedHome := t.TempDir()
	cmd.Env = mergeEnv(map[string]string{
		"HOME":                     isolatedHome,
		"XDG_CACHE_HOME":           filepath.Join(isolatedHome, "cache"),
		"CODEX_THREAD_ID":          "",
		"PLANMAXX_NO_UPDATE_CHECK": "",
	}, env)
	stdout := &lockedBuffer{}
	stderr := &lockedBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	review := &reviewProcess{stdout: stdout, stderr: stderr, done: make(chan error, 1), cancel: cancel}
	go func() { review.done <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-review.done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})
	review.url = waitForReviewURL(t, stderr)
	return review
}

func releaseArchiveName(version string) string {
	return fmt.Sprintf("planmaxx_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
}

func updateReleaseArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	if runtime.GOOS == "windows" && name == "planmaxx" {
		name += ".exe"
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
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

func assertExecutableUnchanged(t *testing.T, executable string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("failed update changed the executable")
	}
}
