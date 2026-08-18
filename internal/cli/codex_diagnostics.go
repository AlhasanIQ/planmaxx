package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/appserver"
)

const codexDoctorTimeout = 8 * time.Second

type codexIntegrationReport struct {
	Executable           string
	Version              string
	CLIError             error
	AppServerInitialized bool
	AppServerError       error
	ThreadID             string
	ThreadReadable       bool
	ThreadError          error
}

var inspectCodexIntegration = diagnoseCodexIntegration

func diagnoseCodexIntegration(parent context.Context, cwd string, explicit string, threadID string) codexIntegrationReport {
	ctx, cancel := context.WithTimeout(parent, codexDoctorTimeout)
	defer cancel()

	candidate, executable, version, err := findRunnableCodexVersion(ctx, cwd, explicit)
	report := codexIntegrationReport{Executable: executable, Version: version, CLIError: err, ThreadID: strings.TrimSpace(threadID)}
	if err != nil {
		return report
	}

	process, err := startCodexAppServer(ctx, cwd, candidate, io.Discard)
	if err != nil {
		report.AppServerError = err
		return report
	}
	defer stopAppServerProcess(process.cmd, process.stdin)

	client := appserver.NewClient(bufio.NewReader(process.stdout), process.stdin)
	if _, err := client.Initialize(ctx); err != nil {
		report.AppServerError = fmt.Errorf("initialize: %w", err)
		return report
	}
	if err := client.NotifyInitialized(ctx); err != nil {
		report.AppServerError = fmt.Errorf("notify initialized: %w", err)
		return report
	}
	report.AppServerInitialized = true
	if report.ThreadID == "" {
		return report
	}
	thread, err := client.ReadThread(ctx, report.ThreadID)
	if err != nil {
		report.ThreadError = err
		return report
	}
	if thread.Thread.ID != report.ThreadID {
		report.ThreadError = fmt.Errorf("read thread %q, expected %q", thread.Thread.ID, report.ThreadID)
		return report
	}
	report.ThreadReadable = true
	return report
}

func findRunnableCodexVersion(ctx context.Context, cwd string, explicit string) (candidate string, executable string, version string, err error) {
	candidates := findCodexExecutableCandidates(explicit)
	if len(candidates) == 0 {
		return "", "", "", fmt.Errorf("no Codex executable candidates were found; set %s or --codex-bin", codexBinaryEnvironment)
	}
	attempts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		command := execCommandContext(ctx, candidate, "--version")
		command.Dir = cwd
		output, commandErr := command.CombinedOutput()
		display := displayCodexCommand(command, candidate)
		if commandErr != nil {
			detail := strings.TrimSpace(string(output))
			if detail != "" {
				attempts = append(attempts, fmt.Sprintf("%s: %v (%s)", display, commandErr, detail))
			} else {
				attempts = append(attempts, fmt.Sprintf("%s: %v", display, commandErr))
			}
			continue
		}
		return candidate, display, strings.TrimSpace(string(output)), nil
	}
	return "", "", "", fmt.Errorf("all Codex version checks failed (%s)", strings.Join(attempts, "; "))
}
