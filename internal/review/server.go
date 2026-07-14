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

	"github.com/AlhasanIQ/planmaxx/internal/digest"
	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/reviewmodel"
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
	mux.HandleFunc("/api/revisions/propose-review", s.handleProposeReview)
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
		var migrated bool
		var err error
		s.autosaveStatus, migrated, err = migrateLoadedSession(s.session, saved.Status, true)
		if err != nil {
			return fmt.Errorf("migrate existing autosave: %w", err)
		}
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
		if err := s.session.Validate(); err != nil {
			return fmt.Errorf("validate existing autosave: %w", err)
		}
		if migrated || (s.revisionStore != nil && hasUnpersistedRevisions(*s.session)) {
			return s.persistLocked()
		}
		return nil
	}
	if err := s.hydrateRevisionBodiesLocked(); err != nil {
		return err
	}
	if err := s.session.Validate(); err != nil {
		return fmt.Errorf("validate review session: %w", err)
	}
	return s.persistLocked()
}

func rebuildPendingProposal(s *session.Session) bool {
	proposal := s.PendingProposal
	if proposal == nil || proposal.Obsolete || proposal.ParentID != s.CurrentRevisionID || proposal.RawResponse == "" || len(proposal.AppliedHunks) == 0 {
		return false
	}
	parsed, err := sectioniter.ParseResponse(proposal.RawResponse)
	if err != nil {
		return false
	}
	rebuilt, err := sectioniter.BuildProposal(sectioniter.Request{
		RevisionID:          parsed.RevisionID,
		ThreadID:            proposal.ThreadID,
		Plan:                s.Plan,
		Anchor:              proposal.Anchor,
		SelectedSection:     proposal.OriginalSection,
		ReviewerInstruction: proposal.Instruction,
		IncludedThreadIDs:   proposal.IncludedThreadIDs,
		Format:              s.PlanFormat,
	}, proposal.RawResponse)
	if err != nil || rebuilt.ProposedPlan == proposal.ProposedPlan {
		return false
	}
	proposal.Anchor = rebuilt.Anchor
	proposal.AppliedAnchor = rebuilt.AppliedAnchor
	proposal.AppliedHunks = rebuilt.AppliedHunks
	proposal.ReplacementAnchor = rebuilt.ReplacementAnchor
	proposal.OriginalSection = rebuilt.OriginalSection
	proposal.ProposedSection = rebuilt.ProposedSection
	proposal.ProposedPlan = rebuilt.ProposedPlan
	proposal.Summary = rebuilt.Summary
	proposal.Instruction = rebuilt.Instruction
	proposal.RawResponse = rebuilt.RawResponse
	proposal.IncludedThreadIDs = rebuilt.IncludedThreadIDs
	return true
}

func migrateLegacyReviewProposal(s *session.Session) bool {
	proposal := s.PendingProposal
	if proposal == nil || proposal.Kind != "" {
		return false
	}
	isWholePlan := proposal.ParentID == s.CurrentRevisionID && proposal.ThreadID == "" && proposal.Anchor == wholePlanAnchor(s.Plan)
	if !session.IsReviewProposal(*proposal) && !(isWholePlan && digestHasContent(s.Digest)) {
		return false
	}
	proposal.Kind = session.ProposalKindReview
	if proposal.ReviewDigest == nil {
		digest := s.Digest
		digest.ReviewerDecisions = append([]string(nil), s.Digest.ReviewerDecisions...)
		digest.PromotedSideAnswers = append([]string(nil), s.Digest.PromotedSideAnswers...)
		proposal.ReviewDigest = &digest
	}
	if len(proposal.ConsumedSideAnswerIDs) == 0 {
		proposal.ConsumedSideAnswerIDs = promotedOpenSideAnswerIDs(*s)
	}
	return true
}

func isTerminalAutosaveStatus(status string) bool {
	switch status {
	case "finalized", "canceled", "rejected":
		return true
	default:
		return false
	}
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
	writeJSON(w, buildClientState(*s.session, s.finished))
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
	Anchor       session.Anchor       `json:"anchor"`
	Body         string               `json:"body"`
	SelectedText string               `json:"selectedText"`
	Intent       session.ThreadIntent `json:"intent"`
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
	if s.session.PendingProposal != nil {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "apply or discard the pending proposal before changing review feedback")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if err := validateThreadRequest(request, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid thread json")
		return
	}
	before := cloneSession(*s.session)
	if request.Intent == "" {
		request.Intent = session.ThreadIntentInstruction
	}
	thread, err := s.session.AddThreadWithIntent(request.Anchor, request.Body, request.SelectedText, request.Intent)
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			s.mu.Unlock()
			writePersistenceError(w, err)
			return
		}
	}
	view, _ := projectThread(*s.session, s.finished, thread.ID)
	s.mu.Unlock()
	writeJSON(w, view)
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
	case "intent":
		s.handleThreadIntent(w, r, threadID)
	case "follow-up":
		s.handleCreateFollowUp(w, threadID)
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
	s.mu.Lock()
	if s.session.PendingProposal != nil {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "apply or discard the pending proposal before asking a side question")
		return
	}
	var thread session.Thread
	var ok bool
	for _, candidate := range s.session.Threads {
		if candidate.ID == req.ThreadID {
			thread, ok = candidate, true
			break
		}
	}
	if ok && thread.Lifecycle() != session.ThreadLifecycleActive {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "reactivate feedback before asking a side question")
		return
	}
	baseRevisionID := s.session.CurrentRevisionID
	baseFeedback := reviewFeedbackFingerprint(*s.session)
	s.mu.Unlock()
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
	if s.session.PendingProposal != nil {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "apply or discard the pending proposal before asking a side question")
		return
	}
	if err := s.syncExternalSourceLocked(); err != nil {
		s.mu.Unlock()
		writeSourceSyncError(w, err)
		return
	}
	if s.session.CurrentRevisionID != baseRevisionID || reviewFeedbackFingerprint(*s.session) != baseFeedback {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "review feedback changed while the side question was running")
		return
	}
	before := cloneSession(*s.session)
	sideAnswer, err := s.session.AddSideAnswerChecked(req.ThreadID, req.Question, answer)
	if err != nil {
		*s.session = before
		s.mu.Unlock()
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.persistLocked(); err != nil {
		if !autosaveWasCommitted(err) {
			*s.session = before
			s.mu.Unlock()
			writePersistenceError(w, err)
			return
		}
	}
	view, _ := projectSideAnswer(*s.session, s.finished, sideAnswer.ID)
	s.mu.Unlock()
	writeJSON(w, view)
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

type threadIntentRequest struct {
	Intent session.ThreadIntent `json:"intent"`
}

type proposeSectionRequest struct {
	ThreadID    string         `json:"threadId"`
	Anchor      session.Anchor `json:"anchor"`
	Instruction string         `json:"instruction"`
}

type proposalLifecycle struct {
	kind                  string
	reviewDigest          *session.Digest
	consumedSideAnswerIDs []string
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := s.session.AddReplyChecked(threadID, request.Body); err != nil {
			return nil, transitionResponseError(err)
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := validateAnchor(anchor, s.session.Plan); err != nil {
			return nil, responseError{status: http.StatusBadRequest, message: "invalid anchor json"}
		}
		if err := s.session.ReanchorThreadChecked(threadID, anchor); err != nil {
			return nil, transitionResponseError(err)
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := validateAnchor(request.Anchor, s.session.Plan); err != nil {
			return nil, responseError{status: http.StatusBadRequest, message: "invalid thread edit json"}
		}
		if err := s.session.EditThreadChecked(threadID, request.Anchor, request.Body, request.SelectedText); err != nil {
			return nil, transitionResponseError(err)
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if !s.session.SetThreadKind(threadID, request.Kind) {
			if !threadExists(*s.session, threadID) {
				return nil, responseError{status: http.StatusNotFound, message: "thread not found"}
			}
			return nil, responseError{status: http.StatusBadRequest, message: "invalid thread kind json"}
		}
		return statusResponse("updated"), nil
	})
}

func (s *Server) handleThreadIntent(w http.ResponseWriter, r *http.Request, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	var request threadIntentRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid thread intent json")
		return
	}
	s.mutateSession(w, func() (any, error) {
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := s.session.SetThreadIntent(threadID, request.Intent); err != nil {
			return nil, transitionResponseError(err)
		}
		return statusResponse("updated"), nil
	})
}

func (s *Server) handleCreateFollowUp(w http.ResponseWriter, threadID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}
	s.mutateSession(w, func() (any, error) {
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		thread, err := s.session.CreateFollowUp(threadID)
		if err != nil {
			return nil, transitionResponseError(err)
		}
		view, _ := projectThread(*s.session, s.finished, thread.ID)
		return view, nil
	})
}

func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := buildClientState(*s.session, s.finished)
	writeJSON(w, map[string]any{
		"currentRevisionId": s.session.CurrentRevisionID,
		"revisions":         state.Revisions,
		"pendingProposal":   state.PendingProposal,
		"activeChange":      state.ActiveChange,
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
	format := s.session.PlanFormat
	var threads []session.Thread
	if to == s.session.CurrentRevisionID {
		threads = comparisonThreads(s.session.Threads, feedback)
	}
	store := s.revisionStore
	s.mu.Unlock()
	if !fromOK || !toOK {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	cacheKey, cacheable := revisionComparisonCacheKey(fromRevision, toRevision)
	cacheable = cacheable && store != nil
	if cacheable {
		if cached, ok := s.comparisonCache.Load(cacheKey); ok {
			writeJSON(w, reviewmodel.WithThreadPlacements(cached.(reviewmodel.ChangeView), threads))
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
	comparison := reviewmodel.Build(reviewmodel.BuildInput{
		Mode: reviewmodel.ModeRevision, IsDirect: toRevision.ParentID == from, BaseID: from, TargetID: to,
		Format: string(format), Before: fromRevision.Plan, After: toRevision.Plan,
		Feedback: feedback,
	})
	if cacheable {
		actual, _ := s.comparisonCache.LoadOrStore(cacheKey, comparison)
		comparison = actual.(reviewmodel.ChangeView)
	}
	writeJSON(w, reviewmodel.WithThreadPlacements(comparison, threads))
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
		restored, err := s.session.RestoreRevision(revisionID, content)
		if err != nil {
			return nil, transitionResponseError(err)
		}
		return restored, nil
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
	s.proposeSection(w, r, request, proposalLifecycle{})
}

func (s *Server) handleProposeReview(w http.ResponseWriter, r *http.Request) {
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
	s.proposeSection(w, r, proposeSectionRequest{}, proposalLifecycle{
		kind:         session.ProposalKindReview,
		reviewDigest: &reviewDigest,
	})
}

func (s *Server) proposeSection(w http.ResponseWriter, r *http.Request, request proposeSectionRequest, lifecycle proposalLifecycle) {
	request.Instruction = strings.TrimSpace(request.Instruction)

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
	if lifecycle.kind == session.ProposalKindReview {
		if s.session.PendingProposal != nil {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "apply or discard the pending proposal before iterating the final review")
			return
		}
		authoritative := digest.DraftFromState(*s.session)
		if lifecycle.reviewDigest != nil && strings.TrimSpace(lifecycle.reviewDigest.Summary) != "" {
			authoritative.Summary = strings.TrimSpace(lifecycle.reviewDigest.Summary)
		}
		lifecycle.reviewDigest = &authoritative
		request.Anchor = wholePlanAnchor(s.session.Plan)
		request.Instruction = reviewIterationInstruction(lifecycle.reviewDigest, "")
		lifecycle.consumedSideAnswerIDs = append([]string(nil), session.SelectContext(*s.session, session.ContextOptions{}).AnswerIDs...)
	} else if pending := s.session.PendingProposal; pending != nil && pending.Kind == session.ProposalKindReview && anchorsEqual(pending.Anchor, request.Anchor) {
		lifecycle = proposalLifecycle{
			kind:                  pending.Kind,
			reviewDigest:          cloneDigestPointer(pending.ReviewDigest),
			consumedSideAnswerIDs: append([]string(nil), pending.ConsumedSideAnswerIDs...),
		}
		request.Instruction = reviewIterationInstruction(lifecycle.reviewDigest, request.Instruction)
	}
	if request.Instruction == "" {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	if err := validateAnchor(request.Anchor, s.session.Plan); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid section proposal json")
		return
	}
	if request.ThreadID != "" {
		thread, exists := findThread(*s.session, request.ThreadID)
		if !exists {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "thread not found")
			return
		}
		if thread.Lifecycle() != session.ThreadLifecycleActive {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "reactivate feedback before iterating it")
			return
		}
	}
	if len(lifecycle.consumedSideAnswerIDs) == 0 {
		selection := session.SelectContext(*s.session, session.ContextOptions{Anchor: &request.Anchor, ExplicitThreadID: request.ThreadID})
		lifecycle.consumedSideAnswerIDs = append([]string(nil), selection.AnswerIDs...)
	}
	if s.session.PendingProposal != nil && s.session.PendingProposal.Obsolete {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "discard the stale section proposal before iterating again")
		return
	}
	if s.session.PendingProposal != nil && !anchorsEqual(s.session.PendingProposal.Anchor, request.Anchor) {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "apply or discard the pending proposal before iterating a different section")
		return
	}
	baseRevisionID := s.session.CurrentRevisionID
	basePendingProposalID := pendingProposalID(s.session.PendingProposal)
	baseFeedback := reviewFeedbackFingerprint(*s.session)
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
	if s.session.CurrentRevisionID != baseRevisionID || pendingProposalID(s.session.PendingProposal) != basePendingProposalID || reviewFeedbackFingerprint(*s.session) != baseFeedback {
		writeError(w, http.StatusConflict, "plan changed while section iteration was running")
		return
	}
	before := cloneSession(*s.session)
	input.Kind = lifecycle.kind
	input.ReviewDigest = cloneDigestPointer(lifecycle.reviewDigest)
	input.ConsumedSideAnswerIDs = append([]string(nil), lifecycle.consumedSideAnswerIDs...)
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
			revision, err := s.session.ApplyProposalChecked(proposalID)
			if err != nil {
				return nil, transitionResponseError(err)
			}
			return revision, nil
		})
	case "discard":
		s.mutateSession(w, func() (any, error) {
			if err := s.session.DiscardProposalChecked(proposalID); err != nil {
				return nil, transitionResponseError(err)
			}
			return statusResponse("discarded"), nil
		})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func transitionResponseError(err error) error {
	switch {
	case session.IsTransition(err, session.TransitionMissing):
		return responseError{status: http.StatusNotFound, message: err.Error()}
	case session.IsTransition(err, session.TransitionStale), session.IsTransition(err, session.TransitionBlocked):
		return responseError{status: http.StatusConflict, message: err.Error()}
	case session.IsTransition(err, session.TransitionInvariant):
		return responseError{status: http.StatusBadRequest, message: err.Error()}
	default:
		return err
	}
}

func (s *Server) handlePromoteSideAnswer(w http.ResponseWriter, sideAnswerID string) {
	if s.isFinished() {
		writeError(w, http.StatusConflict, "review already completed")
		return
	}

	s.mutateSession(w, func() (any, error) {
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := s.session.IncludeSideAnswer(sideAnswerID); err != nil {
			return nil, transitionResponseError(err)
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
		if err := rejectPendingFeedbackMutation(s.session); err != nil {
			return nil, err
		}
		if err := s.session.KeepSideAnswerPrivate(sideAnswerID); err != nil {
			return nil, transitionResponseError(err)
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
		*s.session = before
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

func rejectPendingFeedbackMutation(s *session.Session) error {
	if s.PendingProposal == nil {
		return nil
	}
	return responseError{
		status:  http.StatusConflict,
		message: "apply or discard the pending proposal before changing review feedback",
	}
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

func (s *Server) finalize(submitted session.Digest) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return Result{}, errors.New("review already completed")
	}
	if err := s.syncExternalSourceLocked(); err != nil {
		return Result{}, err
	}
	if s.session.PendingProposal != nil {
		return Result{}, errors.New("apply or discard the pending proposal before finalizing")
	}
	canonical := digest.DraftFromState(*s.session)
	if strings.TrimSpace(submitted.Summary) != "" {
		canonical.Summary = strings.TrimSpace(submitted.Summary)
	}
	before := cloneSession(*s.session)
	previousStatus := s.autosaveStatus
	s.session.SetDigest(canonical)
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
	if err := s.session.Validate(); err != nil {
		return fmt.Errorf("validate review session: %w", err)
	}
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
	next := saved.Session
	status, _, err := migrateLoadedSession(&next, saved.Status, false)
	if err != nil {
		return fmt.Errorf("migrate reloaded autosave: %w", err)
	}
	previousSession, previousStatus, previousFinished := s.session, s.autosaveStatus, s.finished
	previousDocument, previousGeneration := s.autosaveDocument, s.autosaveGeneration
	s.session = &next
	s.autosaveStatus = status
	s.autosaveDocument = saved.Document
	s.autosaveGeneration = saved.Generation
	s.finished = isTerminalAutosaveStatus(status)
	if err := s.hydrateRevisionBodiesLocked(); err != nil {
		s.session, s.autosaveStatus, s.finished = previousSession, previousStatus, previousFinished
		s.autosaveDocument, s.autosaveGeneration = previousDocument, previousGeneration
		return err
	}
	if err := s.session.Validate(); err != nil {
		s.session, s.autosaveStatus, s.finished = previousSession, previousStatus, previousFinished
		s.autosaveDocument, s.autosaveGeneration = previousDocument, previousGeneration
		return fmt.Errorf("validate reloaded autosave: %w", err)
	}
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
	selection := session.SelectContext(*s.session, session.ContextOptions{Anchor: &request.Anchor, ExplicitThreadID: request.ThreadID})
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
			RevisionID:       protocolRevisionID,
			FilePath:         s.session.PlanPath,
			Plan:             basePlan,
			Target:           replacementAnchor,
			Instruction:      request.Instruction,
			Threads:          s.session.Threads,
			SideAnswers:      s.session.SideAnswers,
			Selection:        &selection,
			ExplicitThreadID: request.ThreadID,
			Format:           s.session.PlanFormat,
		}),
		IncludedThreadIDs: append([]string(nil), selection.ThreadIDs...),
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

func comparisonThreads(threads []session.Thread, feedback []session.RevisionFeedback) []session.Thread {
	ownedByFeedback := make(map[string]bool, len(feedback))
	for _, item := range feedback {
		ownedByFeedback[item.ThreadID] = true
	}
	out := make([]session.Thread, 0, len(threads))
	for _, thread := range threads {
		if !ownedByFeedback[thread.ID] {
			out = append(out, thread)
		}
	}
	return out
}

func promotedOpenSideAnswerIDs(s session.Session) []string {
	return session.SelectContext(s, session.ContextOptions{}).AnswerIDs
}

func digestHasContent(digest session.Digest) bool {
	if strings.TrimSpace(digest.Summary) != "" {
		return true
	}
	for _, values := range [][]string{digest.ReviewerDecisions, digest.PromotedSideAnswers} {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

func reviewFeedbackFingerprint(value session.Session) string {
	data, err := json.Marshal(struct {
		Threads     []session.Thread
		SideAnswers []session.SideAnswer
	}{value.Threads, value.SideAnswers})
	if err != nil {
		panic(fmt.Sprintf("fingerprint review feedback: %v", err))
	}
	return string(data)
}

func reviewIterationInstruction(reviewDigest *session.Digest, refinement string) string {
	if reviewDigest == nil {
		return strings.TrimSpace(refinement)
	}
	return prompts.ReviewIterationInstruction(
		reviewDigest.Summary,
		reviewDigest.ReviewerDecisions,
		reviewDigest.PromotedSideAnswers,
		refinement,
	)
}

func cloneDigestPointer(source *session.Digest) *session.Digest {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.ReviewerDecisions = append([]string(nil), source.ReviewerDecisions...)
	cloned.PromotedSideAnswers = append([]string(nil), source.PromotedSideAnswers...)
	return &cloned
}

func wholePlanAnchor(plan string) session.Anchor {
	lines := strings.Split(plan, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return session.Anchor{StartLine: 1, EndLine: max(1, len(lines))}
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
