package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func gitCommand(args ...string) *exec.Cmd {
	command := exec.Command("git", args...)
	command.Env = gitEnvironment()
	return command
}

const (
	bundleExtension  = ".planmaxx"
	revisionsRef     = "refs/heads/revisions"
	proposalRef      = "refs/heads/proposal"
	stateRef         = "refs/heads/state"
	feedbackNotesRef = "feedback"
	stateFileName    = "review.json"
	baselineFileName = "source-baseline.source"
	bundlePlanFile   = "plan.source"
	legacyBundlePlan = "plan.md"
)

var ErrBundleConflict = errors.New("review bundle changed in another PlanMaxx session")

// BundlePath returns the deterministic, privacy-preserving path for one
// logical plan beneath the user-scoped state root.
func BundlePath(stateRoot, canonicalPlanPath string) string {
	sum := sha256.Sum256([]byte(canonicalPlanPath))
	return filepath.Join(stateRoot, "reviews", hex.EncodeToString(sum[:])+bundleExtension)
}

// LocalBundlePath returns the opt-in bundle path beside the canonical plan.
func LocalBundlePath(canonicalPlanPath string) string {
	return canonicalPlanPath + bundleExtension
}

// BundleStore materializes a single-file Git bundle into a disposable bare
// repository for reads. Every save is built in a fresh staging repository and
// atomically replaces the durable bundle, so a failed save cannot poison the
// active reader.
type BundleStore struct {
	mu            sync.Mutex
	path          string
	repoDir       string
	expectedState string
	autosave      Autosave
	hasAutosave   bool
	legacyRepo    string
	legacyRef     string
	legacyPending bool
}

func OpenBundleStore(path string) (*BundleStore, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("PlanMaxx review storage requires Git on PATH")
	}
	store := &BundleStore{path: path}
	repoDir, err := materializeBundle(path)
	if err != nil {
		return nil, err
	}
	store.repoDir = repoDir
	if err := store.loadMaterialized(); err != nil {
		_ = os.RemoveAll(repoDir)
		return nil, err
	}
	return store, nil
}

func (s *BundleStore) Path() string { return s.path }

func (s *BundleStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repoDir == "" {
		return nil
	}
	err := os.RemoveAll(s.repoDir)
	s.repoDir = ""
	return err
}

func (s *BundleStore) Load() (Autosave, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAutosave {
		return Autosave{}, false
	}
	return cloneAutosave(s.autosave), true
}

// Refresh rematerializes the durable bundle when another PlanMaxx process has
// advanced its state ref. It returns true only when newer state was loaded.
func (s *BundleStore) Refresh() (Autosave, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	durableState, err := bundleRefOID(s.path, stateRef)
	if err != nil {
		return Autosave{}, false, err
	}
	if durableState == s.expectedState {
		return cloneAutosave(s.autosave), false, nil
	}
	if durableState == "" && s.expectedState != "" {
		return Autosave{}, false, fmt.Errorf("%w (expected %s, bundle state is missing)", ErrBundleConflict, s.expectedState)
	}
	nextRepo, err := materializeBundle(s.path)
	if err != nil {
		return Autosave{}, false, err
	}
	oldRepo := s.repoDir
	oldState, oldAutosave, oldHasAutosave := s.expectedState, s.autosave, s.hasAutosave
	s.repoDir = nextRepo
	s.expectedState, s.autosave, s.hasAutosave = "", Autosave{}, false
	if err := s.loadMaterialized(); err != nil {
		s.repoDir = oldRepo
		s.expectedState, s.autosave, s.hasAutosave = oldState, oldAutosave, oldHasAutosave
		_ = os.RemoveAll(nextRepo)
		return Autosave{}, false, err
	}
	_ = os.RemoveAll(oldRepo)
	return cloneAutosave(s.autosave), true, nil
}

// WithLegacyImport arranges for the first bundle save to fetch the old
// per-plan revision ref before writing the new bundle. Reachable commit IDs are
// preserved exactly.
func (s *BundleStore) WithLegacyImport(repositoryPath, ref string) *BundleStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacyRepo, s.legacyRef = repositoryPath, ref
	return s
}

// WithLegacyAutosave seeds a not-yet-created bundle from the former JSON
// record. EnableBundle will run lifecycle migration and commit it immediately.
func (s *BundleStore) WithLegacyAutosave(saved Autosave) *BundleStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.expectedState == "" {
		s.autosave, s.hasAutosave, s.legacyPending = cloneAutosave(saved), true, true
	}
	return s
}

func (s *BundleStore) requiresSave() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.legacyPending
}

func (s *BundleStore) ReadRevision(commitID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(commitID) == "" {
		return "", errors.New("revision has no Git commit")
	}
	return readPlanAt(s.repoDir, commitID)
}

// Save persists the complete review workspace. The returned session contains
// commit IDs assigned to any newly accepted revisions and must replace the
// caller's in-memory session only after this method succeeds.
func (s *BundleStore) Save(status string, document Document, source session.Session) (session.Session, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	release, err := acquireAutosaveLock(bundleLockPath(s.path))
	if err != nil {
		return source, s.autosave.Generation, err
	}
	defer release()

	durableState, err := bundleRefOID(s.path, stateRef)
	if err != nil {
		return source, s.autosave.Generation, err
	}
	if durableState != s.expectedState {
		return source, s.autosave.Generation, fmt.Errorf("%w (expected %s, found %s)", ErrBundleConflict, displayOID(s.expectedState), displayOID(durableState))
	}

	staging, err := materializeBundle(s.path)
	if err != nil {
		return source, s.autosave.Generation, err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if s.legacyRepo != "" && s.expectedState == "" {
		if err := fetchRef(staging, s.legacyRepo, s.legacyRef, "refs/legacy/planmaxx/revisions"); err != nil {
			return source, s.autosave.Generation, fmt.Errorf("import legacy revisions: %w", err)
		}
	}

	working := cloneSession(source)
	if s.legacyRepo != "" && s.expectedState == "" {
		if err := attachLegacyCommitIDs(staging, "refs/legacy/planmaxx/revisions", &working); err != nil {
			return source, s.autosave.Generation, err
		}
	}
	if err := syncBundleRevisions(staging, &working); err != nil {
		return source, s.autosave.Generation, err
	}
	if err := syncBundleProposal(staging, working); err != nil {
		return source, s.autosave.Generation, err
	}
	if err := syncFeedbackNotes(staging, working); err != nil {
		return source, s.autosave.Generation, err
	}
	if s.legacyRepo != "" && s.expectedState == "" {
		if err := deleteRef(staging, "refs/legacy/planmaxx/revisions"); err != nil {
			return source, s.autosave.Generation, err
		}
	}

	nextGeneration := s.autosave.Generation + 1
	document.PlanFormat = working.PlanFormat
	if document.SourceHash == "" {
		document.SourceText = working.Plan
		document.SourceHash = sourceHash(document.SourceText)
	}
	compact := compactBundleSession(working)
	persistedDocument := document
	persistedDocument.SourceText = ""
	payload := Autosave{
		Version: autosaveVersion, Format: autosaveFormat, Generation: nextGeneration,
		SavedAt: time.Now().UTC(), Status: status, Document: persistedDocument, Session: compact,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return source, s.autosave.Generation, fmt.Errorf("encode bundle review state: %w", err)
	}
	encoded = append(encoded, '\n')
	stateOID, err := commitFiles(staging, stateRef, map[string][]byte{
		stateFileName:    encoded,
		baselineFileName: []byte(document.SourceText),
	}, "persist PlanMaxx review state: "+status, payload.SavedAt)
	if err != nil {
		return source, s.autosave.Generation, err
	}
	if status == "finalized" && s.autosave.Status != "finalized" {
		if err := createFinalizedTag(staging, stateOID, payload); err != nil {
			return source, s.autosave.Generation, err
		}
	}
	if err := hydrateBundleRevisions(staging, &working); err != nil {
		return source, s.autosave.Generation, err
	}

	bundleErr := writeBundleAtomic(staging, s.path)
	if bundleErr != nil && !autosaveWasCommitted(bundleErr) {
		return source, s.autosave.Generation, bundleErr
	}

	oldRepo := s.repoDir
	s.repoDir = staging
	keepStaging = true
	s.expectedState = stateOID
	cached := payload
	cached.Document = document
	cached.Session = cloneSession(working)
	s.autosave, s.hasAutosave = cached, true
	s.legacyRepo, s.legacyRef = "", ""
	s.legacyPending = false
	_ = os.RemoveAll(oldRepo)
	return working, nextGeneration, bundleErr
}

func attachLegacyCommitIDs(repo, ref string, value *session.Session) error {
	out, err := gitOutput(repo, nil, "rev-list", "--reverse", "--first-parent", ref)
	if err != nil {
		return fmt.Errorf("read imported revision identities: %w", err)
	}
	commits := strings.Fields(string(out))
	for index := range value.Revisions {
		if index >= len(commits) {
			break
		}
		revision := &value.Revisions[index]
		if revision.CommitID != "" || revision.Plan == "" {
			continue
		}
		plan, err := readPlanAt(repo, commits[index])
		if err != nil {
			return err
		}
		if plan == revision.Plan {
			revision.CommitID = commits[index]
		}
	}
	return nil
}

func (s *BundleStore) loadMaterialized() error {
	oid, err := refOID(s.repoDir, stateRef)
	if err != nil {
		return err
	}
	s.expectedState = oid
	if oid == "" {
		return nil
	}
	data, err := gitOutput(s.repoDir, nil, "show", oid+":"+stateFileName)
	if err != nil {
		return fmt.Errorf("read bundled review state: %w", err)
	}
	var saved Autosave
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("decode bundled review state: %w", err)
	}
	baseline, err := gitOutput(s.repoDir, nil, "show", oid+":"+baselineFileName)
	if err != nil {
		return fmt.Errorf("read bundled source baseline: %w", err)
	}
	saved.Document.SourceText = string(baseline)
	if err := saved.migrate(); err != nil {
		return err
	}
	if err := hydrateBundleRevisions(s.repoDir, &saved.Session); err != nil {
		return err
	}
	if saved.Session.PendingProposal != nil && saved.Session.PendingProposal.ProposedPlan == "" {
		plan, err := readPlanAt(s.repoDir, proposalRef)
		if err != nil {
			return fmt.Errorf("hydrate bundled proposal: %w", err)
		}
		saved.Session.PendingProposal.ProposedPlan = plan
	}
	saved.Session.RestoreCounters()
	s.autosave, s.hasAutosave = saved, true
	return nil
}

func compactBundleSession(source session.Session) session.Session {
	compact := compactRevisionBodies(source)
	compact.Plan = ""
	if compact.PendingProposal != nil {
		compact.PendingProposal.ProposedPlan = ""
	}
	return compact
}

func hydrateBundleRevisions(repo string, value *session.Session) error {
	for index := range value.Revisions {
		revision := &value.Revisions[index]
		if revision.CommitID == "" {
			continue
		}
		plan, err := readPlanAt(repo, revision.CommitID)
		if err != nil {
			return fmt.Errorf("hydrate bundled revision %s: %w", revision.ID, err)
		}
		revision.Plan = plan
		if revision.ID == value.CurrentRevisionID {
			value.Plan = plan
		}
	}
	return nil
}

func syncBundleRevisions(repo string, value *session.Session) error {
	byRevision := make(map[string]string, len(value.Revisions))
	for index := range value.Revisions {
		revision := &value.Revisions[index]
		if revision.CommitID != "" && objectExists(repo, revision.CommitID+"^{commit}") {
			byRevision[revision.ID] = revision.CommitID
			continue
		}
		if revision.Plan == "" {
			return fmt.Errorf("revision %s has neither content nor a reachable commit", revision.ID)
		}
		parent := byRevision[revision.ParentID]
		oid, err := commitPlan(repo, revision.Plan, parent, revision.Summary, revision.CreatedAt)
		if err != nil {
			return fmt.Errorf("commit revision %s: %w", revision.ID, err)
		}
		revision.CommitID = oid
		byRevision[revision.ID] = oid
	}
	current := byRevision[value.CurrentRevisionID]
	if current == "" {
		return errors.New("current revision has no Git commit")
	}
	return updateRef(repo, revisionsRef, current)
}

func syncBundleProposal(repo string, value session.Session) error {
	if value.PendingProposal == nil {
		return deleteRef(repo, proposalRef)
	}
	proposal := value.PendingProposal
	parent := ""
	for _, revision := range value.Revisions {
		if revision.ID == proposal.ParentID {
			parent = revision.CommitID
			break
		}
	}
	if parent == "" {
		return errors.New("proposal parent has no Git commit")
	}
	oid, err := commitPlan(repo, proposal.ProposedPlan, parent, proposal.Summary, proposal.CreatedAt)
	if err != nil {
		return fmt.Errorf("commit pending proposal: %w", err)
	}
	return updateRef(repo, proposalRef, oid)
}

func syncFeedbackNotes(repo string, value session.Session) error {
	for _, revision := range value.Revisions {
		if revision.CommitID == "" || len(revision.Feedback) == 0 {
			continue
		}
		encoded, err := json.Marshal(revision.Feedback)
		if err != nil {
			return err
		}
		if _, err := gitOutput(repo, encoded, "notes", "--ref="+feedbackNotesRef, "add", "-f", "-F", "-", revision.CommitID); err != nil {
			return fmt.Errorf("store feedback note for %s: %w", revision.ID, err)
		}
	}
	return nil
}

func commitPlan(repo, plan, parent, message string, when time.Time) (string, error) {
	blob, err := hashObject(repo, []byte(plan))
	if err != nil {
		return "", err
	}
	tree, err := makeTree(repo, map[string]string{bundlePlanFile: blob})
	if err != nil {
		return "", err
	}
	return commitTree(repo, tree, parent, message, when)
}

func commitFiles(repo, ref string, files map[string][]byte, message string, when time.Time) (string, error) {
	entries := make(map[string]string, len(files))
	for name, content := range files {
		oid, err := hashObject(repo, content)
		if err != nil {
			return "", err
		}
		entries[name] = oid
	}
	tree, err := makeTree(repo, entries)
	if err != nil {
		return "", err
	}
	parent, err := refOID(repo, ref)
	if err != nil {
		return "", err
	}
	commit, err := commitTree(repo, tree, parent, message, when)
	if err != nil {
		return "", err
	}
	if err := updateRef(repo, ref, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func hashObject(repo string, content []byte) (string, error) {
	out, err := gitOutput(repo, content, "hash-object", "-w", "--stdin")
	return strings.TrimSpace(string(out)), err
}

func makeTree(repo string, entries map[string]string) (string, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var input bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&input, "100644 blob %s\t%s\n", entries[name], name)
	}
	out, err := gitOutput(repo, input.Bytes(), "mktree")
	return strings.TrimSpace(string(out)), err
}

func commitTree(repo, tree, parent, message string, when time.Time) (string, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	if strings.TrimSpace(message) == "" {
		message = "PlanMaxx review update"
	}
	command := exec.Command("git", append([]string{"--git-dir", repo}, args...)...)
	command.Stdin = strings.NewReader(message + "\n")
	date := when.UTC().Format(time.RFC3339)
	command.Env = append(gitEnvironment(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := command.CombinedOutput()
	if err != nil {
		return "", gitCommandError(args, out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func createFinalizedTag(repo, stateOID string, saved Autosave) error {
	short := stateOID
	if len(short) > 12 {
		short = short[:12]
	}
	name := fmt.Sprintf("finalized/%s-%s", saved.SavedAt.UTC().Format("20060102T150405Z"), short)
	digest, _ := json.Marshal(saved.Session.Digest)
	command := exec.Command("git", "--git-dir", repo, "tag", "-a", name, "-m", string(digest), stateOID)
	command.Env = gitEnvironment()
	if out, err := command.CombinedOutput(); err != nil {
		return gitCommandError([]string{"tag", "-a", name}, out, err)
	}
	return nil
}

func materializeBundle(path string) (string, error) {
	repo, err := os.MkdirTemp("", "planmaxx-review-*.git")
	if err != nil {
		return "", err
	}
	if _, err := gitOutput("", nil, "init", "--bare", repo); err != nil {
		_ = os.RemoveAll(repo)
		return "", fmt.Errorf("initialize temporary review repository: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		if err := fetchRef(repo, path, "+refs/*", "refs/*"); err != nil {
			_ = os.RemoveAll(repo)
			return "", fmt.Errorf("materialize review bundle: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(repo)
		return "", err
	}
	return repo, nil
}

func fetchRef(repo, source, sourceRef, destinationRef string) error {
	refspec := sourceRef + ":" + destinationRef
	_, err := gitOutput(repo, nil, "fetch", "--no-tags", source, refspec)
	return err
}

func writeBundleAtomic(repo, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create review bundle directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".planmaxx-bundle-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Remove(tmpPath)
	if _, err := gitOutput(repo, nil, "bundle", "create", tmpPath, "--all"); err != nil {
		return fmt.Errorf("create review bundle: %w", err)
	}
	if _, err := gitOutput(repo, nil, "bundle", "verify", tmpPath); err != nil {
		return fmt.Errorf("verify review bundle: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	file, err := os.OpenFile(tmpPath, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return autosaveCommittedError{fmt.Errorf("open bundle directory after replace: %w", err)}
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return autosaveCommittedError{fmt.Errorf("sync bundle directory after replace: %w", err)}
	}
	return nil
}

func bundleRefOID(path, ref string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	command := exec.Command("git", "bundle", "list-heads", path, ref)
	command.Env = gitEnvironment()
	out, err := command.CombinedOutput()
	if err != nil {
		return "", gitCommandError([]string{"bundle", "list-heads", path, ref}, out, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) < 2 || fields[1] != ref {
		return "", fmt.Errorf("review bundle returned malformed ref %q", strings.TrimSpace(string(out)))
	}
	return fields[0], nil
}

func refOID(repo, ref string) (string, error) {
	out, err := gitOutput(repo, nil, "rev-parse", "--verify", ref)
	if err != nil {
		if isMissingRevision(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func updateRef(repo, ref, oid string) error {
	current, err := refOID(repo, ref)
	if err != nil {
		return err
	}
	args := []string{"update-ref", "--create-reflog", ref, oid}
	if current != "" {
		args = append(args, current)
	}
	_, err = gitOutput(repo, nil, args...)
	return err
}

func deleteRef(repo, ref string) error {
	current, err := refOID(repo, ref)
	if err != nil || current == "" {
		return err
	}
	_, err = gitOutput(repo, nil, "update-ref", "-d", ref, current)
	return err
}

func readPlanAt(repo, revision string) (string, error) {
	data, err := gitOutput(repo, nil, "show", revision+":"+bundlePlanFile)
	if err == nil {
		return string(data), nil
	}
	legacy, legacyErr := gitOutput(repo, nil, "show", revision+":"+legacyBundlePlan)
	if legacyErr != nil {
		return "", err
	}
	return string(legacy), nil
}

func objectExists(repo, revision string) bool {
	command := exec.Command("git", "--git-dir", repo, "cat-file", "-e", revision)
	command.Env = gitEnvironment()
	return command.Run() == nil
}

func gitOutput(repo string, input []byte, args ...string) ([]byte, error) {
	full := args
	if repo != "" {
		full = append([]string{"--git-dir", repo}, args...)
	}
	command := exec.Command("git", full...)
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	command.Env = gitEnvironment()
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, gitCommandError(args, out, err)
	}
	return out, nil
}

func gitEnvironment() []string {
	return append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=PlanMaxx", "GIT_AUTHOR_EMAIL=planmaxx@local",
		"GIT_COMMITTER_NAME=PlanMaxx", "GIT_COMMITTER_EMAIL=planmaxx@local",
	)
}

type gitExecError struct {
	args   []string
	output string
	err    error
}

func (e *gitExecError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, strings.TrimSpace(e.output))
}

func (e *gitExecError) Unwrap() error { return e.err }

func gitCommandError(args []string, output []byte, err error) error {
	return &gitExecError{args: append([]string(nil), args...), output: string(output), err: err}
}

func isMissingRevision(err error) bool {
	var execErr *gitExecError
	return errors.As(err, &execErr) && strings.Contains(execErr.output, "Needed a single revision")
}

func bundleLockPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(os.TempDir(), "planmaxx-locks", hex.EncodeToString(sum[:16]))
}

// ProbeBundleLock reports whether another process currently holds the bundle's
// write lock. The returned marker path is runtime state; its mere existence is
// not evidence that a review process is active.
func ProbeBundleLock(path string) (held bool, markerPath string, err error) {
	base := bundleLockPath(path)
	held, err = probeAutosaveLock(base)
	return held, base + ".lock", err
}

func displayOID(oid string) string {
	if oid == "" {
		return "missing"
	}
	return oid
}

func cloneAutosave(source Autosave) Autosave {
	encoded, err := json.Marshal(source)
	if err != nil {
		return source
	}
	var clone Autosave
	if json.Unmarshal(encoded, &clone) != nil {
		return source
	}
	clone.Session.RestoreCounters()
	return clone
}
