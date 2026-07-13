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
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	plandiff "github.com/AlhasanIQ/planmaxx/internal/diff"
	"github.com/AlhasanIQ/planmaxx/internal/digest"
	"github.com/AlhasanIQ/planmaxx/internal/reviewxml"
	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/AlhasanIQ/planmaxx/internal/sectioniter"
	"github.com/AlhasanIQ/planmaxx/internal/session"
	"github.com/AlhasanIQ/planmaxx/internal/sidequestions"
	"github.com/go-git/go-git/v5/plumbing"
)

// The web UI is built separately into static/ (see scripts/build-web.sh).
// Embed its complete generated directory. Referencing static makes `go build`
// fail loudly until the UI has been built.
//
//go:embed static
var staticFiles embed.FS

type Server struct {
	mu                 sync.Mutex
	session            *session.Session
	done               chan Result
	finished           bool
	autosavePath       string
	autosaveStatus     string
	autosaveGeneration uint64
	autosaveDocument   Document
	autosaveFallback   string

	sideQuestions       sidequestions.Service
	sideQuestionTimeout time.Duration
	sectionIterations   sectioniter.Service
	revisionStore       *revisions.Store
	revisionPlanID      string
	comparisonCache     sync.Map
}

type revisionComparison struct {
	From     string                     `json:"from"`
	To       string                     `json:"to"`
	Lines    []plandiff.Line            `json:"lines"`
	Feedback []session.RevisionFeedback `json:"feedback"`
}

type Result struct {
	Session  session.Session
	Canceled bool
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

func (s *Server) WithRevisionStore(store *revisions.Store, planID string) *Server {
	s.revisionStore = store
	s.revisionPlanID = planID
	return s
}

func (s *Server) EnableAutosave(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autosavePath = path
	s.autosaveStatus = "active"
	requestedDocument := s.autosaveDocument
	if s.autosaveDocument.SourceHash == "" && s.session.PlanPath != "" {
		s.autosaveDocument = NewDocument(s.session.PlanPath, s.session.Plan)
	}
	if err := s.recoverRevisionJournalLocked(); err != nil {
		return err
	}
	if saved, ok, err := LoadAutosave(path); err != nil {
		return fmt.Errorf("load existing autosave: %w", err)
	} else if ok {
		s.session = &saved.Session
		s.autosaveStatus = saved.Status
		s.autosaveDocument = saved.Document
		s.autosaveGeneration = saved.Generation
		if requestedDocument.SourceHash != "" && !saved.Document.SourceMatches(requestedDocument.SourceText) {
			s.session.ReconcileExternalPlan(saved.Document.SourceText, requestedDocument.SourceText)
			s.autosaveDocument = requestedDocument
			return s.persistLocked()
		}
		if err := s.hydrateRevisionBodiesLocked(); err != nil {
			return err
		}
		if s.revisionStore != nil && hasUnpersistedRevisions(*s.session) {
			return s.persistLocked()
		}
		return nil
	}
	if err := s.hydrateRevisionBodiesLocked(); err != nil {
		return err
	}
	return s.persistLocked()
}

func hasUnpersistedRevisions(s session.Session) bool {
	for _, revision := range s.Revisions {
		if revision.CommitID == "" {
			return true
		}
	}
	return false
}

// WithAutosaveDocument identifies the source file independently from the
// mutable review draft. This prevents an accepted in-app revision from being
// mistaken for an external file change after reopening the review.
func (s *Server) WithAutosaveDocument(document Document) *Server {
	s.autosaveDocument = document
	return s
}

// WithAutosaveFallback keeps review state durable if the preferred sidecar
// becomes unwritable after a review has already started.
func (s *Server) WithAutosaveFallback(path string) *Server {
	s.autosaveFallback = path
	return s
}

func (s *Server) AutosavePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autosavePath
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
	if err := s.reloadAutosaveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, sessionForClient(*s.session))
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
	if err := s.syncExternalSourceLocked(); err != nil {
		s.mu.Unlock()
		writeSourceSyncError(w, err)
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if err := validateThreadRequest(request, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid thread json")
		return
	}
	before := cloneSession(*s.session)
	thread := s.session.AddThreadWithSelectedText(request.Anchor, request.Body, request.SelectedText)
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			s.mu.Unlock()
			writePersistenceError(w, err)
			return
		}
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
	if err := s.syncExternalSourceLocked(); err != nil {
		s.mu.Unlock()
		writeSourceSyncError(w, err)
		return
	}
	before := cloneSession(*s.session)
	sideAnswer := s.session.AddSideAnswer(req.ThreadID, req.Question, answer)
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			s.mu.Unlock()
			writePersistenceError(w, err)
			return
		}
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
	state := sessionForClient(*s.session)
	writeJSON(w, map[string]any{
		"currentRevisionId": s.session.CurrentRevisionID,
		"revisions":         state.Revisions,
		"pendingProposal":   s.session.PendingProposal,
	})
}

func (s *Server) handleRevisionAction(w http.ResponseWriter, r *http.Request) {
	if revisionID, ok := parseRevisionRestorePath(r.URL.Path); ok {
		s.handleRevisionRestore(w, r, revisionID)
		return
	}
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
	feedback := revisionFeedbackBetween(*s.session, from, to)
	store := s.revisionStore
	s.mu.Unlock()
	if !fromOK || !toOK {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	cacheKey, cacheable := revisionComparisonCacheKey(fromRevision, toRevision)
	if cacheable {
		if cached, ok := s.comparisonCache.Load(cacheKey); ok {
			writeJSON(w, cached)
			return
		}
	}
	if store != nil && fromRevision.CommitID != "" && toRevision.CommitID != "" {
		fromPlan, err := store.Read(plumbing.NewHash(fromRevision.CommitID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		toPlan, err := store.Read(plumbing.NewHash(toRevision.CommitID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		fromRevision.Plan, toRevision.Plan = fromPlan, toPlan
	}
	comparison := revisionComparison{
		From:     from,
		To:       to,
		Lines:    plandiff.Lines(fromRevision.Plan, toRevision.Plan),
		Feedback: feedback,
	}
	if cacheable {
		actual, _ := s.comparisonCache.LoadOrStore(cacheKey, comparison)
		comparison = actual.(revisionComparison)
	}
	writeJSON(w, comparison)
}

func (s *Server) handleRevisionRestore(w http.ResponseWriter, r *http.Request, revisionID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mutateSession(w, func() (any, error) {
		revision, ok := findRevision(*s.session, revisionID)
		if !ok {
			return nil, responseError{status: http.StatusNotFound, message: "revision not found"}
		}
		content := revision.Plan
		if s.revisionStore != nil && revision.CommitID != "" {
			var err error
			content, err = s.revisionStore.Read(plumbing.NewHash(revision.CommitID))
			if err != nil {
				return nil, err
			}
		}
		return s.session.AddTurnRevision(content, "Restored revision "+revisionID), nil
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
	if err := s.syncExternalSourceLocked(); err != nil {
		s.mu.Unlock()
		writeSourceSyncError(w, err)
		return
	}
	if err := validateAnchor(request.Anchor, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	if s.session.PendingProposal != nil && s.session.PendingProposal.Obsolete {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "discard the stale section proposal before iterating again")
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
	if err := s.syncExternalSourceLocked(); err != nil {
		writeSourceSyncError(w, err)
		return
	}
	if s.session.CurrentRevisionID != baseRevisionID || pendingProposalID(s.session.PendingProposal) != basePendingProposalID {
		writeError(w, http.StatusConflict, "plan changed while section iteration was running")
		return
	}
	before := cloneSession(*s.session)
	proposal := s.session.CreateSectionProposal(input)
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			writePersistenceError(w, err)
			return
		}
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
			if proposal := s.session.PendingProposal; proposal != nil && proposal.ID == proposalID && (proposal.Obsolete || proposal.ParentID != s.session.CurrentRevisionID) {
				return nil, responseError{status: http.StatusConflict, message: "proposal is stale after a source-file change; discard it and iterate again"}
			}
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
	if err := s.syncExternalSourceLocked(); err != nil {
		writeSourceSyncError(w, err)
		return
	}
	before := cloneSession(*s.session)
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
		if !autosaveWasCommitted(err) {
			*s.session = before
			writePersistenceError(w, err)
			return
		}
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
	if err := s.syncExternalSourceLocked(); err != nil {
		return Result{}, err
	}
	before := cloneSession(*s.session)
	previousStatus := s.autosaveStatus
	s.session.SetDigest(digest)
	s.finished = true
	s.autosaveStatus = "finalized"
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			s.finished = false
			s.autosaveStatus = previousStatus
			return Result{}, err
		}
	}
	return Result{Session: *s.session}, nil
}

func (s *Server) cancel() (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return Result{}, errors.New("review already completed")
	}
	previousStatus := s.autosaveStatus
	s.finished = true
	s.autosaveStatus = "canceled"
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			s.finished = false
			s.autosaveStatus = previousStatus
			return Result{}, err
		}
	}
	return Result{Session: *s.session, Canceled: true}, nil
}

func (s *Server) persistLocked() error {
	if s.revisionStore != nil && s.revisionPlanID != "" {
		return s.revisionStore.WithPlanTransaction(s.revisionPlanID, s.persistTransactionLocked)
	}
	return s.persistTransactionLocked()
}

func (s *Server) persistTransactionLocked() error {
	if err := s.syncRevisionStoreLocked(); err != nil {
		return err
	}
	if s.autosaveStatus == "" {
		s.autosaveStatus = "active"
	}
	savedSession := compactRevisionBodies(*s.session)
	journaled := s.revisionStore != nil && s.autosavePath != ""
	if journaled {
		if err := writeRevisionJournal(s.autosavePath, revisionJournal{ExpectedGeneration: s.autosaveGeneration, Status: s.autosaveStatus, Document: s.autosaveDocument, Session: savedSession}); err != nil {
			return err
		}
	}
	nextGeneration, err := writeAutosave(s.autosavePath, s.autosaveStatus, savedSession, s.autosaveDocument, s.autosaveGeneration)
	if nextGeneration != s.autosaveGeneration {
		s.autosaveGeneration = nextGeneration
	}
	if err == nil || autosaveWasCommitted(err) {
		if journaled {
			if clearErr := clearRevisionJournal(s.autosavePath); clearErr != nil {
				return clearErr
			}
		}
		return err
	}
	if errors.Is(err, ErrAutosaveConflict) || s.autosaveFallback == "" || s.autosaveFallback == s.autosavePath {
		if journaled {
			_ = clearRevisionJournal(s.autosavePath)
		}
		return err
	}
	if nextGeneration, fallbackErr := writeAutosave(s.autosaveFallback, s.autosaveStatus, savedSession, s.autosaveDocument, s.autosaveGeneration); fallbackErr != nil {
		return fmt.Errorf("write autosave: %w; write fallback: %v", err, fallbackErr)
	} else {
		s.autosaveGeneration = nextGeneration
	}
	if journaled {
		if clearErr := clearRevisionJournal(s.autosavePath); clearErr != nil {
			return clearErr
		}
	}
	s.autosavePath = s.autosaveFallback
	return nil
}

func (s *Server) recoverRevisionJournalLocked() error {
	if s.revisionStore != nil && s.revisionPlanID != "" {
		return s.revisionStore.WithPlanTransaction(s.revisionPlanID, s.recoverRevisionJournalTransactionLocked)
	}
	return s.recoverRevisionJournalTransactionLocked()
}

func (s *Server) recoverRevisionJournalTransactionLocked() error {
	if s.revisionStore == nil || s.autosavePath == "" {
		return nil
	}
	journal, ok, err := loadRevisionJournal(s.autosavePath)
	if err != nil || !ok {
		return err
	}
	saved, exists, err := LoadAutosave(s.autosavePath)
	if err != nil {
		return err
	}
	if exists && saved.Generation > journal.ExpectedGeneration {
		return clearRevisionJournal(s.autosavePath)
	}
	if exists && saved.Generation != journal.ExpectedGeneration {
		return fmt.Errorf("recover revision journal: %w (expected generation %d, found %d)", ErrAutosaveConflict, journal.ExpectedGeneration, saved.Generation)
	}
	if _, err := writeAutosave(s.autosavePath, journal.Status, journal.Session, journal.Document, journal.ExpectedGeneration); err != nil && !autosaveWasCommitted(err) {
		return fmt.Errorf("recover revision journal: %w", err)
	}
	return clearRevisionJournal(s.autosavePath)
}

func compactRevisionBodies(source session.Session) session.Session {
	copy := cloneSession(source)
	for i := range copy.Revisions {
		if copy.Revisions[i].CommitID != "" {
			copy.Revisions[i].Plan = ""
		}
	}
	return copy
}

// sessionForClient keeps normal review refreshes small. The working plan is
// already exposed as Session.Plan; historical bodies are fetched only through
// the comparison endpoint when the reviewer asks for them.
func sessionForClient(source session.Session) session.Session {
	copy := source
	copy.Revisions = append([]session.Revision(nil), source.Revisions...)
	for i := range copy.Revisions {
		copy.Revisions[i].Plan = ""
	}
	return copy
}

func revisionComparisonCacheKey(from, to session.Revision) (string, bool) {
	if from.CommitID == "" || to.CommitID == "" {
		return "", false
	}
	return from.ID + ":" + from.CommitID + ":" + to.ID + ":" + to.CommitID, true
}

func (s *Server) hydrateRevisionBodiesLocked() error {
	if s.revisionStore == nil {
		return nil
	}
	for i := range s.session.Revisions {
		revision := &s.session.Revisions[i]
		if revision.ID != s.session.CurrentRevisionID || revision.Plan != "" || revision.CommitID == "" {
			continue
		}
		content, err := s.revisionStore.Read(plumbing.NewHash(revision.CommitID))
		if err != nil {
			return fmt.Errorf("load revision %s from git store: %w", revision.ID, err)
		}
		revision.Plan = content
		s.session.Plan = content
	}
	return nil
}

func (s *Server) syncRevisionStoreLocked() error {
	if s.revisionStore == nil || s.revisionPlanID == "" {
		return nil
	}
	commits := map[string]string{}
	for _, revision := range s.session.Revisions {
		commits[revision.ID] = revision.CommitID
	}
	for i := range s.session.Revisions {
		revision := &s.session.Revisions[i]
		if revision.CommitID != "" {
			continue
		}
		parent := plumbing.ZeroHash
		if revision.ParentID != "" {
			parentID := commits[revision.ParentID]
			if parentID == "" {
				return fmt.Errorf("revision %s has no persisted parent commit", revision.ID)
			}
			parent = plumbing.NewHash(parentID)
		}
		hash, err := s.revisionStore.Commit(s.revisionPlanID, parent, revision.Plan, revision.Summary)
		if err != nil {
			return fmt.Errorf("persist revision %s in git store: %w", revision.ID, err)
		}
		revision.CommitID = hash.String()
		commits[revision.ID] = revision.CommitID
	}
	return nil
}

func (s *Server) reloadAutosaveLocked() error {
	if s.autosavePath == "" {
		return nil
	}
	saved, ok, err := LoadAutosave(s.autosavePath)
	if err != nil || !ok || saved.Generation <= s.autosaveGeneration {
		return err
	}
	s.session = &saved.Session
	s.autosaveStatus = saved.Status
	s.autosaveDocument = saved.Document
	s.autosaveGeneration = saved.Generation
	return nil
}

func cloneSession(source session.Session) session.Session {
	data, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("clone review session: %v", err))
	}
	var cloned session.Session
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(fmt.Sprintf("decode review session clone: %v", err))
	}
	cloned.RestoreCounters()
	return cloned
}

// syncExternalSourceLocked detects a plan edit made by an editor or another
// agent while this review is open. Session.Plan is deliberately not compared:
// applying a proposal changes the working review draft, not the source file.
func (s *Server) syncExternalSourceLocked() error {
	if s.autosaveDocument.CanonicalPath == "" {
		return nil
	}
	source, err := os.ReadFile(s.autosaveDocument.CanonicalPath)
	if err != nil {
		return fmt.Errorf("read source plan: %w", err)
	}
	if s.autosaveDocument.SourceMatches(string(source)) {
		return nil
	}
	beforeSession := cloneSession(*s.session)
	beforeDocument := s.autosaveDocument
	s.session.ReconcileExternalPlan(s.autosaveDocument.SourceText, string(source))
	s.autosaveDocument = NewDocument(s.autosaveDocument.CanonicalPath, string(source))
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = beforeSession
			s.autosaveDocument = beforeDocument
			return err
		}
	}
	return responseError{status: http.StatusConflict, message: "source plan changed outside PlanMaxx; reload the review before continuing"}
}

func writeSourceSyncError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrAutosaveConflict) {
		writeError(w, http.StatusConflict, "review state changed in another PlanMaxx session; reload the review before continuing")
		return
	}
	var httpErr responseError
	if errors.As(err, &httpErr) {
		writeError(w, httpErr.status, httpErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writePersistenceError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrAutosaveConflict) || errors.Is(err, revisions.ErrHeadChanged) {
		writeError(w, http.StatusConflict, "review state changed in another PlanMaxx session; reload the review before continuing")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
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

func parseRevisionRestorePath(path string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/revisions/"), "/")
	return parts[0], len(parts) == 2 && parts[0] != "" && parts[1] == "restore"
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
	rootAppliedAnchor := session.Anchor{}
	selectedSection, _ := sectioniter.SectionForAnchor(basePlan, request.Anchor)
	if s.session.PendingProposal != nil && anchorsEqual(s.session.PendingProposal.Anchor, request.Anchor) {
		basePlan = s.session.PendingProposal.ProposedPlan
		selectedSection = s.session.PendingProposal.ProposedSection
		replacementAnchor = s.session.PendingProposal.ReplacementAnchor
		if replacementAnchor.StartLine == 0 {
			replacementAnchor = pendingProposalReplacementAnchor(request.Anchor, selectedSection)
		}
		rootAppliedAnchor = s.session.PendingProposal.AppliedAnchor
		if rootAppliedAnchor.StartLine == 0 {
			rootAppliedAnchor = s.session.PendingProposal.Anchor
		}
	}
	includedThreadIDs := includedThreadIDs(*s.session, request.ThreadID, request.Anchor)
	protocolRevisionID := s.protocolRevisionIDLocked()
	return sectioniter.Request{
		RevisionID:          protocolRevisionID,
		ThreadID:            request.ThreadID,
		Plan:                basePlan,
		Anchor:              request.Anchor,
		ReplacementAnchor:   replacementAnchor,
		RootAppliedAnchor:   rootAppliedAnchor,
		SelectedSection:     selectedSection,
		ReviewerInstruction: request.Instruction,
		Protocol: reviewxml.Iteration(reviewxml.IterationInput{
			RevisionID:  protocolRevisionID,
			FilePath:    s.session.PlanPath,
			Plan:        basePlan,
			Target:      replacementAnchor,
			Instruction: request.Instruction,
			Threads:     s.session.Threads,
			SideAnswers: s.session.SideAnswers,
			Format:      s.session.PlanFormat,
		}),
		IncludedThreadIDs: includedThreadIDs,
		Format:            s.session.PlanFormat,
	}
}

func (s *Server) protocolRevisionIDLocked() string {
	for _, revision := range s.session.Revisions {
		if revision.ID == s.session.CurrentRevisionID && revision.CommitID != "" {
			return revision.CommitID
		}
	}
	return s.session.CurrentRevisionID
}

func (s *Server) withSideQuestionContext(req sidequestions.Request, thread session.Thread) sidequestions.Request {
	s.mu.Lock()
	planPath := s.session.PlanPath
	plan := s.session.Plan
	format := s.session.PlanFormat
	s.mu.Unlock()

	if req.FilePath == "" {
		req.FilePath = planPath
	}
	req.Format = format
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

// revisionFeedbackBetween returns immutable feedback snapshots in the order
// their accepted revisions occurred. A comparison may span several revisions;
// walking the parent chain avoids treating comments from unrelated history as
// causes of the displayed changes.
func revisionFeedbackBetween(s session.Session, fromID, toID string) []session.RevisionFeedback {
	byID := make(map[string]session.Revision, len(s.Revisions))
	for _, revision := range s.Revisions {
		byID[revision.ID] = revision
	}
	var reverse []session.Revision
	current, ok := byID[toID]
	for ok && current.ID != fromID {
		reverse = append(reverse, current)
		current, ok = byID[current.ParentID]
	}
	if !ok || current.ID != fromID {
		return nil
	}
	feedback := make([]session.RevisionFeedback, 0)
	for index := len(reverse) - 1; index >= 0; index-- {
		feedback = append(feedback, reverse[index].Feedback...)
	}
	return feedback
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
	aStartLine, aStartChar, aEndLine, aEndChar := anchorBounds(a)
	bStartLine, bStartChar, bEndLine, bEndChar := anchorBounds(b)
	return pointBefore(aStartLine, aStartChar, bEndLine, bEndChar) && pointBefore(bStartLine, bStartChar, aEndLine, aEndChar)
}

func anchorBounds(anchor session.Anchor) (startLine, startChar, endLine, endChar int) {
	if anchor.StartChar == 0 && anchor.EndChar == 0 {
		return anchor.StartLine, 0, anchor.EndLine, maxInt
	}
	return anchor.StartLine, anchor.StartChar, anchor.EndLine, anchor.EndChar
}

func pointBefore(line, char, otherLine, otherChar int) bool {
	return line < otherLine || (line == otherLine && char < otherChar)
}

const maxInt = int(^uint(0) >> 1)

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
