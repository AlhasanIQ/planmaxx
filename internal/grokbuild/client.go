package grokbuild

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

const forkCleanupTimeout = 10 * time.Second
const isolatedSandboxProfile = "planmaxx-isolated"

const (
	maxWorkspaceSnapshotFiles = 100_000
	maxWorkspaceSnapshotBytes = int64(512 << 20)
	maxSessionSnapshotFiles   = 10_000
	maxSessionSnapshotBytes   = int64(256 << 20)
)

// ErrClientUnusable marks failures that leave PlanMaxx unable to prove that a
// disposable Grok Build fork was removed. Callers should disable the attachment
// until the review is restarted and the session state is checked.
var ErrClientUnusable = errors.New("Grok Build client is unusable")

// CommandFactory creates a Grok Build process. The default factory uses
// exec.CommandContext so cancellation terminates the process and Run waits for
// it to be reaped.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// ForkIDGenerator returns a fresh UUID for a disposable session fork.
type ForkIDGenerator func() (string, error)

// IsolationFactory stages the source conversation in a temporary Grok home.
type IsolationFactory func(context.Context, string) (*Isolation, error)

// Isolation describes the environment used for one disposable fork.
type Isolation struct {
	HomeDir                string
	GrokHome               string
	WorkspaceRoot          string
	WorkingDirectory       string
	SourceRoot             string
	SourceWorkingDirectory string
	AuthPath               string
	Validate               func(context.Context) error
	Relocate               func(context.Context, string, string) (bool, error)
	Cleanup                func() error
}

// Option customizes a Client.
type Option func(*Client)

// WithCommandFactory replaces process creation. It is primarily useful for
// tests that need to run a controlled helper process.
func WithCommandFactory(factory CommandFactory) Option {
	return func(client *Client) {
		if factory != nil {
			client.command = factory
		}
	}
}

// WithForkIDGenerator replaces disposable fork ID generation.
func WithForkIDGenerator(generator ForkIDGenerator) Option {
	return func(client *Client) {
		if generator != nil {
			client.forkID = generator
		}
	}
}

// WithIsolationFactory replaces isolated-session staging. It is primarily
// useful for tests with a controlled fake Grok executable.
func WithIsolationFactory(factory IsolationFactory) Option {
	return func(client *Client) {
		if factory != nil {
			client.isolation = factory
		}
	}
}

// Client asks questions in disposable forks of an existing Grok Build session.
//
// Grok Build operations are serialized because concurrent resumes of the same
// source session are not guaranteed to be safe. The gate is context-aware so a
// canceled caller does not remain queued behind another operation.
type Client struct {
	sessionID string
	command   CommandFactory
	forkID    ForkIDGenerator
	isolation IsolationFactory
	gate      chan struct{}
}

// NewClient creates a Grok Build client for sessionID. The source session's
// recorded working directory is authoritative so the disposable child sees the
// same project context. workingDirectory remains accepted for API compatibility.
func NewClient(sessionID, workingDirectory string, options ...Option) *Client {
	_ = workingDirectory
	client := &Client{
		sessionID: sessionID,
		command:   exec.CommandContext,
		forkID:    newUUID,
		isolation: stageIsolatedSession,
		gate:      make(chan struct{}, 1),
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
}

var (
	_ sidequestions.AskClient  = (*Client)(nil)
	_ sectioniter.PromptClient = (*Client)(nil)
)

// Ask implements sidequestions.AskClient.
func (c *Client) Ask(ctx context.Context, req sidequestions.Request) (string, error) {
	prompt := prompts.SideQuestion(
		req.Question,
		req.FilePath,
		req.Reference,
		req.SelectedText,
		req.PlanExcerpt,
		req.Format,
	)
	return c.askPromptInFork(ctx, c.sessionID, prompt)
}

// AskPrompt implements sectioniter.PromptClient.
func (c *Client) AskPrompt(ctx context.Context, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("Grok Build client is unavailable")
	}
	return c.askPromptInFork(ctx, c.sessionID, prompt)
}

func (c *Client) askPromptInFork(ctx context.Context, sessionID, prompt string) (_ string, returnErr error) {
	if c == nil {
		return "", fmt.Errorf("Grok Build client is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("Grok Build session id is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("Grok Build prompt is required")
	}
	if c.command == nil || c.forkID == nil || c.isolation == nil || c.gate == nil {
		return "", fmt.Errorf("Grok Build client dependencies are unavailable")
	}

	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	childSessionID, err := c.forkID()
	if err != nil {
		return "", fmt.Errorf("create disposable Grok Build fork id: %w", err)
	}
	isolation, err := c.isolation(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("isolate Grok Build source session: %w", err)
	}
	if isolation == nil || isolation.Cleanup == nil ||
		strings.TrimSpace(isolation.HomeDir) == "" ||
		strings.TrimSpace(isolation.GrokHome) == "" ||
		strings.TrimSpace(isolation.WorkspaceRoot) == "" ||
		strings.TrimSpace(isolation.WorkingDirectory) == "" ||
		isolation.Relocate == nil {
		if isolation != nil && isolation.Cleanup != nil {
			_ = isolation.Cleanup()
		}
		return "", fmt.Errorf("Grok Build isolation factory returned an incomplete environment")
	}
	forkCreated := false
	defer func() {
		var cleanupErr error
		if forkCreated {
			cleanupErr = c.removeFork(childSessionID, isolation)
		}
		if err := isolation.Cleanup(); err != nil {
			cleanupErr = combineCleanupError(
				cleanupErr,
				fmt.Errorf("remove isolated Grok Build home %s: %w", isolation.GrokHome, err),
			)
		}
		if cleanupErr != nil {
			returnErr = combineCleanupError(returnErr, cleanupErr)
		}
	}()
	if isolation.Validate != nil {
		if err := isolation.Validate(ctx); err != nil {
			return "", fmt.Errorf("validate isolated Grok Build configuration: %w", err)
		}
	}
	forkCreated, err = isolation.Relocate(ctx, sessionID, childSessionID)
	if err != nil {
		return "", fmt.Errorf("relocate disposable Grok Build fork: %w", err)
	}
	if !forkCreated {
		return "", errors.New("relocate disposable Grok Build fork did not create a child session")
	}

	promptFile, err := os.CreateTemp(isolation.WorkingDirectory, "planmaxx-grok-prompt-*.txt")
	if err != nil {
		return "", fmt.Errorf("create Grok Build prompt file: %w", err)
	}
	promptPath := promptFile.Name()
	defer os.Remove(promptPath)
	if err := promptFile.Chmod(0o600); err != nil {
		_ = promptFile.Close()
		return "", fmt.Errorf("secure Grok Build prompt file: %w", err)
	}
	if _, err := promptFile.WriteString(isolatedPrompt(isolation, prompt)); err != nil {
		_ = promptFile.Close()
		return "", fmt.Errorf("write Grok Build prompt file: %w", err)
	}
	if err := promptFile.Close(); err != nil {
		return "", fmt.Errorf("close Grok Build prompt file: %w", err)
	}

	args := []string{
		"--cwd", isolation.WorkingDirectory,
		"--prompt-file", promptPath,
		"--resume", childSessionID,
		"--output-format", "json",
		"--tools", "read_file,grep,list_dir",
		"--allow", "Read(" + filepath.ToSlash(filepath.Join(isolation.WorkspaceRoot, "**")) + ")",
		"--allow", "Grep(" + filepath.ToSlash(filepath.Join(isolation.WorkspaceRoot, "**")) + ")",
		"--deny", "MCPTool",
		"--permission-mode", "dontAsk",
		"--sandbox", isolatedSandboxProfile,
		"--no-subagents",
		"--no-memory",
		"--disable-web-search",
		"--no-plan",
		"--max-turns", "2",
		"--verbatim",
	}
	command := c.command(ctx, "grok", args...)
	if command == nil {
		return "", fmt.Errorf("Grok Build command factory returned no command")
	}
	command.Dir = isolation.WorkingDirectory
	command.Env = isolatedEnvironment(command.Environ(), isolation)

	stdout, stderr, runErr := runBoundedOutput(
		command,
		maxGrokResponseBytes,
		maxGrokErrorBytes,
	)
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			runErr = ctxErr
		}
		return "", withStderr(fmt.Errorf("run Grok Build: %w", runErr), string(stderr))
	}

	var response struct {
		Type       string `json:"type"`
		Message    string `json:"message"`
		Text       string `json:"text"`
		StopReason string `json:"stopReason"`
		SessionID  string `json:"sessionId"`
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		return "", withStderr(fmt.Errorf("decode Grok Build response: %w", err), string(stderr))
	}
	if response.Type == "error" {
		message := strings.TrimSpace(response.Message)
		if message == "" {
			message = "unknown error"
		}
		return "", withStderr(fmt.Errorf("Grok Build operation failed: %s", message), string(stderr))
	}
	if strings.TrimSpace(response.Text) == "" {
		return "", withStderr(errors.New("Grok Build returned an empty result"), string(stderr))
	}
	if response.StopReason != "EndTurn" {
		reason := strings.TrimSpace(response.StopReason)
		if reason == "" {
			reason = "missing"
		}
		return "", withStderr(fmt.Errorf("Grok Build stopped without completing the request (reason %q)", reason), string(stderr))
	}
	if strings.TrimSpace(response.SessionID) == "" {
		return "", withStderr(errors.New("Grok Build did not report the disposable fork session id"), string(stderr))
	}
	if response.SessionID != childSessionID {
		return "", withStderr(fmt.Errorf("Grok Build reported unexpected fork session id %q", response.SessionID), string(stderr))
	}
	return response.Text, nil
}

func (c *Client) removeFork(sessionID string, isolation *Isolation) error {
	ctx, cancel := context.WithTimeout(context.Background(), forkCleanupTimeout)
	defer cancel()

	command := c.command(ctx, "grok", "sessions", "delete", sessionID)
	if command == nil {
		return fmt.Errorf("%w: Grok Build command factory returned no cleanup command", ErrClientUnusable)
	}
	command.Dir = isolation.WorkingDirectory
	command.Env = isolatedEnvironment(command.Environ(), isolation)
	output, stderr, err := runBoundedOutput(command, maxGrokErrorBytes, maxGrokErrorBytes)
	if err != nil {
		return withStderr(
			fmt.Errorf("%w: remove disposable Grok Build fork %s: %v", ErrClientUnusable, sessionID, err),
			string(append(output, stderr...)),
		)
	}
	if err := verifySessionAbsent(filepath.Join(isolation.GrokHome, "sessions"), sessionID); err != nil {
		return fmt.Errorf("%w: disposable Grok Build fork %s remains after deletion: %v", ErrClientUnusable, sessionID, err)
	}
	return nil
}

func stageIsolatedSession(ctx context.Context, sessionID string) (_ *Isolation, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sourceHome, err := sourceGrokHome()
	if err != nil {
		return nil, err
	}
	sourceHome, err = filepath.EvalSymlinks(sourceHome)
	if err != nil {
		return nil, fmt.Errorf("resolve Grok Build home: %w", err)
	}
	sourceSession, err := findSourceSession(filepath.Join(sourceHome, "sessions"), sessionID)
	if err != nil {
		return nil, err
	}

	root, err := os.MkdirTemp("", "planmaxx-grok-isolation-*")
	if err != nil {
		return nil, err
	}
	createdRoot := root
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		_ = os.RemoveAll(createdRoot)
		return nil, fmt.Errorf("resolve Grok Build isolation root: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(root)
		}
	}()
	home := filepath.Join(root, "home")
	grokHome := filepath.Join(home, ".grok")
	workspace := filepath.Join(root, "workspace")
	temporary := filepath.Join(root, "tmp")
	for _, path := range []string{home, grokHome, workspace, temporary} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}

	sourceWorkingDirectory, err := persistedSessionWorkingDirectory(sourceSession)
	if err != nil {
		return nil, err
	}
	sourceWorkspace, err := canonicalWorkingDirectory(sourceWorkingDirectory)
	if err != nil {
		return nil, err
	}
	isolatedWorkingDirectory, sourceRoot, err := snapshotWorkspace(ctx, sourceWorkspace, workspace)
	if err != nil {
		return nil, fmt.Errorf("snapshot Grok Build workspace: %w", err)
	}
	sourceNamespace := filepath.Base(filepath.Dir(sourceSession))
	stagedSourceSession := filepath.Join(grokHome, "sessions", sourceNamespace, sessionID)
	if err := copySessionDirectory(ctx, sourceSession, stagedSourceSession); err != nil {
		return nil, fmt.Errorf("copy source session: %w", err)
	}
	if err := writeSandboxProfile(grokHome, sourceRoot); err != nil {
		return nil, err
	}

	authPath := ""
	sourceHomeDirectory, openErr := openRootDirectoryNoFollow(sourceHome)
	if openErr != nil {
		return nil, fmt.Errorf("securely open Grok Build home: %w", openErr)
	}
	defer sourceHomeDirectory.Close()
	if auth, readErr := readRegularFileBoundedAt(sourceHomeDirectory, "auth.json", 1<<20); readErr == nil {
		authPath = filepath.Join(grokHome, "auth.json")
		if err := os.WriteFile(authPath, auth, 0o600); err != nil {
			return nil, fmt.Errorf("copy Grok Build authentication state: %w", err)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("read Grok Build authentication state: %w", readErr)
	}

	isolation := &Isolation{
		HomeDir:                home,
		GrokHome:               grokHome,
		WorkspaceRoot:          workspace,
		WorkingDirectory:       isolatedWorkingDirectory,
		SourceRoot:             sourceRoot,
		SourceWorkingDirectory: sourceWorkingDirectory,
		AuthPath:               authPath,
		Cleanup:                func() error { return os.RemoveAll(root) },
	}
	isolation.Validate = func(ctx context.Context) error {
		return validateIsolatedConfiguration(ctx, isolation)
	}
	isolation.Relocate = func(ctx context.Context, sourceID, childID string) (bool, error) {
		created, err := relocateSessionWithACP(ctx, isolation, sourceID, childID)
		if err != nil {
			return created, err
		}
		childSession, err := findSourceSession(
			filepath.Join(isolation.GrokHome, "sessions"),
			childID,
		)
		if err != nil {
			return true, fmt.Errorf("locate relocated Grok Build child session: %w", err)
		}
		if err := rewriteChildSessionPaths(
			ctx,
			childSession,
			isolation.SourceRoot,
			isolation.WorkspaceRoot,
		); err != nil {
			return true, fmt.Errorf("rewrite relocated Grok Build workspace paths: %w", err)
		}
		return true, rewriteSessionMetadata(
			filepath.Join(childSession, "summary.json"),
			isolation.WorkingDirectory,
			isolation.WorkspaceRoot,
			isolation.GrokHome,
		)
	}
	return isolation, nil
}

func persistedSessionWorkingDirectory(sessionDirectory string) (string, error) {
	root, err := openRootDirectoryNoFollow(sessionDirectory)
	if err != nil {
		return "", fmt.Errorf("securely open Grok Build source session: %w", err)
	}
	defer root.Close()
	content, err := readRegularFileBoundedAt(root, "summary.json", 1<<20)
	if err != nil {
		return "", fmt.Errorf("read staged Grok Build summary: %w", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(content, &summary); err != nil {
		return "", fmt.Errorf("decode staged Grok Build summary: %w", err)
	}
	info, ok := summary["info"].(map[string]any)
	if !ok {
		return "", errors.New("staged Grok Build summary is missing session info")
	}
	cwd, ok := info["cwd"].(string)
	if !ok || strings.TrimSpace(cwd) == "" {
		return "", errors.New("staged Grok Build summary is missing its working directory")
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("staged Grok Build working directory is not absolute: %s", cwd)
	}
	return filepath.Clean(cwd), nil
}

func canonicalWorkingDirectory(workingDirectory string) (string, error) {
	absolute := filepath.Clean(workingDirectory)
	var err error
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve source workspace %s: %w", absolute, err)
	}
	stat, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("open source workspace %s: %w", absolute, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("source workspace is not a directory: %s", absolute)
	}
	return absolute, nil
}

func snapshotWorkspace(ctx context.Context, source, destination string) (string, string, error) {
	root, files, err := workspaceFileList(ctx, source)
	if err != nil {
		return "", "", err
	}
	rootDirectory, err := openRootDirectoryNoFollow(root)
	if err != nil {
		return "", "", fmt.Errorf("securely open workspace root: %w", err)
	}
	defer rootDirectory.Close()
	sourceRelative, err := filepath.Rel(root, source)
	if err != nil {
		return "", "", err
	}
	if sourceRelative == ".." || strings.HasPrefix(sourceRelative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("source working directory %s is outside workspace root %s", source, root)
	}
	var totalBytes int64
	for index, relative := range files {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		if index >= maxWorkspaceSnapshotFiles {
			return "", "", fmt.Errorf("workspace contains more than %d files", maxWorkspaceSnapshotFiles)
		}
		if excludedWorkspacePath(relative) {
			continue
		}
		clean := filepath.Clean(relative)
		if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", "", fmt.Errorf("unsafe workspace path %q", relative)
		}
		sourcePath := filepath.Join(root, clean)
		info, statErr := os.Lstat(sourcePath)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return "", "", statErr
		}
		if info.IsDir() {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("workspace symlinks are not copied into Grok Build isolation: %s", sourcePath)
		}
		if !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("workspace contains unsupported file: %s", sourcePath)
		}
		targetPath := filepath.Join(destination, clean)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return "", "", err
		}
		copied, err := copyRegularFile(
			rootDirectory,
			clean,
			info,
			targetPath,
			maxWorkspaceSnapshotBytes-totalBytes,
		)
		if err != nil {
			return "", "", err
		}
		totalBytes += copied
	}
	isolatedWorkingDirectory := filepath.Join(destination, sourceRelative)
	if err := os.MkdirAll(isolatedWorkingDirectory, 0o700); err != nil {
		return "", "", err
	}
	return isolatedWorkingDirectory, root, nil
}

func workspaceFileList(ctx context.Context, source string) (string, []string, error) {
	rootCommand := exec.CommandContext(ctx, "git", "-C", source, "rev-parse", "--show-toplevel")
	rootOutput, rootStderr, rootErr := runBoundedOutput(rootCommand, 1<<20, maxGrokErrorBytes)
	if rootErr == nil {
		root := strings.TrimSpace(string(rootOutput))
		listCommand := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		output, stderr, err := runBoundedOutput(listCommand, maxGitOutputBytes, maxGrokErrorBytes)
		if err != nil {
			return "", nil, fmt.Errorf("list Git workspace files: %w: %s", err, strings.TrimSpace(string(stderr)))
		}
		raw := strings.Split(string(output), "\x00")
		files := make([]string, 0, len(raw))
		for _, path := range raw {
			if path != "" {
				files = append(files, filepath.FromSlash(path))
			}
		}
		return root, files, nil
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	_ = rootStderr

	var files []string
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() && excludedWorkspacePath(relative) {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, relative)
		}
		return nil
	})
	return source, files, err
}

func excludedWorkspacePath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case ".mcp.json", ".cursorrules", ".envrc", "agents.md", "claude.md", "claude.local.md", "grok.md":
		return true
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		switch strings.ToLower(part) {
		case ".git", ".grok", ".claude", ".agents", ".cursor", ".direnv":
			return true
		}
	}
	return false
}

func copyRegularFile(
	root *os.File,
	relative string,
	before os.FileInfo,
	destination string,
	remaining int64,
) (int64, error) {
	if remaining < 0 {
		return 0, fmt.Errorf("workspace snapshot exceeds %d bytes", maxWorkspaceSnapshotBytes)
	}
	if !before.Mode().IsRegular() {
		return 0, fmt.Errorf("workspace file changed type while staging: %s", relative)
	}
	sourceFile, err := openRegularFileBeneath(root, relative)
	if err != nil {
		return 0, err
	}
	opened, err := sourceFile.Stat()
	if err != nil {
		_ = sourceFile.Close()
		return 0, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = sourceFile.Close()
		return 0, fmt.Errorf("workspace file changed while staging: %s", relative)
	}
	targetFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = sourceFile.Close()
		return 0, err
	}
	copied, copyErr := io.Copy(targetFile, io.LimitReader(sourceFile, remaining+1))
	finished, statErr := sourceFile.Stat()
	sourceCloseErr := sourceFile.Close()
	targetCloseErr := targetFile.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return 0, copyErr
	}
	if copied > remaining {
		_ = os.Remove(destination)
		return 0, fmt.Errorf("workspace snapshot exceeds %d bytes", maxWorkspaceSnapshotBytes)
	}
	if statErr != nil ||
		!os.SameFile(before, finished) ||
		finished.Size() != before.Size() ||
		!finished.ModTime().Equal(before.ModTime()) {
		_ = os.Remove(destination)
		return 0, fmt.Errorf("workspace file changed while staging: %s", relative)
	}
	if sourceCloseErr != nil {
		return 0, sourceCloseErr
	}
	if targetCloseErr != nil {
		return 0, targetCloseErr
	}
	return copied, nil
}

func sourceGrokHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("GROK_HOME")); configured != "" {
		if configured == "~" || strings.HasPrefix(configured, "~/") || strings.HasPrefix(configured, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			configured = filepath.Join(home, strings.TrimLeft(configured[1:], `/\`))
		}
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("user home directory is empty")
	}
	return filepath.Join(home, ".grok"), nil
}

func findSourceSession(sessionsRoot, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" || strings.ContainsAny(sessionID, `/\`) {
		return "", errors.New("invalid Grok Build source session id")
	}
	roots, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return "", fmt.Errorf("read Grok Build sessions: %w", err)
	}
	var match string
	for _, root := range roots {
		if !root.IsDir() || root.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := filepath.Join(sessionsRoot, root.Name(), sessionID)
		info, statErr := os.Lstat(candidate)
		if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if _, summaryErr := os.Stat(filepath.Join(candidate, "summary.json")); summaryErr == nil {
				if match != "" {
					return "", fmt.Errorf(
						"Grok Build source session %s exists in more than one workspace namespace",
						sessionID,
					)
				}
				match = candidate
			}
		}
	}
	if match != "" {
		return match, nil
	}
	return "", fmt.Errorf("Grok Build source session %s was not found in %s", sessionID, sessionsRoot)
}

func verifySessionAbsent(sessionsRoot, sessionID string) error {
	roots, err := os.ReadDir(sessionsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, root := range roots {
		if !root.IsDir() || root.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := filepath.Join(sessionsRoot, root.Name(), sessionID)
		if info, statErr := os.Lstat(candidate); statErr == nil {
			return fmt.Errorf("session storage still exists at %s (%s)", candidate, info.Mode())
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	return nil
}

type sessionFileSignature struct {
	path    string
	size    int64
	modTime int64
	mode    os.FileMode
}

func copySessionDirectory(ctx context.Context, source, destination string) error {
	const attempts = 3
	for attempt := 1; attempt <= attempts; attempt++ {
		before, err := sessionDirectorySignature(ctx, source)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		if err := copySessionDirectoryOnce(ctx, source, destination); err != nil {
			return err
		}
		after, err := sessionDirectorySignature(ctx, source)
		if err != nil {
			return err
		}
		if sessionSignaturesEqual(before, after) {
			return nil
		}
	}
	return errors.New("Grok Build source session changed repeatedly while staging")
}

func copySessionDirectoryOnce(ctx context.Context, source, destination string) error {
	sourceDirectory, err := openRootDirectoryNoFollow(source)
	if err != nil {
		return fmt.Errorf("securely open Grok Build source session: %w", err)
	}
	defer sourceDirectory.Close()
	files := 0
	remaining := maxSessionSnapshotBytes
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in Grok Build session: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("refusing non-regular file in Grok Build session: %s", path)
		}
		if strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		files++
		if files > maxSessionSnapshotFiles {
			return fmt.Errorf("Grok Build session contains more than %d files", maxSessionSnapshotFiles)
		}
		content, err := readRegularFileBoundedAt(sourceDirectory, relative, remaining)
		if err != nil {
			return err
		}
		remaining -= int64(len(content))
		return os.WriteFile(target, content, 0o600)
	})
}

func sessionDirectorySignature(ctx context.Context, source string) ([]sessionFileSignature, error) {
	signatures := make([]sessionFileSignature, 0, 16)
	var totalBytes int64
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in Grok Build session: %s", path)
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular file in Grok Build session: %s", path)
		}
		totalBytes += info.Size()
		if totalBytes > maxSessionSnapshotBytes {
			return fmt.Errorf("Grok Build session exceeds %d bytes", maxSessionSnapshotBytes)
		}
		if len(signatures) >= maxSessionSnapshotFiles {
			return fmt.Errorf("Grok Build session contains more than %d files", maxSessionSnapshotFiles)
		}
		signatures = append(signatures, sessionFileSignature{
			path:    relative,
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
			mode:    info.Mode(),
		})
		return nil
	})
	return signatures, err
}

func sessionSignaturesEqual(left, right []sessionFileSignature) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func readRegularFileBounded(path string, remaining int64) ([]byte, error) {
	if remaining < 0 {
		return nil, fmt.Errorf("snapshot exceeds its byte limit")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file: %s", path)
	}
	file, err := openRegularFileNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("file changed while staging: %s", path)
	}
	content, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return nil, err
	}
	finished, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, finished) ||
		finished.Size() != before.Size() ||
		!finished.ModTime().Equal(before.ModTime()) {
		return nil, fmt.Errorf("file changed while staging: %s", path)
	}
	if int64(len(content)) > remaining {
		return nil, fmt.Errorf("snapshot exceeds its byte limit")
	}
	return content, nil
}

func readRegularFileBoundedAt(root *os.File, relative string, remaining int64) ([]byte, error) {
	if remaining < 0 {
		return nil, fmt.Errorf("snapshot exceeds its byte limit")
	}
	path := filepath.Join(root.Name(), relative)
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file: %s", path)
	}
	file, err := openRegularFileBeneath(root, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("file changed while staging: %s", path)
	}
	content, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return nil, err
	}
	finished, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, finished) ||
		finished.Size() != before.Size() ||
		!finished.ModTime().Equal(before.ModTime()) {
		return nil, fmt.Errorf("file changed while staging: %s", path)
	}
	if int64(len(content)) > remaining {
		return nil, fmt.Errorf("snapshot exceeds its byte limit")
	}
	return content, nil
}

func rewriteSessionMetadata(summaryPath, workingDirectory, workspaceRoot, grokHome string) error {
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		return fmt.Errorf("read staged Grok Build summary: %w", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(content, &summary); err != nil {
		return fmt.Errorf("decode staged Grok Build summary: %w", err)
	}
	info, ok := summary["info"].(map[string]any)
	if !ok {
		return errors.New("staged Grok Build summary is missing session info")
	}
	info["cwd"] = workingDirectory
	summary["git_root_dir"] = workspaceRoot + string(filepath.Separator)
	summary["grok_home"] = grokHome
	summary["sandbox_profile"] = isolatedSandboxProfile
	updated, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	if err := os.WriteFile(summaryPath, append(updated, '\n'), 0o600); err != nil {
		return fmt.Errorf("rewrite staged Grok Build summary: %w", err)
	}
	return nil
}

func rewriteChildSessionPaths(ctx context.Context, sessionDirectory, sourceRoot, workspaceRoot string) error {
	sourceRoot = filepath.Clean(sourceRoot)
	workspaceRoot = filepath.Clean(workspaceRoot)
	if sourceRoot == workspaceRoot {
		return nil
	}
	if sourceRoot == filepath.VolumeName(sourceRoot)+string(filepath.Separator) {
		return errors.New("refusing to rewrite a filesystem-root workspace")
	}
	remaining := maxSessionSnapshotBytes
	return filepath.WalkDir(sessionDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in relocated Grok Build session: %s", path)
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("refusing non-regular file in relocated Grok Build session: %s", path)
		}
		content, err := readRegularFileBounded(path, remaining)
		if err != nil {
			return err
		}
		remaining -= int64(len(content))
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json":
			updated, err := rewriteJSONPaths(content, sourceRoot, workspaceRoot)
			if err != nil {
				return fmt.Errorf("rewrite %s: %w", path, err)
			}
			return os.WriteFile(path, append(updated, '\n'), 0o600)
		case ".jsonl":
			var updated bytes.Buffer
			for lineNumber, line := range bytes.Split(content, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				rewritten, err := rewriteJSONPaths(line, sourceRoot, workspaceRoot)
				if err != nil {
					return fmt.Errorf("rewrite %s line %d: %w", path, lineNumber+1, err)
				}
				updated.Write(rewritten)
				updated.WriteByte('\n')
			}
			return os.WriteFile(path, updated.Bytes(), 0o600)
		default:
			if bytes.Contains(content, []byte(sourceRoot)) {
				return fmt.Errorf("unsupported session file contains the original workspace path: %s", path)
			}
			return nil
		}
	})
}

func rewriteJSONPaths(content []byte, sourceRoot, workspaceRoot string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("JSON contains more than one value")
		}
		return nil, err
	}
	return json.Marshal(rewriteJSONValue(value, sourceRoot, workspaceRoot))
}

func rewriteJSONValue(value any, sourceRoot, workspaceRoot string) any {
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, sourceRoot, workspaceRoot)
	case []any:
		for index := range typed {
			typed[index] = rewriteJSONValue(typed[index], sourceRoot, workspaceRoot)
		}
		return typed
	case map[string]any:
		rewritten := make(map[string]any, len(typed))
		for key, item := range typed {
			rewrittenKey := strings.ReplaceAll(key, sourceRoot, workspaceRoot)
			rewritten[rewrittenKey] = rewriteJSONValue(item, sourceRoot, workspaceRoot)
		}
		return rewritten
	default:
		return value
	}
}

func writeSandboxProfile(grokHome, sourceRoot string) error {
	content := fmt.Sprintf(
		"[profiles.%s]\nextends = \"strict\"\nrestrict_network = true\ndeny = [%q]\n",
		isolatedSandboxProfile,
		sourceRoot,
	)
	if err := os.WriteFile(filepath.Join(grokHome, "sandbox.toml"), []byte(content), 0o600); err != nil {
		return fmt.Errorf("write isolated Grok Build sandbox profile: %w", err)
	}
	return nil
}

func validateIsolatedConfiguration(ctx context.Context, isolation *Isolation) error {
	command := exec.CommandContext(
		ctx,
		"grok",
		"--cwd", isolation.WorkingDirectory,
		"--sandbox", isolatedSandboxProfile,
		"inspect",
		"--json",
	)
	command.Dir = isolation.WorkingDirectory
	command.Env = isolatedEnvironment(os.Environ(), isolation)
	output, stderr, err := runBoundedOutput(command, maxGrokResponseBytes, maxGrokErrorBytes)
	if err != nil {
		return fmt.Errorf(
			"run grok inspect in isolated sandbox: %w: %s",
			err,
			strings.TrimSpace(string(append(output, stderr...))),
		)
	}
	return validateIsolatedInspection(output)
}

func validateIsolatedInspection(output []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(output, &fields); err != nil {
		return fmt.Errorf("decode isolated grok inspect response: %w", err)
	}
	if fields == nil {
		return errors.New("isolated grok inspect response is null")
	}
	requiredArrays := []string{
		"projectInstructions", "hooks", "skills", "plugins", "marketplaces",
		"mcpServers", "lspServers", "agents",
	}
	arrays := make(map[string][]json.RawMessage, len(requiredArrays))
	for _, name := range requiredArrays {
		raw, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("isolated grok inspect response is missing required field %q", name)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("isolated grok inspect field %q has an unexpected shape: %w", name, err)
		}
		arrays[name] = values
	}
	for _, name := range requiredArrays[:len(requiredArrays)-1] {
		if len(arrays[name]) != 0 {
			return errors.New("isolated Grok Build environment loaded external instructions or executable configuration")
		}
	}

	permissionsRaw, ok := fields["permissions"]
	if !ok || bytes.Equal(bytes.TrimSpace(permissionsRaw), []byte("null")) {
		return errors.New(`isolated grok inspect response is missing required field "permissions"`)
	}
	var permissions map[string]json.RawMessage
	if err := json.Unmarshal(permissionsRaw, &permissions); err != nil {
		return fmt.Errorf("isolated grok inspect permissions have an unexpected shape: %w", err)
	}
	managedRaw, ok := permissions["managedSettingsActive"]
	if !ok {
		return errors.New(`isolated grok inspect permissions are missing "managedSettingsActive"`)
	}
	var managed bool
	if err := json.Unmarshal(managedRaw, &managed); err != nil {
		return fmt.Errorf("isolated grok inspect managedSettingsActive has an unexpected shape: %w", err)
	}
	if managed {
		return errors.New("isolated Grok Build environment loaded external instructions or executable configuration")
	}

	configRaw, ok := fields["configSources"]
	if !ok || bytes.Equal(bytes.TrimSpace(configRaw), []byte("null")) {
		return errors.New(`isolated grok inspect response is missing required field "configSources"`)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(configRaw, &config); err != nil {
		return fmt.Errorf("isolated grok inspect configSources have an unexpected shape: %w", err)
	}
	layersRaw, ok := config["layers"]
	if !ok || bytes.Equal(bytes.TrimSpace(layersRaw), []byte("null")) {
		return errors.New(`isolated grok inspect configSources are missing "layers"`)
	}
	var layers []json.RawMessage
	if err := json.Unmarshal(layersRaw, &layers); err != nil {
		return fmt.Errorf("isolated grok inspect configSources.layers has an unexpected shape: %w", err)
	}
	if len(layers) != 0 {
		return errors.New("isolated Grok Build environment loaded external instructions or executable configuration")
	}

	var agents []struct {
		Source *struct {
			Type string `json:"type"`
		} `json:"source"`
	}
	agentsRaw, _ := json.Marshal(arrays["agents"])
	if err := json.Unmarshal(agentsRaw, &agents); err != nil {
		return fmt.Errorf("isolated grok inspect agents have an unexpected shape: %w", err)
	}
	for _, agent := range agents {
		if agent.Source == nil || agent.Source.Type != "builtin" {
			return errors.New("isolated Grok Build environment loaded a custom agent")
		}
	}
	return nil
}

func isolatedPrompt(isolation *Isolation, prompt string) string {
	if isolation.SourceRoot != "" && isolation.WorkspaceRoot != "" {
		prompt = strings.ReplaceAll(prompt, isolation.SourceRoot, isolation.WorkspaceRoot)
	}
	return fmt.Sprintf(
		"Answer from the resumed conversation and the request below. The parent project was copied into %s, and this fork's working directory is %s. You may inspect files only with absolute paths under that copied workspace through the provided read-only tools. Do not edit files, run shell commands, spawn subagents, access memory, or search the web.\n\n%s",
		isolation.WorkspaceRoot,
		isolation.WorkingDirectory,
		prompt,
	)
}

func isolatedEnvironment(base []string, isolation *Isolation) []string {
	overrides := map[string]string{
		"HOME":                     isolation.HomeDir,
		"USERPROFILE":              isolation.HomeDir,
		"XDG_CONFIG_HOME":          filepath.Join(isolation.HomeDir, ".config"),
		"CLAUDE_CONFIG_DIR":        filepath.Join(isolation.HomeDir, ".claude"),
		"GROK_HOME":                isolation.GrokHome,
		"GROK_DISABLE_AUTOUPDATER": "1",
		"GROK_SANDBOX":             isolatedSandboxProfile,
		"TMPDIR":                   filepath.Join(filepath.Dir(isolation.HomeDir), "tmp"),
		"TMP":                      filepath.Join(filepath.Dir(isolation.HomeDir), "tmp"),
		"TEMP":                     filepath.Join(filepath.Dir(isolation.HomeDir), "tmp"),
	}
	if isolation.AuthPath != "" {
		overrides["GROK_AUTH_PATH"] = isolation.AuthPath
	}
	allowed := map[string]bool{
		"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true,
		"USER": true, "LOGNAME": true, "TZ": true,
		"LANG": true, "TERM": true, "COLORTERM": true, "NO_COLOR": true,
		"XAI_API_KEY": true,
		"HTTP_PROXY":  true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, replaced := overrides[key]; replaced || key == "GROK_AUTH_PATH" {
			continue
		}
		if allowed[key] || strings.HasPrefix(key, "LC_") ||
			strings.HasPrefix(key, "PLANMAXX_GROKBUILD_") {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 32)
	hex.Encode(encoded, value[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]), nil
}

func combineCleanupError(err, cleanupErr error) error {
	if cleanupErr == nil {
		return err
	}
	if err == nil {
		return fmt.Errorf("%w: failed to prove disposable fork cleanup: %v", ErrClientUnusable, cleanupErr)
	}
	return fmt.Errorf(
		"%w: failed to prove disposable fork cleanup: %v; original operation: %w",
		ErrClientUnusable,
		cleanupErr,
		err,
	)
}

func withStderr(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}
