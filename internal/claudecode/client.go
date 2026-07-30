package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

// CommandFactory creates a Claude Code process. The default factory uses
// exec.CommandContext so cancellation terminates the process and Run waits for
// it to be reaped.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

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

// Client asks questions in forks of an existing Claude Code session.
//
// Claude Code print-mode operations are serialized because multiple concurrent
// resumes of the same session are not safe. The gate is context-aware so a
// canceled caller does not remain queued behind another operation.
type Client struct {
	sessionID        string
	workingDirectory string
	command          CommandFactory
	gate             chan struct{}
}

// NewClient creates a Claude Code client for sessionID. Commands run in
// workingDirectory; an empty directory inherits the PlanMaxx process directory.
func NewClient(sessionID, workingDirectory string, options ...Option) *Client {
	client := &Client{
		sessionID:        sessionID,
		workingDirectory: workingDirectory,
		command:          exec.CommandContext,
		gate:             make(chan struct{}, 1),
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
		return "", fmt.Errorf("Claude Code client is unavailable")
	}
	return c.askPromptInFork(ctx, c.sessionID, prompt)
}

func (c *Client) askPromptInFork(ctx context.Context, sessionID, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("Claude Code client is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("Claude Code session id is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("Claude Code prompt is required")
	}
	if c.command == nil || c.gate == nil {
		return "", fmt.Errorf("Claude Code command factory is unavailable")
	}

	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	args := []string{
		"-p",
		"--resume", sessionID,
		"--fork-session",
		"--safe-mode",
		"--no-session-persistence",
		"--output-format", "json",
		"--tools", "",
		"--permission-mode", "dontAsk",
	}
	command := c.command(ctx, "claude", args...)
	if command == nil {
		return "", fmt.Errorf("Claude Code command factory returned no command")
	}
	command.Dir = c.workingDirectory
	command.Stdin = strings.NewReader(prompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return "", withStderr(fmt.Errorf("run Claude Code: %w", err), stderr.String())
	}

	var response struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Session string `json:"session_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return "", withStderr(fmt.Errorf("decode Claude Code response: %w", err), stderr.String())
	}
	if response.Type != "result" {
		return "", withStderr(fmt.Errorf("Claude Code returned response type %q, expected %q", response.Type, "result"), stderr.String())
	}
	if response.IsError || response.Subtype != "success" {
		subtype := response.Subtype
		if subtype == "" {
			subtype = "unknown"
		}
		return "", withStderr(fmt.Errorf("Claude Code operation did not succeed (subtype %q)", subtype), stderr.String())
	}
	if strings.TrimSpace(response.Result) == "" {
		return "", withStderr(errors.New("Claude Code returned an empty result"), stderr.String())
	}
	if strings.TrimSpace(response.Session) == "" {
		return "", withStderr(errors.New("Claude Code did not report the disposable fork session id"), stderr.String())
	}
	if response.Session == sessionID {
		return "", withStderr(errors.New("Claude Code reused the source session instead of creating a disposable fork"), stderr.String())
	}
	return response.Result, nil
}

func withStderr(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}
