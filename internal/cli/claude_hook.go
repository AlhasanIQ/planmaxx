package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/spf13/cobra"
)

const claudeSessionEnvFile = "CLAUDE_ENV_FILE"

var claudeSessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type claudeSessionHookInput struct {
	SessionID string `json:"session_id"`
}

// newClaudeSessionHookCommand remains temporarily available for older
// PlanMaxx-managed Claude plugin installs. New installs use Claude Code's
// invocation-scoped CLAUDE_CODE_SESSION_ID instead of a SessionStart hook.
// This compatibility endpoint stays hidden because it is not interactive.
func newClaudeSessionHookCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "claude-session-hook",
		Short:  "Support legacy PlanMaxx Claude Code hooks",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeClaudeSessionEnvironment(cmd.InOrStdin(), os.Getenv(claudeSessionEnvFile))
		},
	}
	return cmd
}

func writeClaudeSessionEnvironment(input io.Reader, envFile string) error {
	if envFile == "" {
		return fmt.Errorf("%s is required", claudeSessionEnvFile)
	}
	var payload claudeSessionHookInput
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decode Claude SessionStart hook input: %w", err)
	}
	if !claudeSessionIDPattern.MatchString(payload.SessionID) {
		return errors.New("Claude SessionStart hook input contains an invalid session_id")
	}
	file, err := os.OpenFile(envFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open Claude environment file: %w", err)
	}
	defer file.Close()
	// Do not set PLANMAXX_AGENT here. That variable is an explicit user
	// selection and must keep taking precedence over automatic session markers.
	if _, err := fmt.Fprintf(file, "export PLANMAXX_CLAUDE_SESSION_ID='%s'\n", payload.SessionID); err != nil {
		return fmt.Errorf("write Claude environment file: %w", err)
	}
	return nil
}
