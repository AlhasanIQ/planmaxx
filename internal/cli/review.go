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

	"github.com/AlhasanIQ/planmaxx/internal/appdata"
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

const defaultAppServerRequestTimeout = 30 * time.Minute

func newReviewCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := reviewOptions{host: "127.0.0.1", sideQuestionTimeout: defaultAppServerRequestTimeout}

	cmd := &cobra.Command{
		Use:   "review <plan-file>",
		Short: "Open a blocking local review session for a Codex plan",
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
	cmd.Flags().DurationVar(&opts.sideQuestionTimeout, "side-question-timeout", opts.sideQuestionTimeout, "maximum duration for one Codex app-server request")
	cmd.Flags().StringVar(&opts.saveToFile, "save-to-file", opts.saveToFile, "save the finalized plan to this file instead of the source plan")
	cmd.Flags().BoolVar(&opts.localBundle, "local-bundle", opts.localBundle, "store <plan-file>.planmaxx beside the plan instead of in user state")
	cmd.Flags().StringVar(&opts.bundleOut, "bundle-out", opts.bundleOut, "write the recoverable single-file Git review bundle here")
	cmd.Flags().StringVar(&opts.bundleOut, "autosave-out", opts.bundleOut, "deprecated alias for --bundle-out")
	_ = cmd.Flags().MarkDeprecated("autosave-out", "use --bundle-out")
	_ = cmd.Flags().MarkHidden("autosave-out")
	for _, name := range []string{"host", "port", "side-question-timeout", "bundle-out"} {
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
