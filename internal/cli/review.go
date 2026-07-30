package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/appdata"
	"github.com/AlhasanIQ/planmaxx/internal/appserver"
	"github.com/AlhasanIQ/planmaxx/internal/browser"
	"github.com/AlhasanIQ/planmaxx/internal/claudecode"
	"github.com/AlhasanIQ/planmaxx/internal/handoff"
	"github.com/AlhasanIQ/planmaxx/internal/planfile"
	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"

	"github.com/spf13/cobra"
)

type reviewOptions struct {
	host                string
	port                int
	noBrowser           bool
	sideQuestionTimeout time.Duration
	agent               string
	claudeSessionID     string
	saveToFile          string
	bundleOut           string
	localBundle         bool
}

var execCommandContext = exec.CommandContext
var openBrowser = browser.Open
var userCacheDir = os.UserCacheDir
var userDataDir = os.UserConfigDir
var userStateDir = appdata.StateDir
var writePlanFile = savePlanFile
var agentLookPath = exec.LookPath
var newClaudeClient = func(sessionID, cwd string) attachedAgentClient {
	return claudecode.NewClient(sessionID, cwd)
}
var checkClaudeCapabilities = validateClaudeCapabilities

const defaultAppServerRequestTimeout = 30 * time.Minute
const claudeCapabilityCheckTimeout = 5 * time.Second
const agentAttachmentProbeTimeout = 5 * time.Second
const minimumClaudeVersion = "2.1.214"
const claudeCodeSessionIDEnvironment = "CLAUDE_CODE_SESSION_ID"
const legacyClaudeSessionIDEnvironment = "PLANMAXX_CLAUDE_SESSION_ID"

var claudeCodeSessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	agentAuto   = "auto"
	agentCodex  = "codex"
	agentClaude = "claude"
	agentNone   = "none"
)

func newReviewCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := reviewOptions{host: "127.0.0.1", sideQuestionTimeout: defaultAppServerRequestTimeout, agent: agentAuto}

	cmd := &cobra.Command{
		Use:   "review <plan-file>",
		Short: "Open a blocking local review session for a coding-agent plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := planfile.Load(args[0])
			if err != nil {
				return err
			}
			updateNotice := beginUpdateCheck(cmd.Context())
			planPath, err := filepath.Abs(plan.Path)
			if err != nil {
				planPath = plan.Path
			}
			document := review.NewDocument(planPath, plan.Markdown)
			document.PlanFormat = plan.Format
			planPath = document.CanonicalPath

			if opts.localBundle && opts.bundleOut != "" {
				return errors.New("--local-bundle cannot be combined with --bundle-out or --autosave-out")
			}

			var bundlePath string
			if opts.localBundle {
				bundlePath = review.LocalBundlePath(planPath)
			} else {
				stateRoot, err := userStateDir()
				if err != nil {
					return fmt.Errorf("find application state directory: %w", err)
				}
				bundlePath = review.BundlePath(stateRoot, planPath)
			}
			legacyAutosavePath := ""
			legacyRevisionBundle := ""
			if opts.bundleOut != "" {
				info, err := review.InspectStorageFile(opts.bundleOut)
				if err != nil {
					return fmt.Errorf("inspect review storage override: %w", err)
				}
				switch info.Kind {
				case review.StorageFileMissing, review.StorageFileBundle:
					bundlePath = opts.bundleOut
				case review.StorageFileLegacyJSON:
					if !cmd.Flags().Changed("autosave-out") {
						return fmt.Errorf("--bundle-out points to a legacy JSON autosave; run once with --autosave-out %q to import it without overwriting the original", opts.bundleOut)
					}
					legacyAutosavePath = opts.bundleOut
				case review.StorageFileLegacyBundle:
					if !cmd.Flags().Changed("autosave-out") {
						return fmt.Errorf("--bundle-out points to a legacy revision bundle; run once with --autosave-out %q to import it without overwriting the original", opts.bundleOut)
					}
					legacyRevisionBundle = opts.bundleOut
				default:
					return fmt.Errorf("review storage override is neither JSON nor a valid Git bundle: %s", opts.bundleOut)
				}
			}
			bundle, err := review.OpenBundleStore(bundlePath)
			if err != nil {
				return fmt.Errorf("open review bundle: %w", err)
			}
			defer bundle.Close()

			reviewSession := session.NewWithFormat("session-1", plan.Markdown, plan.Format)
			if _, ok := bundle.Load(); ok {
				if legacyAutosavePath != "" || legacyRevisionBundle != "" {
					return fmt.Errorf("cannot import legacy storage because the destination bundle already exists: %s", bundlePath)
				}
				fmt.Fprintf(stderr, "PlanMaxx restored bundle: %s\n", bundlePath)
			} else {
				legacyAutosaves := []string{legacyAutosavePath}
				if legacyAutosavePath == "" {
					legacyCache, cacheErr := cacheAutosavePath(planPath)
					if cacheErr != nil {
						return fmt.Errorf("find legacy review autosave: %w", cacheErr)
					}
					legacyAutosaves = []string{defaultAutosavePath(planPath), legacyCache}
				}
				importedAutosave := false
				if saved, legacyPath, ok, loadErr := loadNewestAutosave(legacyAutosaves, document); loadErr != nil {
					return fmt.Errorf("import legacy review autosave: %w", loadErr)
				} else if ok {
					bundle.WithLegacyAutosave(saved)
					importedAutosave = true
					fmt.Fprintf(stderr, "PlanMaxx imported legacy autosave: %s -> %s\n", legacyPath, bundlePath)
				}

				planRef := revisions.PlanRef(revisions.PlanID(planPath))
				if legacyRevisionBundle == "" {
					candidate := filepath.Join(filepath.Dir(planPath), ".planmaxx", "revisions.bundle")
					if info, inspectErr := review.InspectStorageFile(candidate); inspectErr != nil {
						return fmt.Errorf("inspect project legacy revision bundle: %w", inspectErr)
					} else if info.Kind == review.StorageFileLegacyBundle && info.HasRef(planRef) {
						legacyRevisionBundle = candidate
					}
				}
				if legacyRevisionBundle != "" {
					info, inspectErr := review.InspectStorageFile(legacyRevisionBundle)
					if inspectErr != nil {
						return inspectErr
					}
					if !info.HasRef(planRef) {
						return fmt.Errorf("legacy revision bundle does not contain history for this plan: %s", legacyRevisionBundle)
					}
					bundle.WithLegacyImport(legacyRevisionBundle, planRef)
					if !importedAutosave {
						imported, importedOK, importErr := review.ImportLegacyRevisionHistory(legacyRevisionBundle, planRef, *reviewSession)
						if importErr != nil {
							return fmt.Errorf("import project revision history: %w", importErr)
						}
						if importedOK {
							reviewSession = &imported
							if reviewSession.Plan != plan.Markdown {
								reviewSession.ReconcileExternalPlan(reviewSession.Plan, plan.Markdown)
							}
						}
					}
					fmt.Fprintf(stderr, "PlanMaxx imported legacy revision bundle: %s -> %s\n", legacyRevisionBundle, bundlePath)
				} else {
					legacyStore, storeErr := openLegacyRevisionStore(planPath)
					if storeErr != nil {
						return storeErr
					}
					if legacyStore != nil {
						bundle.WithLegacyImport(legacyStore.Path(), planRef)
						if !importedAutosave {
							imported, importedOK, importErr := review.ImportLegacyRevisionHistory(legacyStore.Path(), planRef, *reviewSession)
							if importErr != nil {
								return fmt.Errorf("import shared revision history: %w", importErr)
							}
							if importedOK {
								reviewSession = &imported
								if reviewSession.Plan != plan.Markdown {
									reviewSession.ReconcileExternalPlan(reviewSession.Plan, plan.Markdown)
								}
							}
						}
						fmt.Fprintf(stderr, "PlanMaxx imported legacy revision store: %s -> %s\n", legacyStore.Path(), bundlePath)
					}
				}
			}
			reviewSession.PlanPath = planPath
			reviewServer := review.NewServer(reviewSession).
				WithSideQuestionTimeout(opts.sideQuestionTimeout).
				WithAutosaveDocument(document)
			if err := reviewServer.EnableBundle(bundle); err != nil {
				return fmt.Errorf("persist review bundle: %w", err)
			}
			cleanup, err := tryAttachAgentServices(cmd.Context(), stderr, reviewServer, opts.agent, opts.claudeSessionID)
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}
			if opts.port < 0 || opts.port > 65535 {
				return fmt.Errorf("port must be between 0 and 65535")
			}
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", opts.host, opts.port))
			if err != nil {
				return fmt.Errorf("listen for review server: %w", err)
			}

			httpServer := &http.Server{Handler: reviewServer.Handler()}
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := httpServer.Shutdown(shutdownCtx); err != nil {
					_ = httpServer.Close()
				}
			}()
			go func() {
				_ = httpServer.Serve(listener)
			}()

			reviewURL := "http://" + listener.Addr().String()
			fmt.Fprintf(stderr, "PlanMaxx review URL: %s\n", reviewURL)
			fmt.Fprintf(stderr, "PlanMaxx bundle: %s\n", bundlePath)
			if !opts.noBrowser {
				if err := openBrowser(reviewURL); err != nil {
					fmt.Fprintf(stderr, "Open %s in your browser: %v\n", reviewURL, err)
				}
			}

			result, err := reviewServer.Wait(cmd.Context())
			if err != nil {
				return err
			}
			if result.Canceled {
				return fmt.Errorf("review canceled")
			}
			savePath := planPath
			if opts.saveToFile != "" {
				savePath = opts.saveToFile
			}
			if err := writePlanFile(savePath, result.Session.Plan); err != nil {
				return fmt.Errorf("save finalized plan: %w", err)
			}
			if review.NewDocument(savePath, "").CanonicalPath == planPath {
				if err := reviewServer.RecordSourceSave(result.Session.Plan); err != nil {
					return fmt.Errorf("record saved source plan: %w", err)
				}
			}

			output, err := handoff.Format(result.Session)
			if err != nil {
				return err
			}
			output = appendUpdateNotice(output, updateNotice)
			_, err = fmt.Fprint(stdout, output)
			return err
		},
	}

	cmd.Flags().StringVar(&opts.host, "host", opts.host, "host interface for the local review server")
	cmd.Flags().IntVar(&opts.port, "port", opts.port, "port for the local review server; 0 chooses a random port")
	cmd.Flags().BoolVar(&opts.noBrowser, "no-browser", opts.noBrowser, "print the review URL without opening a browser")
	cmd.Flags().StringVar(&opts.agent, "agent", opts.agent, "agent integration to use: auto, codex, claude, or none")
	cmd.Flags().StringVar(&opts.claudeSessionID, "claude-session-id", "", "Claude Code session identifier supplied by the invoked PlanMaxx skill")
	cmd.Flags().DurationVar(&opts.sideQuestionTimeout, "side-question-timeout", opts.sideQuestionTimeout, "maximum duration for one agent request")
	cmd.Flags().StringVar(&opts.saveToFile, "save-to-file", opts.saveToFile, "save the finalized plan to this file instead of the source plan")
	cmd.Flags().BoolVar(&opts.localBundle, "local-bundle", opts.localBundle, "store <plan-file>.planmaxx beside the plan instead of in user state")
	cmd.Flags().StringVar(&opts.bundleOut, "bundle-out", opts.bundleOut, "write the recoverable single-file Git review bundle here")
	cmd.Flags().StringVar(&opts.bundleOut, "autosave-out", opts.bundleOut, "deprecated alias for --bundle-out")
	_ = cmd.Flags().MarkDeprecated("autosave-out", "use --bundle-out")
	_ = cmd.Flags().MarkHidden("autosave-out")
	for _, name := range []string{"host", "port", "side-question-timeout", "bundle-out", "claude-session-id"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}

func savePlanFile(path string, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func openLegacyRevisionStore(planPath string) (*revisions.Store, error) {
	planID := revisions.PlanID(planPath)
	var candidates []string
	if dataDir, err := userDataDir(); err != nil {
		return nil, fmt.Errorf("find legacy application data directory: %w", err)
	} else if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "planmaxx", "revisions.git"))
	}
	if cacheDir, err := userCacheDir(); err == nil && cacheDir != "" {
		candidates = append(candidates, filepath.Join(cacheDir, "planmaxx", "revisions.git"))
	}
	for _, candidate := range candidates {
		store, ok, err := revisions.OpenExisting(candidate)
		if err != nil {
			return nil, fmt.Errorf("open legacy revision store: %w", err)
		}
		if ok && store.HasPlan(planID) {
			return store, nil
		}
	}
	return nil, nil
}

func defaultAutosavePath(planPath string) string {
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return planPath + ".planmaxx-review.json"
	}
	return abs + ".planmaxx-review.json"
}

func cacheAutosavePath(planPath string) (string, error) {
	cacheDir, err := userCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = os.TempDir()
	}
	abs, err := filepath.Abs(planPath)
	if err != nil {
		abs = planPath
	}
	sum := sha256.Sum256([]byte(abs))
	name := hex.EncodeToString(sum[:16]) + ".planmaxx-review.json"
	return filepath.Join(cacheDir, "planmaxx", "reviews", name), nil
}

func loadNewestAutosave(paths []string, document review.Document) (review.Autosave, string, bool, error) {
	var newest review.Autosave
	var newestPath string
	var firstErr error
	for _, path := range paths {
		saved, ok, err := review.LoadAutosave(path)
		if err != nil {
			if errors.Is(err, review.ErrFutureAutosave) {
				return review.Autosave{}, "", false, fmt.Errorf("%s: %w", path, err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", path, err)
			}
			continue
		}
		if !ok {
			continue
		}
		if !saved.Document.MatchesPath(document.CanonicalPath) {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s belongs to %q, not %q", path, saved.Document.CanonicalPath, document.CanonicalPath)
			}
			continue
		}
		if newestPath == "" || saved.SavedAt.After(newest.SavedAt) {
			newest, newestPath = saved, path
		}
	}
	if newestPath == "" && firstErr != nil {
		return review.Autosave{}, "", false, firstErr
	}
	return newest, newestPath, newestPath != "", nil
}

type attachedAgentClient interface {
	sidequestions.AskClient
	sectioniter.PromptClient
}

type monitoredAgentClient struct {
	inner     attachedAgentClient
	failed    atomic.Bool
	onFailure func()
}

func (c *monitoredAgentClient) Ask(ctx context.Context, request sidequestions.Request) (string, error) {
	if err := c.unavailableError(); err != nil {
		return "", err
	}
	answer, err := c.inner.Ask(ctx, request)
	if err != nil {
		c.fail(err)
	}
	return answer, err
}

func (c *monitoredAgentClient) AskPrompt(ctx context.Context, prompt string) (string, error) {
	if err := c.unavailableError(); err != nil {
		return "", err
	}
	answer, err := c.inner.AskPrompt(ctx, prompt)
	if err != nil {
		c.fail(err)
	}
	return answer, err
}

func (c *monitoredAgentClient) unavailableError() error {
	if c == nil || c.inner == nil {
		return errors.New("agent integration is unavailable")
	}
	if c.failed.Load() {
		return errors.New("agent integration is unavailable after its last request failed")
	}
	return nil
}

func (c *monitoredAgentClient) fail(err error) {
	if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) &&
		!errors.Is(err, appserver.ErrClientUnusable) {
		return
	}
	if c.failed.CompareAndSwap(false, true) && c.onFailure != nil {
		c.onFailure()
	}
}

func monitorAgentClient(client attachedAgentClient, reviewServer *review.Server, displayName string, cleanup ...func()) attachedAgentClient {
	return &monitoredAgentClient{
		inner: client,
		onFailure: func() {
			for _, stop := range cleanup {
				if stop != nil {
					stop()
				}
			}
			reviewServer.MarkAgentUnavailable(
				displayName + " failed its last assisted request. Restart the review after checking the active agent session.",
			)
		},
	}
}

type agentSelection struct {
	id          string
	displayName string
	sessionID   string
}

func resolveAgentSelection(requested string, invokedClaudeSessionID string) (agentSelection, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	invokedClaudeSessionID = strings.TrimSpace(invokedClaudeSessionID)
	if requested == "" {
		requested = agentAuto
	}
	if requested == agentAuto {
		if configured := strings.ToLower(strings.TrimSpace(os.Getenv("PLANMAXX_AGENT"))); configured != "" {
			requested = configured
		}
	}
	if requested == agentAuto {
		switch {
		case invokedClaudeSessionID != "":
			requested = agentClaude
		case strings.TrimSpace(os.Getenv(claudeCodeSessionIDEnvironment)) != "":
			requested = agentClaude
		case strings.TrimSpace(os.Getenv(legacyClaudeSessionIDEnvironment)) != "":
			requested = agentClaude
		case os.Getenv("CODEX_THREAD_ID") != "":
			requested = agentCodex
		default:
			requested = agentNone
		}
	}
	switch requested {
	case agentCodex:
		return agentSelection{id: agentCodex, displayName: "Codex", sessionID: strings.TrimSpace(os.Getenv("CODEX_THREAD_ID"))}, nil
	case agentClaude:
		sessionID, source := selectedClaudeSessionID(invokedClaudeSessionID)
		if sessionID != "" && !claudeCodeSessionIDPattern.MatchString(sessionID) {
			return agentSelection{}, fmt.Errorf("invalid Claude Code session identifier in %s", source)
		}
		return agentSelection{id: agentClaude, displayName: "Claude Code", sessionID: sessionID}, nil
	case agentNone:
		return agentSelection{id: agentNone, displayName: "Agent"}, nil
	default:
		return agentSelection{}, fmt.Errorf("unsupported agent %q; expected auto, codex, claude, or none", requested)
	}
}

func selectedClaudeSessionID(invokedClaudeSessionID string) (string, string) {
	if invokedClaudeSessionID != "" {
		return invokedClaudeSessionID, "--claude-session-id"
	}
	if sessionID := strings.TrimSpace(os.Getenv(claudeCodeSessionIDEnvironment)); sessionID != "" {
		return sessionID, claudeCodeSessionIDEnvironment
	}
	return strings.TrimSpace(os.Getenv(legacyClaudeSessionIDEnvironment)), legacyClaudeSessionIDEnvironment
}

func tryAttachAgentServices(
	ctx context.Context,
	stderr io.Writer,
	reviewServer *review.Server,
	requestedAgent string,
	invokedClaudeSessionID string,
) (func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "PlanMaxx side questions unavailable: read current directory: %v\n", err)
		attachUnavailableAgentServices(reviewServer, review.AgentInfo{
			ID:                agentNone,
			DisplayName:       "Agent",
			ContextMode:       "unavailable",
			UnavailableReason: "PlanMaxx could not determine the agent working directory.",
		})
		return nil, nil
	}

	selection, err := resolveAgentSelection(requestedAgent, invokedClaudeSessionID)
	if err != nil {
		return nil, err
	}
	if selection.id == agentNone {
		attachUnavailableAgentServices(reviewServer, review.AgentInfo{
			ID:                agentNone,
			DisplayName:       "Agent",
			ContextMode:       "unavailable",
			UnavailableReason: "No supported active agent session was detected.",
		})
		return nil, nil
	}
	if selection.sessionID == "" {
		reason := "The active agent session identifier is unavailable."
		if selection.id == agentCodex {
			reason = "CODEX_THREAD_ID is not set for this review."
		} else if selection.id == agentClaude {
			reason = "Claude Code did not provide its active session. Launch the review from the installed skill or another Claude Code tool command."
		}
		attachUnavailableAgentServices(reviewServer, review.AgentInfo{
			ID: selection.id, DisplayName: selection.displayName, ContextMode: "unavailable", UnavailableReason: reason,
		})
		return nil, nil
	}

	switch selection.id {
	case agentClaude:
		if _, err := agentLookPath("claude"); err != nil {
			fmt.Fprintf(stderr, "PlanMaxx assisted actions unavailable: find Claude Code: %v\n", err)
			attachUnavailableAgentServices(reviewServer, review.AgentInfo{
				ID: selection.id, DisplayName: selection.displayName, ContextMode: "unavailable",
				UnavailableReason: "The Claude Code executable is not available on PATH.",
			})
			return nil, nil
		}
		if err := checkClaudeCapabilities(ctx, cwd); err != nil {
			fmt.Fprintf(stderr, "PlanMaxx assisted actions unavailable: check Claude Code capabilities: %v\n", err)
			attachUnavailableAgentServices(reviewServer, review.AgentInfo{
				ID: selection.id, DisplayName: selection.displayName, ContextMode: "unavailable",
				UnavailableReason: "This Claude Code version does not support PlanMaxx's isolated, non-persistent session forks.",
			})
			return nil, nil
		}
		client := monitorAgentClient(newClaudeClient(selection.sessionID, cwd), reviewServer, selection.displayName)
		reviewServer.
			WithAgent(review.AgentInfo{ID: agentClaude, DisplayName: selection.displayName, ContextMode: "current-session-fork", Available: true}).
			WithSideQuestions(sidequestions.NewService(selection.sessionID, client)).
			WithSectionIterations(sectioniter.NewService(selection.sessionID, client))
		return nil, nil
	case agentCodex:
		return attachCodexAppServer(ctx, stderr, reviewServer, selection, cwd), nil
	default:
		return nil, fmt.Errorf("unsupported resolved agent %q", selection.id)
	}
}

func validateClaudeCapabilities(parent context.Context, cwd string) error {
	ctx, cancel := context.WithTimeout(parent, claudeCapabilityCheckTimeout)
	defer cancel()

	versionCommand := execCommandContext(ctx, "claude", "--version")
	versionCommand.Dir = cwd
	versionOutput, err := versionCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run claude --version: %w", err)
	}
	if err := requireMinimumClaudeVersion(strings.TrimSpace(string(versionOutput))); err != nil {
		return err
	}

	command := execCommandContext(ctx, "claude", "--help")
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run claude --help: %w", err)
	}
	help := string(output)
	for _, flag := range []string{"--fork-session", "--safe-mode", "--no-session-persistence", "--output-format", "--tools", "--permission-mode"} {
		if !strings.Contains(help, flag) {
			return fmt.Errorf("claude --help does not advertise required flag %s", flag)
		}
	}
	return nil
}

func requireMinimumClaudeVersion(output string) error {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return errors.New("parse Claude Code version: empty output")
	}
	version := strings.TrimPrefix(fields[0], "v")
	parts := strings.SplitN(version, ".", 4)
	if len(parts) < 3 {
		return fmt.Errorf("parse Claude Code version %q", output)
	}
	numbers := make([]int, 3)
	for index := range numbers {
		number, err := strconv.Atoi(parts[index])
		if err != nil {
			return fmt.Errorf("parse Claude Code version %q", output)
		}
		numbers[index] = number
	}
	minimum := [3]int{2, 1, 214}
	for index, number := range numbers {
		if number > minimum[index] {
			return nil
		}
		if number < minimum[index] {
			return fmt.Errorf("Claude Code %s or newer is required; found %s", minimumClaudeVersion, version)
		}
	}
	return nil
}

func attachCodexAppServer(ctx context.Context, stderr io.Writer, reviewServer *review.Server, selection agentSelection, cwd string) func() {
	currentThreadID := selection.sessionID
	var primary sidequestions.AskClient
	var promptClient sectioniter.PromptClient
	var cleanup func()
	unavailableReason := "PlanMaxx could not start the Codex app-server integration."
	appCmd := execCommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	appStdout, err := appCmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(stderr, "PlanMaxx side questions unavailable: open app-server stdout: %v\n", err)
	} else {
		appStdin, err := appCmd.StdinPipe()
		if err != nil {
			fmt.Fprintf(stderr, "PlanMaxx side questions unavailable: open app-server stdin: %v\n", err)
		} else {
			appCmd.Stderr = stderr
			if err := appCmd.Start(); err != nil {
				fmt.Fprintf(stderr, "PlanMaxx side questions unavailable: start app-server: %v\n", err)
			} else {
				client := appserver.NewClient(bufio.NewReader(appStdout), appStdin)
				asker := &appserver.SideQuestionAsker{Client: client, CWD: cwd, CurrentThreadID: currentThreadID}
				var stopOnce sync.Once
				stop := func() {
					stopOnce.Do(func() {
						stopAppServerProcess(appCmd, appStdin)
					})
				}
				probeCtx, cancel := context.WithTimeout(ctx, agentAttachmentProbeTimeout)
				probeErr := asker.ValidateAttachment(probeCtx)
				cancel()
				if probeErr != nil {
					fmt.Fprintf(stderr, "PlanMaxx assisted actions unavailable: validate Codex attachment: %v\n", probeErr)
					unavailableReason = "PlanMaxx could not validate the active Codex thread."
					stop()
				} else {
					cleanup = stop
					monitored := monitorAgentClient(asker, reviewServer, selection.displayName, cleanup)
					primary = monitored
					promptClient = monitored
					reviewServer.WithAgent(review.AgentInfo{
						ID: agentCodex, DisplayName: selection.displayName, ContextMode: "current-session-fork", Available: true,
					})
				}
			}
		}
	}

	if cleanup == nil {
		reviewServer.WithAgent(review.AgentInfo{
			ID: agentCodex, DisplayName: selection.displayName, ContextMode: "unavailable",
			UnavailableReason: unavailableReason,
		})
	}
	reviewServer.WithSideQuestions(sidequestions.NewService(currentThreadID, primary))
	reviewServer.WithSectionIterations(sectioniter.NewService(currentThreadID, promptClient))
	return cleanup
}

func attachUnavailableAgentServices(reviewServer *review.Server, info review.AgentInfo) {
	reviewServer.
		WithAgent(info).
		WithSideQuestions(sidequestions.NewService("", nil)).
		WithSectionIterations(sectioniter.NewService("", nil))
}

func stopAppServerProcess(cmd *exec.Cmd, stdin io.Closer) {
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
