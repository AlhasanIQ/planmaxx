package session

import "strings"

// ContextOptions scopes the canonical model-facing review selection. A nil
// anchor means the whole plan. ExplicitThreadID is used by "Iterate now" and
// may select an active private thread for that request only.
type ContextOptions struct {
	Anchor           *Anchor
	ExplicitThreadID string
}

type ContextSelection struct {
	InstructionThreads []Thread
	ContextThreads     []Thread
	IncludedAnswers    []SideAnswer
	ThreadIDs          []string
	AnswerIDs          []string
}

func SelectContext(source Session, options ContextOptions) ContextSelection {
	selection := ContextSelection{
		InstructionThreads: []Thread{},
		ContextThreads:     []Thread{},
		IncludedAnswers:    []SideAnswer{},
		ThreadIDs:          []string{},
		AnswerIDs:          []string{},
	}
	threadByID := make(map[string]Thread, len(source.Threads))
	messageThread := map[string]bool{}
	contextThread := map[string]bool{}
	for _, thread := range source.Threads {
		threadByID[thread.ID] = thread
		if thread.Lifecycle() != ThreadLifecycleActive {
			continue
		}
		explicit := thread.ID == options.ExplicitThreadID
		if explicit || (thread.Intent() == ThreadIntentInstruction && threadInScope(thread, options.Anchor)) {
			selection.InstructionThreads = append(selection.InstructionThreads, thread)
			selection.ThreadIDs = append(selection.ThreadIDs, thread.ID)
			messageThread[thread.ID] = true
		}
	}
	for _, answer := range source.SideAnswers {
		thread, ok := threadByID[answer.ThreadID]
		if !answer.Promoted || !ok || thread.Lifecycle() != ThreadLifecycleActive || (thread.ID != options.ExplicitThreadID && !threadInScope(thread, options.Anchor)) {
			continue
		}
		selection.IncludedAnswers = append(selection.IncludedAnswers, answer)
		selection.AnswerIDs = append(selection.AnswerIDs, answer.ID)
		contextThread[thread.ID] = true
	}
	for _, thread := range source.Threads {
		if messageThread[thread.ID] || contextThread[thread.ID] {
			selection.ContextThreads = append(selection.ContextThreads, thread)
		}
	}
	return selection
}

func (selection ContextSelection) InstructionMessages() []string {
	out := []string{}
	for _, thread := range selection.InstructionThreads {
		for _, message := range thread.Messages {
			if strings.TrimSpace(message.Body) != "" {
				out = append(out, message.Body)
			}
		}
	}
	return out
}

func threadInScope(thread Thread, anchor *Anchor) bool {
	return anchor == nil || anchorsOverlap(thread.Anchor, *anchor)
}
