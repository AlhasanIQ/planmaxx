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
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/appserver"
	"github.com/AlhasanIQ/planmaxx/internal/browser"
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
	handoffOut          string
	autosaveOut         string
}

var execCommandContext = exec.CommandContext
var openBrowser = browser.Open
var userCacheDir = os.UserCacheDir
var userDataDir = os.UserConfigDir

func newReviewCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := reviewOptions{host: "127.0.0.1", sideQuestionTimeout: 45 * time.Second}

	cmd := &cobra.Command{
		Use:   "review <plan-file>",
		Short: "Open a blocking local review session for a Codex plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := planfile.Load(args[0])
			if err != nil {
				return err
			}
			planPath, err := filepath.Abs(plan.Path)
			if err != nil {
				planPath = plan.Path
			}
			document := review.NewDocument(planPath, plan.Markdown)
			planPath = document.CanonicalPath

			autosavePath := opts.autosaveOut
			explicitAutosave := autosavePath != ""
			if !explicitAutosave {
				autosavePath = defaultAutosavePath(planPath)
			}
			reviewSession := session.New("session-1", plan.Markdown)
			loadedPath := autosavePath
			candidates := []string{autosavePath}
			fallbackPath := ""
			if !explicitAutosave {
				var fallbackErr error
				fallbackPath, fallbackErr = cacheAutosavePath(planPath)
				if fallbackErr != nil {
					return fmt.Errorf("find review autosave fallback: %w", fallbackErr)
				}
				candidates = append(candidates, fallbackPath)
			}
			if saved, path, ok, err := loadNewestAutosave(candidates, document); err != nil {
				return fmt.Errorf("load review autosave: %w", err)
			} else if ok {
				loadedPath = path
				autosavePath = path
				reviewSession = &saved.Session
				if !saved.Document.SourceMatches(plan.Markdown) {
					reviewSession.ReconcileExternalPlan(saved.Document.SourceText, plan.Markdown)
				}
				fmt.Fprintf(stderr, "PlanMaxx restored autosave: %s\n", loadedPath)
			}
			reviewSession.PlanPath = planPath
			reviewServer := review.NewServer(reviewSession).
				WithSideQuestionTimeout(opts.sideQuestionTimeout).
				WithAutosaveDocument(document).
				WithAutosaveFallback(fallbackPath)
			store, storeErr := openRevisionStore(planPath)
			if storeErr != nil {
				return storeErr
			}
			reviewServer.WithRevisionStore(store, revisions.PlanID(planPath))
			if err := reviewServer.EnableAutosave(autosavePath); err != nil {
				if explicitAutosave {
					return fmt.Errorf("write review autosave: %w", err)
				}
				fmt.Fprintf(stderr, "PlanMaxx autosave fallback: %s (%v)\n", fallbackPath, err)
				autosavePath = fallbackPath
				reviewServer = review.NewServer(reviewSession).
					WithSideQuestionTimeout(opts.sideQuestionTimeout).
					WithAutosaveDocument(document)
				store, storeErr := openRevisionStore(planPath)
				if storeErr != nil {
					return storeErr
				}
				reviewServer.WithRevisionStore(store, revisions.PlanID(planPath))
				if err := reviewServer.EnableAutosave(autosavePath); err != nil {
					return fmt.Errorf("write review autosave fallback: %w", err)
				}
			}
			if actualPath := reviewServer.AutosavePath(); actualPath != autosavePath {
				fmt.Fprintf(stderr, "PlanMaxx autosave fallback: %s\n", actualPath)
				autosavePath = actualPath
			}
			if cleanup := tryAttachAppServerServices(cmd.Context(), stderr, reviewServer, os.Getenv("CODEX_THREAD_ID")); cleanup != nil {
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
			fmt.Fprintf(stderr, "PlanMaxx autosave: %s\n", autosavePath)
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

			var output string
			if result.Rejected {
				output, err = handoff.FormatRejected(result.Session)
			} else {
				output, err = handoff.Format(result.Session)
			}
			if err != nil {
				return err
			}
			if opts.handoffOut != "" {
				if err := os.WriteFile(opts.handoffOut, []byte(output), 0o600); err != nil {
					return fmt.Errorf("write handoff output: %w", err)
				}
			}
			_, err = fmt.Fprint(stdout, output)
			return err
		},
	}

	cmd.Flags().StringVar(&opts.host, "host", opts.host, "host interface for the local review server")
	cmd.Flags().IntVar(&opts.port, "port", opts.port, "port for the local review server; 0 chooses a random port")
	cmd.Flags().BoolVar(&opts.noBrowser, "no-browser", opts.noBrowser, "print the review URL without opening a browser")
	cmd.Flags().DurationVar(&opts.sideQuestionTimeout, "side-question-timeout", opts.sideQuestionTimeout, "maximum duration for one side-question request")
	cmd.Flags().StringVar(&opts.handoffOut, "handoff-out", opts.handoffOut, "write handoff output to this file as well as stdout")
	cmd.Flags().StringVar(&opts.autosaveOut, "autosave-out", opts.autosaveOut, "write recoverable review autosave JSON to this file")
	return cmd
}

func openRevisionStore(planPath string) (*revisions.Store, error) {
	dataDir, err := userDataDir()
	if err != nil || dataDir == "" {
		if err != nil {
			return nil, fmt.Errorf("find application data directory for revision store: %w", err)
		}
		return nil, errors.New("application data directory for revision store is empty")
	}
	planID := revisions.PlanID(planPath)
	storePath := filepath.Join(dataDir, "planmaxx", "revisions.git")
	store, err := revisions.Open(storePath)
	if err != nil {
		return nil, fmt.Errorf("open revision store: %w", err)
	}
	cacheDir, cacheErr := userCacheDir()
	if cacheErr != nil || cacheDir == "" {
		return store, nil
	}
	legacyPath := filepath.Join(cacheDir, "planmaxx", "revisions.git")
	if filepath.Clean(legacyPath) == filepath.Clean(storePath) {
		return store, nil
	}
	legacy, ok, err := revisions.OpenExisting(legacyPath)
	if err != nil {
		return nil, fmt.Errorf("open legacy cache revision store: %w", err)
	}
	if ok {
		if err := store.MigratePlanFrom(legacy, planID); err != nil {
			return nil, fmt.Errorf("migrate legacy revision store: %w", err)
		}
	}
	return store, nil
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

func tryAttachAppServerServices(ctx context.Context, stderr io.Writer, reviewServer *review.Server, currentThreadID string) func() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "PlanMaxx side questions unavailable: read current directory: %v\n", err)
		return nil
	}

	var primary sidequestions.AskClient
	var promptClient sectioniter.PromptClient
	var cleanup func()
	if currentThreadID == "" {
		reviewServer.WithSideQuestions(sidequestions.NewService(currentThreadID, primary))
		reviewServer.WithSectionIterations(sectioniter.NewService(currentThreadID, promptClient))
		return nil
	}

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
				primary = asker
				promptClient = asker
				cleanup = func() {
					stopAppServerProcess(appCmd, appStdin)
				}
			}
		}
	}

	reviewServer.WithSideQuestions(sidequestions.NewService(currentThreadID, primary))
	reviewServer.WithSectionIterations(sectioniter.NewService(currentThreadID, promptClient))
	return cleanup
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
