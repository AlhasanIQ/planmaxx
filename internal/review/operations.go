package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const (
	iterationOperationQueued    = "queued"
	iterationOperationRunning   = "running"
	iterationOperationCanceling = "canceling"
	iterationOperationSucceeded = "succeeded"
	iterationOperationFailed    = "failed"
	iterationOperationCanceled  = "canceled"
)

type iterationOperationView struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"startedAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	BaseRevisionID string     `json:"baseRevisionId"`
	ProposalID     string     `json:"proposalId,omitempty"`
	ResultKind     string     `json:"resultKind,omitempty"`
	ThreadID       string     `json:"threadId,omitempty"`
	Error          string     `json:"error,omitempty"`
}

type iterationOperation struct {
	iterationOperationView
	inputKey string
	cancel   context.CancelFunc
}

func (s *Server) handleStartReviewIteration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var reviewDigest session.Digest
	if err := decodeJSON(r.Body, &reviewDigest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review iteration json")
		return
	}
	if !digestHasContent(reviewDigest) {
		writeError(w, http.StatusBadRequest, "review iteration requires feedback")
		return
	}
	s.startIteration(
		w,
		"review",
		iterationInputKey("review", reviewDigest),
		proposeSectionRequest{},
		proposalLifecycle{kind: session.ProposalKindReview, reviewDigest: &reviewDigest},
	)
}

func (s *Server) handleStartSectionIteration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request proposeSectionRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	s.startIteration(w, "section", iterationInputKey("section", request), request, proposalLifecycle{})
}

func (s *Server) startIteration(
	w http.ResponseWriter,
	kind string,
	inputKey string,
	request proposeSectionRequest,
	lifecycle proposalLifecycle,
) {
	s.mu.Lock()
	if s.iterationRunningLocked() {
		if s.iterationOperation.inputKey == inputKey {
			operation := snapshotIterationOperation(s.iterationOperation)
			s.mu.Unlock()
			writeJSONStatus(w, http.StatusAccepted, operation)
			return
		}
		active := snapshotIterationOperation(s.iterationOperation)
		s.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf("iteration %s is already running", active.ID))
		return
	}
	prepared, err := s.prepareProposalLocked(request, lifecycle)
	if err != nil {
		s.mu.Unlock()
		writeProposalError(w, err)
		return
	}

	s.iterationSequence++
	operationID := fmt.Sprintf("operation-%d", s.iterationSequence)
	ctx := context.Background()
	var cancel context.CancelFunc
	if s.sideQuestionTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.sideQuestionTimeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	operation := &iterationOperation{
		iterationOperationView: iterationOperationView{
			ID:             operationID,
			Kind:           kind,
			Status:         iterationOperationQueued,
			StartedAt:      time.Now().UTC(),
			BaseRevisionID: prepared.baseRevisionID,
		},
		inputKey: inputKey,
		cancel:   cancel,
	}
	s.iterationOperation = operation
	response := snapshotIterationOperation(operation)
	s.mu.Unlock()

	go s.runIteration(ctx, operationID, prepared)
	writeJSONStatus(w, http.StatusAccepted, response)
}

func (s *Server) runIteration(ctx context.Context, operationID string, prepared preparedProposal) {
	s.mu.Lock()
	if s.iterationOperation == nil || s.iterationOperation.ID != operationID {
		s.mu.Unlock()
		return
	}
	s.iterationOperation.Status = iterationOperationRunning
	s.mu.Unlock()

	result, err := s.executePreparedProposal(ctx, prepared)
	completedAt := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iterationOperation == nil || s.iterationOperation.ID != operationID {
		return
	}
	operation := s.iterationOperation
	if operation.cancel != nil {
		operation.cancel()
	}
	operation.cancel = nil
	operation.CompletedAt = &completedAt
	if operation.Status == iterationOperationCanceling || errors.Is(err, context.Canceled) {
		operation.Status = iterationOperationCanceled
		operation.Error = ""
		return
	}
	if err != nil {
		operation.Status = iterationOperationFailed
		operation.Error = err.Error()
		return
	}
	operation.Status = iterationOperationSucceeded
	if result.Proposal != nil {
		operation.ResultKind = "proposal"
		operation.ProposalID = result.Proposal.ID
	} else {
		operation.ResultKind = "thread_reply"
		operation.ThreadID = result.ThreadID
	}
	operation.Error = ""
}

func (s *Server) handleOperationAction(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/operations/"), "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "operation not found")
		return
	}
	parts := strings.Split(path, "/")
	operationID := parts[0]

	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		s.mu.Lock()
		operation := snapshotIterationOperation(s.iterationOperation)
		s.mu.Unlock()
		if operation == nil || operation.ID != operationID {
			writeError(w, http.StatusNotFound, "operation not found")
			return
		}
		writeJSON(w, operation)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "cancel":
		s.mu.Lock()
		if s.iterationOperation == nil || s.iterationOperation.ID != operationID {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "operation not found")
			return
		}
		if s.iterationRunningLocked() {
			s.cancelIterationLocked()
		}
		operation := snapshotIterationOperation(s.iterationOperation)
		s.mu.Unlock()
		writeJSON(w, operation)
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, "operation not found")
	}
}

func (s *Server) iterationRunningLocked() bool {
	if s.iterationOperation == nil {
		return false
	}
	switch s.iterationOperation.Status {
	case iterationOperationQueued, iterationOperationRunning, iterationOperationCanceling:
		return true
	default:
		return false
	}
}

func (s *Server) cancelIterationLocked() {
	if !s.iterationRunningLocked() {
		return
	}
	s.iterationOperation.Status = iterationOperationCanceling
	if s.iterationOperation.cancel != nil {
		s.iterationOperation.cancel()
	}
}

func (s *Server) cancelActiveIteration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelIterationLocked()
}

func snapshotIterationOperation(operation *iterationOperation) *iterationOperationView {
	if operation == nil {
		return nil
	}
	snapshot := operation.iterationOperationView
	return &snapshot
}

func cloneIterationOperation(operation *iterationOperation) *iterationOperation {
	if operation == nil {
		return nil
	}
	clone := *operation
	return &clone
}

func (s *Server) clearTerminalIterationLocked() {
	if s.iterationOperation != nil && !s.iterationRunningLocked() {
		s.iterationOperation = nil
	}
}

func iterationInputKey(kind string, request any) string {
	encoded, _ := json.Marshal(request)
	sum := sha256.Sum256(append([]byte(kind+"\x00"), encoded...))
	return hex.EncodeToString(sum[:])
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
