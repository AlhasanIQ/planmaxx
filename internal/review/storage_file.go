package review

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type StorageFileKind string

const (
	StorageFileMissing      StorageFileKind = "missing"
	StorageFileBundle       StorageFileKind = "bundle"
	StorageFileLegacyBundle StorageFileKind = "legacy-bundle"
	StorageFileLegacyJSON   StorageFileKind = "legacy-json"
	StorageFileInvalid      StorageFileKind = "invalid"
)

type StorageFileInfo struct {
	Path string
	Kind StorageFileKind
	Refs []string
}

func (i StorageFileInfo) HasRef(want string) bool {
	for _, ref := range i.Refs {
		if ref == want {
			return true
		}
	}
	return false
}

// InspectStorageFile distinguishes current bundles from both legacy storage
// formats without modifying the input file.
func InspectStorageFile(path string) (StorageFileInfo, error) {
	info := StorageFileInfo{Path: path, Kind: StorageFileMissing}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return info, nil
	}
	if err != nil {
		return info, err
	}
	prefix := make([]byte, 4096)
	read, readErr := file.Read(prefix)
	_ = file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return info, readErr
	}
	prefix = bytes.TrimSpace(prefix[:read])
	if len(prefix) > 0 && prefix[0] == '{' {
		info.Kind = StorageFileLegacyJSON
		return info, nil
	}
	command := gitCommand("bundle", "list-heads", path)
	out, err := command.CombinedOutput()
	if err != nil {
		info.Kind = StorageFileInvalid
		return info, nil
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			info.Refs = append(info.Refs, fields[1])
		}
	}
	if info.HasRef(stateRef) {
		info.Kind = StorageFileBundle
	} else {
		info.Kind = StorageFileLegacyBundle
	}
	return info, nil
}

// SnapshotBundle copies a verified current review bundle to a standalone
// single-file snapshot. Existing destinations require force.
func SnapshotBundle(source, destination string, force bool) error {
	info, err := InspectStorageFile(source)
	if err != nil {
		return err
	}
	if info.Kind != StorageFileBundle {
		return fmt.Errorf("source is not a current PlanMaxx bundle (found %s)", info.Kind)
	}
	if samePath(source, destination) {
		return errors.New("snapshot destination must differ from the active bundle")
	}
	if !force {
		if _, err := os.Stat(destination); err == nil {
			return fmt.Errorf("snapshot destination already exists: %s", destination)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".planmaxx-snapshot-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	src, err := os.Open(source)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, src)
	closeSourceErr := src.Close()
	if copyErr == nil {
		copyErr = closeSourceErr
	}
	if copyErr == nil {
		copyErr = tmp.Sync()
	}
	if copyErr == nil {
		copyErr = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	verification, err := materializeBundle(tmpPath)
	if err != nil {
		return fmt.Errorf("verify snapshot: %w", err)
	}
	_ = os.RemoveAll(verification)
	if err := os.Rename(tmpPath, destination); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return autosaveCommittedError{err}
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return autosaveCommittedError{err}
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
