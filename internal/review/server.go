package review

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	plandiff "github.com/AlhasanIQ/planmaxx/internal/diff"
	"github.com/AlhasanIQ/planmaxx/internal/digest"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
)

// The web UI is built separately into static/ (see scripts/build-web.sh).
// Listing the bundle files explicitly forces `go build` to fail loudly when
// the UI hasn't been built yet, instead of silently shipping a binary that
// serves a broken page.
//
//go:embed static/index.html
//go:embed static/assets/app.js
//go:embed static/assets/app.css
var staticFiles embed.FS

type Server struct {
	mu             sync.Mutex
	session        *session.Session
	done           chan Result
	finished       bool
	autosavePath   string
	autosaveStatus string

	sideQuestions       sidequestions.Service
	sideQuestionTimeout time.Duration
	sectionIterations   sectioniter.Service
}

type Result struct {
	Session  session.Session
	Canceled bool
	Rejected bool
}

func NewServer(s *session.Session) *Server {
	return &Server{
		session: s,
		done:    make(chan Result, 1),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/threads", s.handleCreateThread)
	mux.HandleFunc("/api/threads/", s.handleThreadAction)
	mux.HandleFunc("/api/side-answers/", s.handleSideAnswerAction)
	mux.HandleFunc("/api/side-questions", s.handleSideQuestion)
	mux.HandleFunc("/api/digest/draft", s.handleDigestDraft)
	mux.HandleFunc("/api/revisions/propose-section", s.handleProposeSection)
	mux.HandleFunc("/api/revisions/proposals/", s.handleRevisionProposalAction)
	mux.HandleFunc("/api/revisions/", s.handleRevisionAction)
	mux.HandleFunc("/api/revisions", s.handleRevisions)
	mux.HandleFunc("/api/finalize", s.handleFinalize)
	mux.HandleFunc("/api/reject", s.handleReject)
	mux.HandleFunc("/api/cancel", s.handleCancel)
	mux.Handle("/", http.FileServer(http.FS(staticSubFS())))
	return mux
}

func (s *Server) WithSideQuestions(service sidequestions.Service) *Server {
	s.sideQuestions = service
	return s
}

func (s *Server) WithSectionIterations(service sectioniter.Service) *Server {
	s.sectionIterations = service
	return s
}

func (s *Server) EnableAutosave(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autosavePath = path
	s.autosaveStatus = "active"
	return s.persistLocked()
}

func (s *Server) WithSideQuestionTimeout(timeout time.Duration) *Server {
	s.sideQuestionTimeout = timeout
	return s
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "active"
	if s.finished {
		status = "completed"
	}
	writeJSON(w, map[string]string{
		"status":    status,
		"sessionId": s.session.ID,
	})
}

func (s *Server) Wait(ctx context.Context) (Result, error) {
	select {
	case result := <-s.done:
		return result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.session)
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var digest session.Digest
	if err := decodeJSON(r.Body, &digest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid digest json")
		return
	}
	result, err := s.finalize(digest)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.done <- result
	writeJSON(w, map[string]string{"status": "finalized"})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var digest session.Digest
	if err := decodeJSON(r.Body, &digest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid digest json")
		return
	}
	result, err := s.reject(digest)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.done <- result
	writeJSON(w, map[string]string{"status": "rejected"})
}

type createThreadRequest struct {
	Anchor       session.Anchor `json:"anchor"`
	Body         string         `json:"body"`
	SelectedText string         `json:"selectedText"`
}

func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request createThreadRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid thread json")
		return
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if err := validateThreadRequest(request, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid thread json")
		return
	}
	thread := s.session.AddThreadWithSelectedText(request.Anchor, request.Body, request.SelectedText)
	if err := s.persistLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Unlock()
	writeJSON(w, thread)
}

func (s *Server) handleThreadAction(w http.ResponseWriter, r *http.Request) {
	threadID, action, ok := parseThreadActionPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch action {
	case "reply":
		s.handleReplyThread(w, r, threadID)
	case "delete":
		s.handleDeleteThread(w, threadID)
	case "move":
		s.handleMoveThread(w, r, threadID)
	case "reanchor":
		s.handleReanchorThread(w, r, threadID)
	case "edit":
		s.handleEditThread(w, r, threadID)
	case "kind":
		s.handleThreadKind(w, r, threadID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleSideAnswerAction(w http.ResponseWriter, r *http.Request) {
	sideAnswerID, action, ok := parseSideAnswerActionPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch action {
	case "promote":
		s.handlePromoteSideAnswer(w, sideAnswerID)
	case "unpromote":
		s.handleUnpromoteSideAnswer(w, sideAnswerID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleSideQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	var req sidequestions.Request
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid side-question json")
		return
	}
	thread, ok := s.threadByID(req.ThreadID)
	if !ok {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	req = s.withSideQuestionContext(req, thread)
	ctx := r.Context()
	if s.sideQuestionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.sideQuestionTimeout)
		defer cancel()
	}
	answer, err := s.sideQuestions.Ask(ctx, req)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	sideAnswer := s.session.AddSideAnswer(req.ThreadID, req.Question, answer)
	if err := s.persistLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Unlock()
	writeJSON(w, sideAnswer)
}

func (s *Server) handleDigestDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	draft := digest.DraftFromState(*s.session)
	s.mu.Unlock()
	writeJSON(w, draft)
}

type replyThreadRequest struct {
	Body string `json:"body"`
}

type editThreadRequest struct {
	Anchor       session.Anchor `json:"anchor"`
	Body         string         `json:"body"`
	SelectedText string         `json:"selectedText"`
}

type threadKindRequest struct {
	Kind string `json:"kind"`
}

type proposeSectionRequest struct {
	ThreadID    string         `json:"threadId"`
	Anchor      session.Anchor `json:"anchor"`
	Instruction string         `json:"instruction"`
}

type responseError struct {
	status  int
	message string
}

func (e responseError) Error() string {
	return e.message
}

func (s *Server) handleReplyThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	var request replyThreadRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid reply json")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if request.Body == "" {
		writeError(w, http.StatusBadRequest, "invalid reply json")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if !s.session.AddReply(threadID, request.Body) {
			return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
		}
		return statusResponse("replied"), nil
	})
}

func (s *Server) handleDeleteThread(w http.ResponseWriter, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if !s.session.DeleteThread(threadID) {
			return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
		}
		return statusResponse("deleted"), nil
	})
}

func (s *Server) handleMoveThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	var position session.Position
	if err := decodeJSON(r.Body, &position); err != nil {
		writeError(w, http.StatusBadRequest, "invalid position json")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if !s.session.MoveThread(threadID, position) {
			return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
		}
		return statusResponse("moved"), nil
	})
}

func (s *Server) handleReanchorThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	var anchor session.Anchor
	if err := decodeJSON(r.Body, &anchor); err != nil {
		writeError(w, http.StatusBadRequest, "invalid anchor json")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if err := validateAnchor(anchor, s.session.Plan); err != nil {
			return nil, responseError{status: http.StatusBadRequest, message: "invalid anchor json"}
		}
		if !s.session.ReanchorThread(threadID, anchor) {
			return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
		}
		return statusResponse("reanchored"), nil
	})
}

func (s *Server) handleEditThread(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	var request editThreadRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid thread edit json")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if request.Body == "" {
		writeError(w, http.StatusBadRequest, "invalid thread edit json")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if err := validateAnchor(request.Anchor, s.session.Plan); err != nil {
			return nil, responseError{status: http.StatusBadRequest, message: "invalid thread edit json"}
		}
		if !s.session.EditThreadWithSelectedText(threadID, request.Anchor, request.Body, request.SelectedText) {
			return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
		}
		return statusResponse("edited"), nil
	})
}

func (s *Server) handleThreadKind(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	var request threadKindRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid thread kind json")
		return
	}
	request.Kind = strings.TrimSpace(request.Kind)

	s.mutateSession(w, func() (any, error) {
		if !s.session.SetThreadKind(threadID, request.Kind) {
			if !threadExists(*s.session, threadID) {
				return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
			}
			return nil, responseError{status: http.StatusBadRequest, message: "invalid thread kind json"}
		}
		return statusResponse("updated"), nil
	})
}

func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{
		"currentRevisionId": s.session.CurrentRevisionID,
		"revisions":         s.session.Revisions,
		"pendingProposal":   s.session.PendingProposal,
	})
}

func (s *Server) handleRevisionAction(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseRevisionDiffPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	fromRevision, fromOK := findRevision(*s.session, from)
	toRevision, toOK := findRevision(*s.session, to)
	s.mu.Unlock()
	if !fromOK || !toOK {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	writeJSON(w, map[string]any{
		"from":  from,
		"to":    to,
		"lines": plandiff.Lines(fromRevision.Plan, toRevision.Plan),
	})
}

func (s *Server) handleProposeSection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	var request proposeSectionRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	request.Instruction = strings.TrimSpace(request.Instruction)
	if request.Instruction == "" {
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}

	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	if err := validateAnchor(request.Anchor, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	baseRevisionID := s.session.CurrentRevisionID
	basePendingProposalID := pendingProposalID(s.session.PendingProposal)
	req := s.sectionIterationRequestLocked(request)
	s.mu.Unlock()

	ctx := r.Context()
	if s.sideQuestionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.sideQuestionTimeout)
		defer cancel()
	}
	input, err := s.sectionIterations.Propose(ctx, req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, sectioniter.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	if s.session.CurrentRevisionID != baseRevisionID || pendingProposalID(s.session.PendingProposal) != basePendingProposalID {
		writeError(w, http.StatusConflict, "plan changed while section iteration was running")
		return
	}
	proposal := s.session.CreateSectionProposal(input)
	if err := s.persistLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, proposal)
}

func (s *Server) handleRevisionProposalAction(w http.ResponseWriter, r *http.Request) {
	proposalID, action, ok := parseRevisionProposalActionPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	switch action {
	case "apply":
		s.mutateSession(w, func() (any, error) {
			revision, ok := s.session.ApplyProposal(proposalID)
			if !ok {
				return nil, responseError{status: http.StatusNotFound, message: "proposal not found"}
			}
			return revision, nil
		})
	case "discard":
		s.mutateSession(w, func() (any, error) {
			if !s.session.DiscardProposal(proposalID) {
				return nil, responseError{status: http.StatusNotFound, message: "proposal not found"}
			}
			return statusResponse("discarded"), nil
		})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handlePromoteSideAnswer(w http.ResponseWriter, sideAnswerID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if !s.session.PromoteSideAnswer(sideAnswerID) {
			return nil, responseError{status: http.StatusNotFound, message: "side answer not found"}
		}
		return statusResponse("promoted"), nil
	})
}

func (s *Server) handleUnpromoteSideAnswer(w http.ResponseWriter, sideAnswerID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if !s.session.UnpromoteSideAnswer(sideAnswerID) {
			return nil, responseError{status: http.StatusNotFound, message: "side answer not found"}
		}
		return statusResponse("unpromoted"), nil
	})
}

func (s *Server) isFinished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished
}

func (s *Server) mutateSession(w http.ResponseWriter, mutate func() (any, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	response, err := mutate()
	if err != nil {
		var httpErr responseError
		if errors.As(err, &httpErr) {
			writeError(w, httpErr.status, httpErr.message)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.persistLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, response)
}

func statusResponse(status string) map[string]string {
	return map[string]string{"status": status}
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := s.cancel()
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.done <- result
	writeJSON(w, map[string]string{"status": "canceled"})
}

func (s *Server) finalize(digest session.Digest) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return Result{}, errors.New("review already completed")
	}
	s.session.SetDigest(digest)
	s.finished = true
	s.autosaveStatus = "finalized"
	if err := s.persistLocked(); err != nil {
		return Result{}, err
	}
	return Result{Session: *s.session}, nil
}

func (s *Server) cancel() (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return Result{}, errors.New("review already completed")
	}
	s.finished = true
	s.autosaveStatus = "canceled"
	if err := s.persistLocked(); err != nil {
		return Result{}, err
	}
	return Result{Session: *s.session, Canceled: true}, nil
}

func (s *Server) reject(digest session.Digest) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return Result{}, errors.New("review already completed")
	}
	s.session.SetDigest(digest)
	s.finished = true
	s.autosaveStatus = "rejected"
	if err := s.persistLocked(); err != nil {
		return Result{}, err
	}
	return Result{Session: *s.session, Rejected: true}, nil
}

func (s *Server) persistLocked() error {
	if s.autosaveStatus == "" {
		s.autosaveStatus = "active"
	}
	return writeAutosave(s.autosavePath, s.autosaveStatus, *s.session)
}

func validateThreadRequest(request createThreadRequest, plan string) error {
	if request.Body == "" {
		return errors.New("thread body is required")
	}
	return validateAnchor(request.Anchor, plan)
}

func validateAnchor(anchor session.Anchor, plan string) error {
	if anchor.StartLine <= 0 || anchor.EndLine <= 0 {
		return errors.New("thread anchor lines must be positive")
	}
	if anchor.EndLine < anchor.StartLine {
		return errors.New("thread anchor end line must be after start line")
	}
	lines := strings.Split(plan, "\n")
	lineCount := len(lines)
	if anchor.StartLine > lineCount || anchor.EndLine > lineCount {
		return errors.New("thread anchor is outside plan")
	}
	if anchor.StartChar == 0 && anchor.EndChar == 0 {
		return nil
	}
	if anchor.StartChar < 0 || anchor.EndChar < 0 {
		return errors.New("thread anchor character offsets must be non-negative")
	}
	startLineLength := utf16Length(lines[anchor.StartLine-1])
	endLineLength := utf16Length(lines[anchor.EndLine-1])
	if anchor.StartChar > startLineLength || anchor.EndChar > endLineLength {
		return errors.New("thread anchor character offset is outside plan")
	}
	if anchor.StartLine == anchor.EndLine && anchor.EndChar <= anchor.StartChar {
		return errors.New("thread anchor end character must be after start character")
	}
	return nil
}

func utf16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func parseThreadActionPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/api/threads/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseSideAnswerActionPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/api/side-answers/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseRevisionProposalActionPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/api/revisions/proposals/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseRevisionDiffPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/api/revisions/")
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "diff" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func (s *Server) hasThread(threadID string) bool {
	_, ok := s.threadByID(threadID)
	return ok
}

func (s *Server) threadByID(threadID string) (session.Thread, bool) {
	if threadID == "" {
		return session.Thread{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, thread := range s.session.Threads {
		if thread.ID == threadID {
			return thread, true
		}
	}
	return session.Thread{}, false
}

func (s *Server) sectionIterationRequestLocked(request proposeSectionRequest) sectioniter.Request {
	basePlan := s.session.Plan
	replacementAnchor := request.Anchor
	selectedSection, _ := sectioniter.SectionForAnchor(basePlan, request.Anchor)
	if s.session.PendingProposal != nil && anchorsEqual(s.session.PendingProposal.Anchor, request.Anchor) {
		basePlan = s.session.PendingProposal.ProposedPlan
		selectedSection = s.session.PendingProposal.ProposedSection
		replacementAnchor = s.session.PendingProposal.ReplacementAnchor
		if replacementAnchor.StartLine == 0 {
			replacementAnchor = pendingProposalReplacementAnchor(request.Anchor, selectedSection)
		}
	}
	includedThreadIDs := includedThreadIDs(*s.session, request.ThreadID, request.Anchor)
	return sectioniter.Request{
		RevisionID:          s.session.CurrentRevisionID,
		ThreadID:            request.ThreadID,
		Plan:                basePlan,
		FilePath:            s.session.PlanPath,
		Reference:           anchorReference(s.session.PlanPath, request.Anchor),
		Anchor:              request.Anchor,
		ReplacementAnchor:   replacementAnchor,
		SelectedSection:     selectedSection,
		PlanExcerpt:         planLineExcerpt(basePlan, max(1, request.Anchor.StartLine-2), request.Anchor.EndLine+2),
		ReviewerInstruction: request.Instruction,
		ReviewerDecisions:   reviewerDecisionsForAnchor(*s.session, request.Anchor),
		PromotedSideAnswers: promotedSideAnswersForAnchor(*s.session, request.Anchor),
		IncludedThreadIDs:   includedThreadIDs,
	}
}

func (s *Server) withSideQuestionContext(req sidequestions.Request, thread session.Thread) sidequestions.Request {
	s.mu.Lock()
	planPath := s.session.PlanPath
	plan := s.session.Plan
	s.mu.Unlock()

	if req.FilePath == "" {
		req.FilePath = planPath
	}
	if req.Reference == "" {
		req.Reference = anchorReference(req.FilePath, thread.Anchor)
	}
	if req.SelectedText == "" {
		req.SelectedText = thread.SelectedText
	}
	if req.PlanExcerpt == "" {
		req.PlanExcerpt = planLineExcerpt(plan, thread.Anchor.StartLine, thread.Anchor.EndLine)
	}
	if req.SelectedText == "" {
		req.SelectedText = req.PlanExcerpt
	}
	return req
}

func threadExists(s session.Session, threadID string) bool {
	for _, thread := range s.Threads {
		if thread.ID == threadID {
			return true
		}
	}
	return false
}

func findRevision(s session.Session, revisionID string) (session.Revision, bool) {
	for _, revision := range s.Revisions {
		if revision.ID == revisionID {
			return revision, true
		}
	}
	return session.Revision{}, false
}

func includedThreadIDs(s session.Session, requestedThreadID string, anchor session.Anchor) []string {
	seen := map[string]bool{}
	var out []string
	if requestedThreadID != "" && threadExists(s, requestedThreadID) {
		seen[requestedThreadID] = true
		out = append(out, requestedThreadID)
	}
	for _, thread := range s.Threads {
		if thread.Kind != session.ThreadKindDecision || thread.Status != session.ThreadStatusOpen {
			continue
		}
		if !anchorsOverlap(thread.Anchor, anchor) || seen[thread.ID] {
			continue
		}
		seen[thread.ID] = true
		out = append(out, thread.ID)
	}
	return out
}

func reviewerDecisionsForAnchor(s session.Session, anchor session.Anchor) []string {
	var out []string
	for _, thread := range s.Threads {
		if thread.Kind != session.ThreadKindDecision || thread.Status != session.ThreadStatusOpen {
			continue
		}
		if !anchorsOverlap(thread.Anchor, anchor) {
			continue
		}
		for _, message := range thread.Messages {
			out = append(out, message.Body)
		}
	}
	return out
}

func promotedSideAnswersForAnchor(s session.Session, anchor session.Anchor) []string {
	var out []string
	for _, answer := range s.SideAnswers {
		if !answer.Promoted {
			continue
		}
		thread, ok := findThread(s, answer.ThreadID)
		if !ok || !anchorsOverlap(thread.Anchor, anchor) {
			continue
		}
		out = append(out, fmt.Sprintf("Question:\n%s\nAnswer:\n%s", answer.Question, answer.Answer))
	}
	return out
}

func findThread(s session.Session, threadID string) (session.Thread, bool) {
	for _, thread := range s.Threads {
		if thread.ID == threadID {
			return thread, true
		}
	}
	return session.Thread{}, false
}

func anchorsOverlap(a session.Anchor, b session.Anchor) bool {
	return a.StartLine <= b.EndLine && b.StartLine <= a.EndLine
}

func anchorsEqual(a session.Anchor, b session.Anchor) bool {
	return a.StartLine == b.StartLine &&
		a.StartChar == b.StartChar &&
		a.EndLine == b.EndLine &&
		a.EndChar == b.EndChar
}

func pendingProposalID(proposal *session.SectionProposal) string {
	if proposal == nil {
		return ""
	}
	return proposal.ID
}

func pendingProposalReplacementAnchor(anchor session.Anchor, selectedSection string) session.Anchor {
	lines := lineCount(selectedSection)
	if lines <= 1 && (anchor.StartChar != 0 || anchor.EndChar != 0) {
		return anchor
	}
	return session.Anchor{
		StartLine: anchor.StartLine,
		EndLine:   anchor.StartLine + max(lines, 1) - 1,
	}
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func anchorReference(filePath string, anchor session.Anchor) string {
	if filePath == "" {
		filePath = "(unknown file)"
	}
	if anchor.StartChar == 0 && anchor.EndChar == 0 {
		if anchor.StartLine == anchor.EndLine {
			return fmt.Sprintf("%s:%d", filePath, anchor.StartLine)
		}
		return fmt.Sprintf("%s:%d-%d", filePath, anchor.StartLine, anchor.EndLine)
	}
	return fmt.Sprintf(
		"%s:%d:%d-%d:%d",
		filePath,
		anchor.StartLine,
		anchor.StartChar+1,
		anchor.EndLine,
		anchor.EndChar+1,
	)
}

func planLineExcerpt(plan string, startLine int, endLine int) string {
	lines := strings.Split(plan, "\n")
	start := max(1, startLine)
	end := max(start, endLine)
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func decodeJSON(r io.Reader, v any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple json values")
		}
		return err
	}
	return nil
}

func staticSubFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
