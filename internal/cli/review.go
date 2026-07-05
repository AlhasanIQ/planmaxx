package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

			autosavePath := opts.autosaveOut
			explicitAutosave := autosavePath != ""
			if !explicitAutosave {
				autosavePath = defaultAutosavePath(args[0])
			}
			reviewSession := session.New("session-1", plan.Markdown)
			if saved, ok, err := review.LoadAutosave(autosavePath); err != nil {
				return fmt.Errorf("load review autosave: %w", err)
			} else if ok {
				reviewSession = &saved.Session
				if reviewSession.Plan != plan.Markdown {
					reviewSession.AddTurnRevision(plan.Markdown, "Plan updated by Codex turn")
				}
				fmt.Fprintf(stderr, "PlanMaxx restored autosave: %s\n", autosavePath)
			}
			reviewSession.PlanPath = planPath
			reviewServer := review.NewServer(reviewSession).WithSideQuestionTimeout(opts.sideQuestionTimeout)
			if err := reviewServer.EnableAutosave(autosavePath); err != nil {
				if explicitAutosave {
					return fmt.Errorf("write review autosave: %w", err)
				}
				fallbackPath, fallbackErr := cacheAutosavePath(planPath)
				if fallbackErr != nil {
					return fmt.Errorf("write review autosave: %w", err)
				}
				fmt.Fprintf(stderr, "PlanMaxx autosave fallback: %s (%v)\n", fallbackPath, err)
				autosavePath = fallbackPath
				reviewServer = review.NewServer(reviewSession).WithSideQuestionTimeout(opts.sideQuestionTimeout)
				if err := reviewServer.EnableAutosave(autosavePath); err != nil {
					return fmt.Errorf("write review autosave fallback: %w", err)
				}
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
