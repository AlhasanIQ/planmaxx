package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const codexBinaryEnvironment = "PLANMAXX_CODEX_BIN"

var findCodexExecutableCandidates = resolveCodexExecutableCandidates

type codexAppServerProcess struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	executable string
}

func resolveCodexExecutableCandidates(explicit string) []string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return []string{explicit}
	}

	// Keep the normal PATH lookup first. On Windows, Codex Desktop may also
	// expose a protected WindowsApps alias that resolves successfully but cannot
	// be launched by child processes, so user-owned desktop runtimes are added as
	// fallbacks below.
	candidates := []string{"codex"}
	candidates = append(candidates, codexDesktopExecutableCandidates()...)
	return dedupeExecutableCandidates(candidates)
}

func dedupeExecutableCandidates(candidates []string) []string {
	seen := make(map[string]bool, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		key := filepath.Clean(candidate)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, candidate)
	}
	return result
}

func startCodexAppServer(ctx context.Context, cwd string, explicit string, stderr io.Writer) (*codexAppServerProcess, error) {
	candidates := findCodexExecutableCandidates(explicit)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Codex executable candidates were found; set %s or --codex-bin", codexBinaryEnvironment)
	}

	attempts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		cmd := execCommandContext(ctx, candidate, "app-server", "--listen", "stdio://")
		cmd.Dir = cwd
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: open stdout: %v", displayCodexCommand(cmd, candidate), err))
			continue
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			_ = stdout.Close()
			attempts = append(attempts, fmt.Sprintf("%s: open stdin: %v", displayCodexCommand(cmd, candidate), err))
			continue
		}
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			attempts = append(attempts, fmt.Sprintf("%s: %v", displayCodexCommand(cmd, candidate), err))
			continue
		}
		return &codexAppServerProcess{cmd: cmd, stdin: stdin, stdout: stdout, executable: displayCodexCommand(cmd, candidate)}, nil
	}

	return nil, fmt.Errorf("all Codex launch attempts failed (%s); set %s or --codex-bin to a runnable Codex CLI", strings.Join(attempts, "; "), codexBinaryEnvironment)
}

func displayCodexCommand(cmd *exec.Cmd, fallback string) string {
	if cmd != nil && strings.TrimSpace(cmd.Path) != "" {
		return cmd.Path
	}
	return fallback
}
