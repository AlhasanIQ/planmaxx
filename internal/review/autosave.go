package review

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const autosaveVersion = 4
const autosaveFormat = "planmaxx.review"

var (
	ErrFutureAutosave   = errors.New("autosave schema is newer than this PlanMaxx version")
	ErrAutosaveConflict = errors.New("review state changed in another PlanMaxx session")
)

type Document struct {
	CanonicalPath string            `json:"canonicalPath"`
	SourceHash    string            `json:"sourceHash"`
	SourceText    string            `json:"sourceText"`
	PlanFormat    planformat.Format `json:"planFormat"`
}

type Autosave struct {
	Version    int             `json:"version"`
	Format     string          `json:"format,omitempty"`
	Generation uint64          `json:"generation"`
	SavedAt    time.Time       `json:"savedAt"`
	Status     string          `json:"status"`
	Document   Document        `json:"document"`
	Session    session.Session `json:"session"`
}

// revisionJournal bridges the two durable stores used for a review revision:
// Git receives the immutable body first, then the autosave receives the
// metadata containing its commit IDs. A crash between those writes is
// recovered by replaying this exact autosave payload, never by guessing from
// ref names or line positions.
type revisionJournal struct {
	ExpectedGeneration uint64          `json:"expectedGeneration"`
	Status             string          `json:"status"`
	Document           Document        `json:"document"`
	Session            session.Session `json:"session"`
}

type autosaveCommittedError struct{ err error }

func (e autosaveCommittedError) Error() string { return e.err.Error() }
func (e autosaveCommittedError) Unwrap() error { return e.err }

func autosaveWasCommitted(err error) bool {
	var committed autosaveCommittedError
	return errors.As(err, &committed)
}

func NewDocument(path string, source string) Document {
	return Document{
		CanonicalPath: canonicalPath(path),
		SourceHash:    sourceHash(source),
		SourceText:    source,
		PlanFormat:    planformat.Detect(path),
	}
}

func (d Document) MatchesPath(path string) bool {
	return d.CanonicalPath == "" || d.CanonicalPath == canonicalPath(path)
}

func (d Document) SourceMatches(source string) bool {
	return d.SourceHash != "" && d.SourceHash == sourceHash(source)
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
	if err := saved.migrate(); err != nil {
		return Autosave{}, false, err
	}
	saved.Session.RestoreCounters()
	return saved, true, nil
}

func (a *Autosave) migrate() error {
	switch a.Version {
	case 1:
		// Version 1 stored the complete Session but had no durable source
		// baseline. Its plan is the best available source snapshot.
		a.Format = autosaveFormat
		if a.Document.SourceText == "" {
			a.Document.SourceText = a.Session.Plan
			a.Document.SourceHash = sourceHash(a.Session.Plan)
			if a.Session.PlanPath != "" {
				a.Document.CanonicalPath = canonicalPath(a.Session.PlanPath)
			}
		}
		fallthrough
	case 2, 3:
		if a.Format != autosaveFormat {
			return fmt.Errorf("unsupported autosave format %q", a.Format)
		}
		if a.Document.SourceHash == "" || a.Document.SourceHash != sourceHash(a.Document.SourceText) {
			return errors.New("autosave is missing a valid source document baseline")
		}
		a.Version = autosaveVersion
	case autosaveVersion:
		if a.Format != autosaveFormat {
			return fmt.Errorf("unsupported autosave format %q", a.Format)
		}
		if a.Document.SourceHash == "" || a.Document.SourceHash != sourceHash(a.Document.SourceText) {
			return errors.New("autosave is missing a valid source document baseline")
		}
	default:
		if a.Version > autosaveVersion {
			return fmt.Errorf("%w: %d", ErrFutureAutosave, a.Version)
		}
		return fmt.Errorf("unsupported autosave schema version %d", a.Version)
	}
	a.Document.PlanFormat = planformat.Normalize(a.Document.PlanFormat, a.Document.CanonicalPath)
	a.Session.PlanFormat = planformat.Normalize(a.Session.PlanFormat, a.Document.CanonicalPath)
	// The source document owns the format for the entire review history.
	a.Session.PlanFormat = a.Document.PlanFormat
	return nil
}

func writeAutosave(path string, status string, s session.Session, document Document, expectedGeneration uint64) (uint64, error) {
	if path == "" {
		return expectedGeneration, nil
	}
	release, err := acquireAutosaveLock(path)
	if err != nil {
		return expectedGeneration, err
	}
	defer release()
	if saved, ok, err := LoadAutosave(path); err != nil {
		return expectedGeneration, fmt.Errorf("read current autosave: %w", err)
	} else if ok && saved.Generation != expectedGeneration {
		return expectedGeneration, fmt.Errorf("%w (expected generation %d, found %d)", ErrAutosaveConflict, expectedGeneration, saved.Generation)
	} else if !ok && expectedGeneration != 0 {
		return expectedGeneration, fmt.Errorf("%w (expected generation %d, record is missing)", ErrAutosaveConflict, expectedGeneration)
	}

	if document.SourceHash == "" {
		document.SourceText = s.Plan
		document.SourceHash = sourceHash(document.SourceText)
	}
	document.PlanFormat = planformat.Normalize(document.PlanFormat, document.CanonicalPath)
	s.PlanFormat = document.PlanFormat
	payload := Autosave{
		Version:    autosaveVersion,
		Format:     autosaveFormat,
		Generation: expectedGeneration + 1,
		SavedAt:    time.Now().UTC(),
		Status:     status,
		Document:   document,
		Session:    s,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return expectedGeneration, fmt.Errorf("encode autosave: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return expectedGeneration, fmt.Errorf("create autosave directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".planmaxx-autosave-*")
	if err != nil {
		return expectedGeneration, fmt.Errorf("create autosave temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return expectedGeneration, fmt.Errorf("write autosave temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return expectedGeneration, fmt.Errorf("sync autosave temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return expectedGeneration, fmt.Errorf("chmod autosave temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return expectedGeneration, fmt.Errorf("close autosave temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return expectedGeneration, fmt.Errorf("replace autosave file: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return payload.Generation, autosaveCommittedError{fmt.Errorf("open autosave directory after replace: %w", err)}
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return payload.Generation, autosaveCommittedError{fmt.Errorf("sync autosave directory after replace: %w", err)}
	}
	return payload.Generation, nil
}

func revisionJournalPath(autosavePath string) string {
	return autosavePath + ".git-journal"
}

func writeRevisionJournal(autosavePath string, journal revisionJournal) error {
	if autosavePath == "" {
		return nil
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("encode revision journal: %w", err)
	}
	return writePrivateAtomic(revisionJournalPath(autosavePath), append(data, '\n'))
}

func loadRevisionJournal(autosavePath string) (revisionJournal, bool, error) {
	data, err := os.ReadFile(revisionJournalPath(autosavePath))
	if errors.Is(err, os.ErrNotExist) {
		return revisionJournal{}, false, nil
	}
	if err != nil {
		return revisionJournal{}, false, fmt.Errorf("read revision journal: %w", err)
	}
	var journal revisionJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return revisionJournal{}, false, fmt.Errorf("decode revision journal: %w", err)
	}
	return journal, true, nil
}

func clearRevisionJournal(autosavePath string) error {
	if autosavePath == "" {
		return nil
	}
	if err := os.Remove(revisionJournalPath(autosavePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove revision journal: %w", err)
	}
	return nil
}

func writePrivateAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".planmaxx-journal-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

func sourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("sha256:%x", sum[:])
}
