package grokbuild

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	maxACPMessageBytes = 64 << 20
	acpExitTimeout     = 5 * time.Second
)

type acpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data,omitempty"`
	} `json:"error,omitempty"`
}

func relocateSessionWithACP(
	ctx context.Context,
	isolation *Isolation,
	sourceSessionID string,
	childSessionID string,
) (forkCreated bool, returnErr error) {
	command := exec.CommandContext(ctx, "grok", "agent", "--no-leader", "stdio")
	command.Dir = isolation.WorkingDirectory
	command.Env = isolatedEnvironment(os.Environ(), isolation)
	stdin, err := command.StdinPipe()
	if err != nil {
		return false, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return false, err
	}
	stderr := newBoundedBuffer(maxGrokErrorBytes)
	command.Stderr = stderr
	prepareProcess(command)
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start Grok Build ACP agent: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		waitDone := make(chan error, 1)
		go func() { waitDone <- command.Wait() }()
		select {
		case waitErr := <-waitDone:
			if returnErr == nil && waitErr != nil {
				returnErr = fmt.Errorf("wait for Grok Build ACP agent: %w", waitErr)
			}
		case <-time.After(acpExitTimeout):
			if command.Process != nil {
				_ = killProcessTree(command)
			}
			<-waitDone
			if returnErr == nil {
				returnErr = errors.New("Grok Build ACP agent did not exit after stdin closed")
			}
		}
		if returnErr != nil {
			returnErr = withStderr(returnErr, stderr.String())
		}
	}()

	reader := bufio.NewReaderSize(stdout, 64<<10)
	initialize := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": 1,
			"clientCapabilities": map[string]any{
				"fs": map[string]bool{
					"readTextFile":  false,
					"writeTextFile": false,
				},
				"terminal": false,
			},
			"clientInfo": map[string]string{
				"name":    "planmaxx",
				"version": "grok-integration-v1",
			},
		},
		"_meta": map[string]any{
			"clientType":    "planmaxx",
			"clientVersion": "grok-integration-v1",
			"startupHints": map[string]bool{
				"nonInteractive":    true,
				"skipGitStatus":     true,
				"skipProjectLayout": true,
			},
		},
	}
	response, err := exchangeACP(stdin, reader, initialize, 1)
	if err != nil {
		return false, fmt.Errorf("initialize Grok Build ACP agent: %w", err)
	}
	var initialized struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
		Meta struct {
			GrokShell bool `json:"grokShell"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(response, &initialized); err != nil {
		return false, fmt.Errorf("decode Grok Build ACP initialize result: %w", err)
	}
	if initialized.ProtocolVersion != 1 ||
		!initialized.AgentCapabilities.LoadSession ||
		!initialized.Meta.GrokShell {
		return false, fmt.Errorf("Grok Build ACP agent does not advertise the required session protocol")
	}

	fork := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "_x.ai/session/fork",
		"params": map[string]any{
			"sourceSessionId":    sourceSessionID,
			"sourceCwd":          isolation.SourceWorkingDirectory,
			"sourceWorkspaceDir": isolation.SourceRoot,
			"newCwd":             isolation.WorkingDirectory,
			"newSessionId":       childSessionID,
			"sessionKind":        "fork",
		},
	}
	// Once the fork request is handed to the ACP transport, the child may exist
	// even if its response is lost. Cleanup by the caller is therefore required
	// on every error from this point; deleting an absent UUID is idempotent.
	forkCreated = true
	response, err = exchangeACP(stdin, reader, fork, 2)
	if err != nil {
		return true, fmt.Errorf("fork Grok Build session through ACP: %w", err)
	}
	var forked struct {
		NewSessionID    string `json:"newSessionId"`
		NewCWD          string `json:"newCwd"`
		ParentSessionID string `json:"parentSessionId"`
	}
	if err := json.Unmarshal(response, &forked); err != nil {
		return true, fmt.Errorf("decode Grok Build ACP fork result: %w", err)
	}
	if forked.NewSessionID != childSessionID ||
		forked.NewCWD != isolation.WorkingDirectory ||
		forked.ParentSessionID != sourceSessionID {
		return true, fmt.Errorf("Grok Build ACP returned an inconsistent session fork")
	}
	return true, nil
}

func exchangeACP(
	writer io.Writer,
	reader *bufio.Reader,
	request map[string]any,
	expectedID int,
) (json.RawMessage, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return nil, err
	}
	for {
		line, err := readACPLine(reader)
		if err != nil {
			return nil, err
		}
		var envelope acpEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return nil, fmt.Errorf("decode ACP message: %w", err)
		}
		if envelope.Method != "" && len(envelope.ID) != 0 {
			return nil, fmt.Errorf("Grok Build ACP requested unsupported client method %s", envelope.Method)
		}
		if len(envelope.ID) == 0 || strings.TrimSpace(string(envelope.ID)) != fmt.Sprint(expectedID) {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf(
				"ACP error %d: %s",
				envelope.Error.Code,
				strings.TrimSpace(envelope.Error.Message),
			)
		}
		if len(envelope.Result) == 0 {
			return nil, errors.New("ACP response is missing a result")
		}
		return envelope.Result, nil
	}
}

func readACPLine(reader *bufio.Reader) ([]byte, error) {
	var message []byte
	for {
		fragment, prefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(message)+len(fragment) > maxACPMessageBytes {
			return nil, fmt.Errorf("Grok Build ACP message exceeds %d bytes", maxACPMessageBytes)
		}
		message = append(message, fragment...)
		if !prefix {
			return message, nil
		}
	}
}
