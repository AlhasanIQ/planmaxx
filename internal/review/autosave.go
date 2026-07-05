package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const autosaveVersion = 1

type Autosave struct {
	Version int             `json:"version"`
	SavedAt time.Time       `json:"savedAt"`
	Status  string          `json:"status"`
	Session session.Session `json:"session"`
}

func LoadAutosave(path string) (Autosave, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Autosave{}, false, nil
	}
	if err != nil {
		return Autosave{}, false, err
	}

	var saved Autosave
	if err := json.Unmarshal(data, &saved); err != nil {
		return Autosave{}, false, err
	}
	saved.Session.RestoreCounters()
	return saved, true, nil
}

func writeAutosave(path string, status string, s session.Session) error {
	if path == "" {
		return nil
	}

	payload := Autosave{
		Version: autosaveVersion,
		SavedAt: time.Now().UTC(),
		Status:  status,
		Session: s,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode autosave: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create autosave directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".planmaxx-autosave-*")
	if err != nil {
		return fmt.Errorf("create autosave temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write autosave temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod autosave temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close autosave temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace autosave file: %w", err)
	}
	return nil
}
