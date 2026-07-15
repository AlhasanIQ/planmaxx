// Package revisions stores immutable plan bodies in a PlanMaxx-managed bare
// Git repository. Review semantics remain in session metadata; Git owns blobs,
// commits, parents, refs, and durable revision content.
package revisions

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	planFileName       = "plan.source"
	legacyPlanFileName = "plan.md"
)

var ErrHeadChanged = errors.New("revision head changed in another PlanMaxx process")

type Store struct {
	repo *git.Repository
	path string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create revision store parent: %w", err)
	}
	release, err := acquireFileLock(repositoryLockPath(path))
	if err != nil {
		return nil, err
	}
	defer release()
	repo, err := git.PlainOpen(path)
	if err == git.ErrRepositoryNotExists {
		repo, err = git.PlainInit(path, true)
	}
	if err != nil {
		return nil, fmt.Errorf("open bare revision store: %w", err)
	}
	return &Store{repo: repo, path: path}, nil
}

// OpenExisting opens a store without creating one. It is used only to migrate
// the old cache-backed location into durable application data.
func OpenExisting(path string) (*Store, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("inspect existing bare revision store: %w", err)
	}
	release, err := acquireFileLock(repositoryLockPath(path))
	if err != nil {
		return nil, false, err
	}
	defer release()
	repo, err := git.PlainOpen(path)
	if err == git.ErrRepositoryNotExists {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open existing bare revision store: %w", err)
	}
	return &Store{repo: repo, path: path}, true, nil
}

func PlanID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", sum[:16])
}

// PlanRef is the legacy ref imported into a per-plan review bundle.
func PlanRef(planID string) string { return refName(planID).String() }

// Path exposes the legacy repository location for a one-time native Git fetch.
func (s *Store) Path() string { return s.path }

func (s *Store) HasPlan(planID string) bool {
	_, err := s.Head(planID)
	return err == nil
}

func (s *Store) Head(planID string) (plumbing.Hash, error) {
	ref, err := s.repo.Reference(refName(planID), true)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return ref.Hash(), nil
}

func (s *Store) Commit(planID string, parent plumbing.Hash, content, message string) (plumbing.Hash, error) {
	var committed plumbing.Hash
	err := s.withRepositoryWriteLock(func() error {
		current, err := s.Head(planID)
		if err != nil && err != plumbing.ErrReferenceNotFound {
			return err
		}
		if current != parent {
			return fmt.Errorf("%w: expected %s, found %s", ErrHeadChanged, parent, current)
		}
		blobHash, err := s.writeBlob(content)
		if err != nil {
			return err
		}
		tree := &object.Tree{Entries: []object.TreeEntry{{Name: planFileName, Mode: filemode.Regular, Hash: blobHash}}}
		treeHash, err := s.writeTree(tree)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		commit := &object.Commit{TreeHash: treeHash, Author: object.Signature{Name: "PlanMaxx", Email: "planmaxx@local", When: now}, Committer: object.Signature{Name: "PlanMaxx", Email: "planmaxx@local", When: now}, Message: message}
		if !parent.IsZero() {
			commit.ParentHashes = []plumbing.Hash{parent}
		}
		committed, err = s.writeCommit(commit)
		if err != nil {
			return err
		}
		if err := s.repo.Storer.SetReference(plumbing.NewHashReference(refName(planID), committed)); err != nil {
			return fmt.Errorf("advance plan ref: %w", err)
		}
		return nil
	})
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return committed, nil
}

// WithPlanTransaction serializes all durable work for one logical plan across
// PlanMaxx processes. Callers hold it across Git, journal, and autosave writes.
func (s *Store) WithPlanTransaction(planID string, fn func() error) error {
	release, err := acquireFileLock(runtimeLockPath("plan-"+planID, s.path))
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func (s *Store) withRepositoryWriteLock(fn func() error) error {
	release, err := acquireFileLock(repositoryLockPath(s.path))
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func repositoryLockPath(path string) string { return runtimeLockPath("repository", path) }

func runtimeLockPath(kind, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(os.TempDir(), "planmaxx-locks", fmt.Sprintf("legacy-%s-%x.lock", kind, sum[:16]))
}

func (s *Store) Read(hash plumbing.Hash) (string, error) {
	commit, err := s.repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("read revision commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	file, err := tree.File(planFileName)
	if err != nil {
		file, err = tree.File(legacyPlanFileName)
	}
	if err != nil {
		return "", err
	}
	r, err := file.Reader()
	if err != nil {
		return "", err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		return "", err
	}
	return out.String(), nil
}

// MigratePlanFrom imports the reachable object graph and head ref for one plan
// from the former cache-backed store. It never overwrites an existing durable
// application-data ref.
func (s *Store) MigratePlanFrom(source *Store, planID string) error {
	if source == nil || source.path == s.path {
		return nil
	}
	head, err := source.Head(planID)
	if err == plumbing.ErrReferenceNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy plan ref: %w", err)
	}
	return s.withRepositoryWriteLock(func() error {
		if _, err := s.Head(planID); err == nil {
			return nil
		} else if err != plumbing.ErrReferenceNotFound {
			return err
		}
		if err := copyCommitGraph(source, s, head, map[plumbing.Hash]bool{}); err != nil {
			return fmt.Errorf("copy legacy revision graph: %w", err)
		}
		if err := s.repo.Storer.SetReference(plumbing.NewHashReference(refName(planID), head)); err != nil {
			return fmt.Errorf("write migrated plan ref: %w", err)
		}
		return nil
	})
}

func copyCommitGraph(source, destination *Store, hash plumbing.Hash, copied map[plumbing.Hash]bool) error {
	if copied[hash] {
		return nil
	}
	copied[hash] = true
	commit, err := source.repo.CommitObject(hash)
	if err != nil {
		return err
	}
	for _, parent := range commit.ParentHashes {
		if err := copyCommitGraph(source, destination, parent, copied); err != nil {
			return err
		}
	}
	if err := copyTreeGraph(source, destination, commit.TreeHash, copied); err != nil {
		return err
	}
	return copyEncodedObject(source, destination, hash)
}

func copyTreeGraph(source, destination *Store, hash plumbing.Hash, copied map[plumbing.Hash]bool) error {
	if copied[hash] {
		return nil
	}
	copied[hash] = true
	tree, err := source.repo.TreeObject(hash)
	if err != nil {
		return err
	}
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir {
			if err := copyTreeGraph(source, destination, entry.Hash, copied); err != nil {
				return err
			}
			continue
		}
		if err := copyEncodedObject(source, destination, entry.Hash); err != nil {
			return err
		}
	}
	return copyEncodedObject(source, destination, hash)
}

func copyEncodedObject(source, destination *Store, hash plumbing.Hash) error {
	if err := destination.repo.Storer.HasEncodedObject(hash); err == nil {
		return nil
	}
	encoded, err := source.repo.Storer.EncodedObject(plumbing.AnyObject, hash)
	if err != nil {
		return err
	}
	copy := destination.repo.Storer.NewEncodedObject()
	copy.SetType(encoded.Type())
	copy.SetSize(encoded.Size())
	reader, err := encoded.Reader()
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := copy.Writer()
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, reader); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	written, err := destination.repo.Storer.SetEncodedObject(copy)
	if err != nil {
		return err
	}
	if written != hash {
		return fmt.Errorf("copied object hash mismatch: expected %s, got %s", hash, written)
	}
	return nil
}

// Restore appends the selected historical content as a new commit. It never
// rewrites history and therefore remains safe for durable review metadata.
func (s *Store) Restore(planID string, current, historical plumbing.Hash) (plumbing.Hash, error) {
	content, err := s.Read(historical)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read revision to restore: %w", err)
	}
	return s.Commit(planID, current, content, "restore plan revision")
}

func refName(planID string) plumbing.ReferenceName {
	return plumbing.ReferenceName("refs/planmaxx/plans/" + planID + "/head")
}
func (s *Store) writeBlob(content string) (plumbing.Hash, error) {
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err = io.WriteString(w, content); err == nil {
		err = w.Close()
	}
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}
func (s *Store) writeTree(tree *object.Tree) (plumbing.Hash, error) {
	obj := &plumbing.MemoryObject{}
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}
func (s *Store) writeCommit(commit *object.Commit) (plumbing.Hash, error) {
	obj := &plumbing.MemoryObject{}
	if err := commit.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}
