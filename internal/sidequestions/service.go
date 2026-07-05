package sidequestions

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrUnavailable = errors.New("side questions unavailable")

type Request struct {
	ThreadID     string `json:"threadID"`
	Question     string `json:"question"`
	FilePath     string `json:"filePath"`
	Reference    string `json:"reference"`
	SelectedText string `json:"selectedText"`
	PlanExcerpt  string `json:"planExcerpt"`
}

type AskClient interface {
	Ask(ctx context.Context, req Request) (string, error)
}

type Service struct {
	currentThreadID string
	primary         AskClient
}

func NewService(currentThreadID string, client AskClient) Service {
	return Service{currentThreadID: currentThreadID, primary: client}
}

func (s Service) Ask(ctx context.Context, req Request) (string, error) {
	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("question is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.currentThreadID == "" || s.primary == nil {
		return "", ErrUnavailable
	}
	req.ThreadID = s.currentThreadID
	return s.primary.Ask(ctx, req)
}
