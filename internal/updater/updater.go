package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	selfapply "github.com/creativeprojects/go-selfupdate/update"
	"golang.org/x/mod/semver"
)

const (
	checksumsAsset         = "checksums.txt"
	githubLatestRelease    = "https://api.github.com/repos/AlhasanIQ/planmaxx/releases/latest"
	maxReleaseResponse     = 2 << 20
	maxChecksumsResponse   = 1 << 20
	maxReleaseArchive      = 128 << 20
	maxExecutableInArchive = 128 << 20
)

var (
	ErrDevelopmentBuild = errors.New("updates are unavailable for development builds")
	httpClient          = http.DefaultClient
	latestReleaseURL    = githubLatestRelease
	executablePath      = resolveExecutablePath
	applyExecutable     = replaceExecutable
)

type Status struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseURL      string
	UpdateAvailable bool
}

type cacheRecord struct {
	CheckedAt     time.Time `json:"checkedAt"`
	LatestVersion string    `json:"latestVersion,omitempty"`
	ReleaseURL    string    `json:"releaseURL,omitempty"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	URL     string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type candidate struct {
	status       Status
	archive      releaseAsset
	checksumsURL string
}

func Check(ctx context.Context, currentVersion string) (Status, error) {
	candidate, err := detect(ctx, currentVersion)
	if err != nil {
		return Status{}, err
	}
	return candidate.status, nil
}

func CheckCached(ctx context.Context, currentVersion, cachePath string, maxAge time.Duration, now time.Time) (Status, error) {
	return checkCached(ctx, currentVersion, cachePath, maxAge, now, Check)
}

func Update(ctx context.Context, currentVersion string) (Status, bool, error) {
	candidate, err := detect(ctx, currentVersion)
	if err != nil || !candidate.status.UpdateAvailable {
		return candidate.status, false, err
	}

	checksums, err := download(ctx, candidate.checksumsURL, maxChecksumsResponse)
	if err != nil {
		return candidate.status, false, fmt.Errorf("download %s: %w", checksumsAsset, err)
	}
	archive, err := download(ctx, candidate.archive.URL, maxReleaseArchive)
	if err != nil {
		return candidate.status, false, fmt.Errorf("download %s: %w", candidate.archive.Name, err)
	}
	if err := verifyChecksum(candidate.archive.Name, archive, checksums); err != nil {
		return candidate.status, false, err
	}

	binary, err := extractExecutable(archive)
	if err != nil {
		return candidate.status, false, fmt.Errorf("extract %s: %w", candidate.archive.Name, err)
	}
	target, err := executablePath()
	if err != nil {
		return candidate.status, false, fmt.Errorf("locate PlanMaxx executable: %w", err)
	}
	if err := applyExecutable(bytes.NewReader(binary), target); err != nil {
		return candidate.status, false, fmt.Errorf("install PlanMaxx %s: %w", candidate.status.LatestVersion, err)
	}
	return candidate.status, true, nil
}

func detect(ctx context.Context, currentVersion string) (candidate, error) {
	if _, err := normalizeVersion(currentVersion); err != nil {
		return candidate{}, err
	}

	body, err := download(ctx, latestReleaseURL, maxReleaseResponse)
	if err != nil {
		return candidate{}, fmt.Errorf("check GitHub releases: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return candidate{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	status, err := statusFromVersions(currentVersion, release.TagName, release.URL)
	if err != nil {
		return candidate{}, err
	}

	version := strings.TrimPrefix(status.LatestVersion, "v")
	archiveName := fmt.Sprintf("planmaxx_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	result := candidate{status: status}
	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			result.archive = asset
		case checksumsAsset:
			result.checksumsURL = asset.URL
		}
	}
	if result.archive.URL == "" {
		return candidate{}, fmt.Errorf("release %s does not contain %s", status.LatestVersion, archiveName)
	}
	if result.checksumsURL == "" {
		return candidate{}, fmt.Errorf("release %s does not contain %s", status.LatestVersion, checksumsAsset)
	}
	return result, nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "planmaxx")
	response, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func verifyChecksum(filename string, content, checksums []byte) error {
	var expected string
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == filename {
			expected = fields[0]
			break
		}
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("checksum for %s was not found", filename)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return fmt.Errorf("checksum for %s is invalid: %w", filename, err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(content))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", filename)
	}
	return nil
}

func extractExecutable(archive []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	name := "planmaxx"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Base(path.Clean(header.Name)) != name {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("%s is not a regular file", name)
		}
		if header.Size < 0 || header.Size > maxExecutableInArchive {
			return nil, fmt.Errorf("%s exceeds %d bytes", name, maxExecutableInArchive)
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, maxExecutableInArchive+1))
		if err != nil {
			return nil, err
		}
		if int64(len(binary)) > maxExecutableInArchive {
			return nil, fmt.Errorf("%s exceeds %d bytes", name, maxExecutableInArchive)
		}
		return binary, nil
	}
	return nil, fmt.Errorf("archive does not contain %s", name)
}

func resolveExecutablePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}

func replaceExecutable(source io.Reader, target string) error {
	err := selfapply.Apply(source, selfapply.Options{TargetPath: target, TargetMode: 0o755})
	if rollbackErr := selfapply.RollbackError(err); rollbackErr != nil {
		return fmt.Errorf("replace failed (%v) and rollback failed (%v)", err, rollbackErr)
	}
	return err
}

func statusFromVersions(currentVersion, latestVersion, releaseURL string) (Status, error) {
	current, err := normalizeVersion(currentVersion)
	if err != nil {
		return Status{}, err
	}
	latest, err := normalizeVersion(latestVersion)
	if err != nil {
		return Status{}, fmt.Errorf("invalid release version %q: %w", latestVersion, err)
	}
	return Status{
		CurrentVersion:  strings.TrimPrefix(current, "v"),
		LatestVersion:   strings.TrimPrefix(latest, "v"),
		ReleaseURL:      releaseURL,
		UpdateAvailable: semver.Compare(latest, current) > 0,
	}, nil
}

func normalizeVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "dev" {
		return "", ErrDevelopmentBuild
	}
	normalized := "v" + strings.TrimPrefix(value, "v")
	if !semver.IsValid(normalized) {
		return "", fmt.Errorf("invalid PlanMaxx version %q", value)
	}
	return normalized, nil
}

func checkCached(
	ctx context.Context,
	currentVersion string,
	cachePath string,
	maxAge time.Duration,
	now time.Time,
	check func(context.Context, string) (Status, error),
) (Status, error) {
	if cached, ok := readFreshCache(cachePath, maxAge, now); ok {
		if cached.LatestVersion == "" {
			return Status{CurrentVersion: strings.TrimPrefix(currentVersion, "v")}, nil
		}
		return statusFromVersions(currentVersion, cached.LatestVersion, cached.ReleaseURL)
	}

	status, err := check(ctx, currentVersion)
	if err != nil {
		return Status{}, err
	}
	_ = writeCache(cachePath, cacheRecord{
		CheckedAt:     now.UTC(),
		LatestVersion: status.LatestVersion,
		ReleaseURL:    status.ReleaseURL,
	})
	return status, nil
}

func readFreshCache(path string, maxAge time.Duration, now time.Time) (cacheRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheRecord{}, false
	}
	var cached cacheRecord
	if err := json.Unmarshal(data, &cached); err != nil || cached.CheckedAt.IsZero() {
		return cacheRecord{}, false
	}
	age := now.Sub(cached.CheckedAt)
	return cached, age >= 0 && age <= maxAge
}

func writeCache(path string, cached cacheRecord) error {
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
