package session

import "fmt"

// ThreadIntent is the reviewer-facing meaning of the legacy persisted Kind
// field. Persisted values stay unchanged so existing autosaves remain valid.
type ThreadIntent string

const (
	ThreadIntentInstruction ThreadIntent = "instruction"
	ThreadIntentPrivate     ThreadIntent = "private"
)

// ThreadLifecycle is the reviewer-facing meaning of the legacy persisted
// Status field. It deliberately remains orthogonal to intent.
type ThreadLifecycle string

const (
	ThreadLifecycleActive    ThreadLifecycle = "active"
	ThreadLifecycleAddressed ThreadLifecycle = "addressed"
	ThreadLifecycleDetached  ThreadLifecycle = "detached"
)

type AnswerDelivery string

const (
	AnswerDeliveryPrivate  AnswerDelivery = "private"
	AnswerDeliveryIncluded AnswerDelivery = "included"
)

func (thread Thread) Intent() ThreadIntent {
	if thread.Kind == ThreadKindNote {
		return ThreadIntentPrivate
	}
	return ThreadIntentInstruction
}

func (thread Thread) Lifecycle() ThreadLifecycle {
	switch thread.Status {
	case ThreadStatusResolved:
		return ThreadLifecycleAddressed
	case ThreadStatusStale:
		return ThreadLifecycleDetached
	default:
		return ThreadLifecycleActive
	}
}

func kindForIntent(intent ThreadIntent) (string, bool) {
	switch intent {
	case ThreadIntentInstruction:
		return ThreadKindDecision, true
	case ThreadIntentPrivate:
		return ThreadKindNote, true
	default:
		return "", false
	}
}

func intentForLegacyKind(kind string) (ThreadIntent, bool) {
	switch kind {
	case "", ThreadKindDecision:
		return ThreadIntentInstruction, true
	case ThreadKindNote:
		return ThreadIntentPrivate, true
	default:
		return "", false
	}
}

func (s *Session) AddThreadWithIntent(anchor Anchor, body, selectedText string, intent ThreadIntent) (Thread, error) {
	kind, ok := kindForIntent(intent)
	if !ok {
		return Thread{}, &TransitionError{Kind: TransitionInvariant, Message: "invalid thread intent"}
	}
	thread := s.addThread(anchor, body, selectedText, kind)
	return thread, nil
}

func (s *Session) SetThreadIntent(threadID string, intent ThreadIntent) error {
	kind, ok := kindForIntent(intent)
	if !ok {
		return &TransitionError{Kind: TransitionInvariant, Message: "invalid thread intent"}
	}
	thread, err := s.mutableActiveThread(threadID, "change intent for")
	if err != nil {
		return err
	}
	thread.Kind = kind
	return nil
}

func (s *Session) AddReplyChecked(threadID, body string) error {
	thread, err := s.mutableActiveThread(threadID, "reply to")
	if err != nil {
		return err
	}
	thread.Messages = append(thread.Messages, s.newMessage("reviewer", body))
	return nil
}

func (s *Session) ReanchorThreadChecked(threadID string, anchor Anchor) error {
	thread, err := s.threadPointer(threadID)
	if err != nil {
		return err
	}
	if thread.Lifecycle() == ThreadLifecycleAddressed {
		return blocked("addressed feedback is read-only; create a follow-up instead")
	}
	thread.Anchor = anchor
	thread.Status = ThreadStatusOpen
	return nil
}

func (s *Session) EditThreadChecked(threadID string, anchor Anchor, body, selectedText string) error {
	thread, err := s.threadPointer(threadID)
	if err != nil {
		return err
	}
	if thread.Lifecycle() == ThreadLifecycleAddressed {
		return blocked("addressed feedback is read-only; create a follow-up instead")
	}
	if len(thread.Messages) == 0 {
		return invariant("thread has no original message")
	}
	thread.Anchor = anchor
	thread.SelectedText = selectedText
	thread.Status = ThreadStatusOpen
	thread.Messages[0].Body = body
	return nil
}

func (s *Session) AddressThread(threadID string, anchor Anchor) error {
	thread, err := s.threadPointer(threadID)
	if err != nil {
		return err
	}
	if thread.Lifecycle() != ThreadLifecycleActive {
		return blocked("only active feedback can be addressed")
	}
	resolveThreadAfterProposal(thread, anchor)
	s.makeAnswersPrivateForThread(threadID)
	return nil
}

func (s *Session) DetachThread(threadID string) error {
	thread, err := s.threadPointer(threadID)
	if err != nil {
		return err
	}
	if thread.Lifecycle() != ThreadLifecycleActive {
		return blocked("only active feedback can become detached")
	}
	thread.Status = ThreadStatusStale
	s.makeAnswersPrivateForThread(threadID)
	return nil
}

func (s *Session) IncludeSideAnswer(answerID string) error {
	answer, thread, err := s.answerAndThread(answerID)
	if err != nil {
		return err
	}
	if thread.Lifecycle() != ThreadLifecycleActive {
		return blocked("only answers on active feedback can be included")
	}
	answer.Promoted = true
	return nil
}

func (s *Session) KeepSideAnswerPrivate(answerID string) error {
	answer, _, err := s.answerAndThread(answerID)
	if err != nil {
		return err
	}
	answer.Promoted = false
	return nil
}

func (s *Session) AddSideAnswerChecked(threadID, question, answer string) (SideAnswer, error) {
	if _, err := s.mutableActiveThread(threadID, "ask a side question on"); err != nil {
		return SideAnswer{}, err
	}
	return s.addSideAnswer(threadID, question, answer), nil
}

func (s *Session) CreateFollowUp(threadID string) (Thread, error) {
	source, err := s.threadPointer(threadID)
	if err != nil {
		return Thread{}, err
	}
	if source.Lifecycle() != ThreadLifecycleAddressed {
		return Thread{}, blocked("follow-ups are created from addressed feedback")
	}
	body := "Follow up on addressed feedback."
	if len(source.Messages) > 0 && source.Messages[len(source.Messages)-1].Body != "" {
		body = "Follow up: " + source.Messages[len(source.Messages)-1].Body
	}
	anchor := clampedLineAnchor(source.Anchor, s.Plan)
	return s.AddThreadWithIntent(anchor, body, "", source.Intent())
}

func (s *Session) threadPointer(threadID string) (*Thread, error) {
	for index := range s.Threads {
		if s.Threads[index].ID == threadID {
			return &s.Threads[index], nil
		}
	}
	return nil, &TransitionError{Kind: TransitionMissing, Message: "thread not found"}
}

func (s *Session) mutableActiveThread(threadID, action string) (*Thread, error) {
	thread, err := s.threadPointer(threadID)
	if err != nil {
		return nil, err
	}
	if thread.Lifecycle() != ThreadLifecycleActive {
		return nil, blocked(fmt.Sprintf("cannot %s non-active feedback", action))
	}
	return thread, nil
}

func (s *Session) answerAndThread(answerID string) (*SideAnswer, *Thread, error) {
	for index := range s.SideAnswers {
		if s.SideAnswers[index].ID != answerID {
			continue
		}
		thread, err := s.threadPointer(s.SideAnswers[index].ThreadID)
		if err != nil {
			return nil, nil, err
		}
		return &s.SideAnswers[index], thread, nil
	}
	return nil, nil, &TransitionError{Kind: TransitionMissing, Message: "side answer not found"}
}

func (s *Session) makeAnswersPrivateForThread(threadID string) {
	for index := range s.SideAnswers {
		if s.SideAnswers[index].ThreadID == threadID {
			s.SideAnswers[index].Promoted = false
		}
	}
}

func (s *Session) normalizeAnswerDelivery() {
	active := make(map[string]bool, len(s.Threads))
	for _, thread := range s.Threads {
		active[thread.ID] = thread.Lifecycle() == ThreadLifecycleActive
	}
	for index := range s.SideAnswers {
		if !active[s.SideAnswers[index].ThreadID] {
			s.SideAnswers[index].Promoted = false
		}
	}
}

func blocked(message string) error {
	return &TransitionError{Kind: TransitionBlocked, Message: message}
}
