package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

type Client struct {
	reader *bufio.Reader
	writer io.Writer

	// App-server responses are matched while one operation owns the protocol
	// stream, so the client intentionally permits one in-flight operation at a
	// time. The semaphore makes waiting for that slot cancellable.
	operationSem chan struct{}
	lines        chan lineResult
	startReader  sync.Once
	nextID       atomic.Int64
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type ThreadResponse struct {
	Thread                Thread   `json:"thread"`
	CWD                   string   `json:"cwd"`
	Model                 string   `json:"model"`
	ModelProvider         string   `json:"modelProvider"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots"`
}

type Thread struct {
	ID           string       `json:"id"`
	ForkedFromID string       `json:"forkedFromId"`
	Ephemeral    bool         `json:"ephemeral"`
	CWD          string       `json:"cwd"`
	Status       ThreadStatus `json:"status"`
}

type ThreadStatus struct {
	Type string `json:"type"`
}

type TurnResponse struct {
	Turn Turn `json:"turn"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func NewClient(reader *bufio.Reader, writer io.Writer) *Client {
	client := &Client{
		reader:       reader,
		writer:       writer,
		operationSem: make(chan struct{}, 1),
		lines:        make(chan lineResult, 64),
	}
	client.operationSem <- struct{}{}
	client.nextID.Store(1)
	return client
}

func (c *Client) Initialize(ctx context.Context) (InitializeResponse, error) {
	var result InitializeResponse
	err := c.call(ctx, "initialize", initializeParams{
		ClientInfo: clientInfo{
			Name:    "planmaxx",
			Version: "0.0.0",
		},
		Capabilities: capabilities{
			ExperimentalAPI:           true,
			RequestAttestation:        false,
			OptOutNotificationMethods: []string{},
		},
	}, &result)
	return result, err
}

// NotifyInitialized sends the post-initialize handshake notification.
func (c *Client) NotifyInitialized(ctx context.Context) error {
	release, err := c.acquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := json.NewEncoder(c.writer).Encode(rpcNotification{Method: "initialized"}); err != nil {
		return fmt.Errorf("write app-server initialized notification: %w", err)
	}
	return nil
}

func (c *Client) ReadThread(ctx context.Context, threadID string) (ThreadResponse, error) {
	var result ThreadResponse
	err := c.call(ctx, "thread/read", readThreadParams{
		ThreadID:     threadID,
		IncludeTurns: false,
	}, &result)
	return result, err
}

func (c *Client) ForkThread(ctx context.Context, threadID string, cwd string) (ThreadResponse, error) {
	var result ThreadResponse
	err := c.call(ctx, "thread/fork", forkThreadParams{
		ThreadID:               threadID,
		CWD:                    cwd,
		Ephemeral:              true,
		ExcludeTurns:           true,
		ApprovalPolicy:         "never",
		PersistExtendedHistory: false,
	}, &result)
	return result, err
}

func (c *Client) StartTurn(ctx context.Context, threadID string, prompt string) (TurnResponse, error) {
	var result TurnResponse
	err := c.call(ctx, "turn/start", startTurnParams{
		ThreadID:       threadID,
		ApprovalPolicy: "never",
		Input: []turnInput{{
			Type:         "text",
			Text:         prompt,
			TextElements: []any{},
		}},
	}, &result)
	return result, err
}

func (c *Client) StartTurnAndWait(ctx context.Context, threadID string, prompt string) (string, error) {
	release, err := c.acquireOperation(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	var result TurnResponse
	if err := c.callLocked(ctx, "turn/start", startTurnParams{
		ThreadID:       threadID,
		ApprovalPolicy: "never",
		Input: []turnInput{{
			Type:         "text",
			Text:         prompt,
			TextElements: []any{},
		}},
	}, &result); err != nil {
		return "", err
	}
	return c.waitForTurnAnswerLocked(ctx, threadID, result.Turn.ID)
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	release, err := c.acquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := ctx.Err(); err != nil {
		return err
	}
	return c.callLocked(ctx, method, params, result)
}

func (c *Client) callLocked(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1) - 1
	request := rpcRequest{
		ID:     id,
		Method: method,
		Params: params,
	}
	if err := json.NewEncoder(c.writer).Encode(request); err != nil {
		return fmt.Errorf("write app-server %s request: %w", method, err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := c.nextLine(ctx)
		if err != nil {
			return fmt.Errorf("read app-server %s response: %w", method, err)
		}

		var envelope rpcResponse
		if err := json.Unmarshal(line, &envelope); err != nil {
			return fmt.Errorf("decode app-server %s message: %w", method, err)
		}
		matches, err := responseIDMatches(envelope.ID, id)
		if err != nil {
			return fmt.Errorf("decode app-server %s response id: %w", method, err)
		}
		if !matches {
			continue
		}
		if envelope.Error != nil {
			return fmt.Errorf("app-server %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
		}
		if len(envelope.Result) == 0 {
			return fmt.Errorf("decode app-server %s result: %w", method, errors.New("missing result"))
		}
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode app-server %s result: %w", method, err)
		}
		return nil
	}
}

func (c *Client) acquireOperation(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.operationSem:
		return func() {
			c.operationSem <- struct{}{}
		}, nil
	}
}

func (c *Client) nextLine(ctx context.Context) ([]byte, error) {
	c.startReader.Do(func() {
		go c.readLines()
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result, ok := <-c.lines:
		if !ok {
			return nil, io.EOF
		}
		if result.err != nil {
			return nil, result.err
		}
		return result.line, nil
	}
}

func (c *Client) readLines() {
	defer close(c.lines)
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.lines <- lineResult{err: err}
			return
		}
		c.lines <- lineResult{line: line}
	}
}

func (c *Client) waitForTurnAnswerLocked(ctx context.Context, threadID string, turnID string) (string, error) {
	var answer strings.Builder
	for {
		line, err := c.nextLine(ctx)
		if err != nil {
			return "", fmt.Errorf("read app-server notification: %w", err)
		}

		var notification rpcNotificationEnvelope
		if err := json.Unmarshal(line, &notification); err != nil {
			return "", fmt.Errorf("decode app-server notification: %w", err)
		}
		if notification.Params.ThreadID != threadID {
			continue
		}

		switch notification.Method {
		case "item/agentMessage/delta":
			if notification.Params.TurnID == turnID {
				answer.WriteString(notification.Params.Delta)
			}
		case "item/completed":
			if notification.Params.TurnID == turnID && notification.Params.Item.Type == "agentMessage" {
				answer.Reset()
				answer.WriteString(notification.Params.Item.Text)
			}
		case "turn/completed":
			if notification.Params.Turn.ID == turnID {
				if notification.Params.Turn.Status != "completed" {
					status := notification.Params.Turn.Status
					if status == "" {
						status = "unknown"
					}
					return "", fmt.Errorf("app-server turn %s ended with status %s", turnID, status)
				}
				if answer.Len() == 0 {
					return "", fmt.Errorf("app-server returned no agent answer")
				}
				return answer.String(), nil
			}
		}
	}
}

func BuildSideQuestionPrompt(req sidequestions.Request) string {
	return prompts.SideQuestion(req.Question, req.FilePath, req.Reference, req.SelectedText, req.PlanExcerpt, req.Format)
}

type SideQuestionAsker struct {
	Client          *Client
	CWD             string
	CurrentThreadID string

	mu          sync.Mutex
	initialized bool
}

func (a *SideQuestionAsker) Ask(ctx context.Context, req sidequestions.Request) (string, error) {
	return a.askPromptInFork(ctx, req.ThreadID, BuildSideQuestionPrompt(req))
}

func (a *SideQuestionAsker) AskPrompt(ctx context.Context, prompt string) (string, error) {
	return a.askPromptInFork(ctx, a.CurrentThreadID, prompt)
}

func (a *SideQuestionAsker) askPromptInFork(ctx context.Context, threadID string, prompt string) (string, error) {
	if a.Client == nil {
		return "", fmt.Errorf("app-server client is nil")
	}
	if threadID == "" {
		return "", fmt.Errorf("app-server thread id is required")
	}
	if err := a.ensureInitialized(ctx); err != nil {
		return "", err
	}
	if _, err := a.Client.ReadThread(ctx, threadID); err != nil {
		return "", err
	}
	fork, err := a.Client.ForkThread(ctx, threadID, a.CWD)
	if err != nil {
		return "", err
	}
	return a.Client.StartTurnAndWait(ctx, fork.Thread.ID, prompt)
}

func (a *SideQuestionAsker) ensureInitialized(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return nil
	}
	if _, err := a.Client.Initialize(ctx); err != nil {
		return err
	}
	if err := a.Client.NotifyInitialized(ctx); err != nil {
		return err
	}
	a.initialized = true
	return nil
}

type rpcRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type rpcNotification struct {
	Method string `json:"method"`
}

type lineResult struct {
	line []byte
	err  error
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appServerError `json:"error"`
}

type rpcNotificationEnvelope struct {
	Method string `json:"method"`
	Params struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Delta    string `json:"delta"`
		Item     struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	} `json:"params"`
}

type appServerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ClientInfo   clientInfo   `json:"clientInfo"`
	Capabilities capabilities `json:"capabilities"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type capabilities struct {
	ExperimentalAPI           bool     `json:"experimentalApi"`
	RequestAttestation        bool     `json:"requestAttestation"`
	OptOutNotificationMethods []string `json:"optOutNotificationMethods"`
}

type readThreadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns"`
}

type forkThreadParams struct {
	ThreadID               string `json:"threadId"`
	CWD                    string `json:"cwd"`
	Ephemeral              bool   `json:"ephemeral"`
	ExcludeTurns           bool   `json:"excludeTurns"`
	ApprovalPolicy         string `json:"approvalPolicy"`
	PersistExtendedHistory bool   `json:"persistExtendedHistory"`
}

type startTurnParams struct {
	ThreadID       string      `json:"threadId"`
	ApprovalPolicy string      `json:"approvalPolicy"`
	Input          []turnInput `json:"input"`
}

type turnInput struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	TextElements []any  `json:"text_elements"`
}

func responseIDMatches(raw json.RawMessage, id int64) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, nil
	}

	var numericID int64
	if err := json.Unmarshal(raw, &numericID); err == nil {
		return numericID == id, nil
	}

	var stringID string
	if err := json.Unmarshal(raw, &stringID); err == nil {
		return stringID == fmt.Sprint(id), nil
	}

	return false, fmt.Errorf("unsupported id %s", string(raw))
}
