package digest

import (
	"fmt"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/reviewxml"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func BuildPrompt(s session.Session) string {
	selection := session.SelectContext(s, session.ContextOptions{})
	return prompts.ReviewDigest(s.Plan, selection.InstructionMessages(), selectedAnswers(s, selection), reviewxml.Handoff(s), s.PlanFormat)
}

func DraftFromState(s session.Session) session.Digest {
	selection := session.SelectContext(s, session.ContextOptions{})
	decisions := selection.InstructionMessages()
	answers := selectedAnswers(s, selection)
	summary := "Approved without comments."
	if len(decisions) > 0 || len(answers) > 0 {
		summary = "Approved with review comments."
	}
	return session.Digest{
		Summary:             summary,
		ReviewerDecisions:   decisions,
		PromotedSideAnswers: answers,
	}
}

func selectedAnswers(s session.Session, selection session.ContextSelection) []string {
	out := make([]string, 0, len(selection.IncludedAnswers))
	for _, answer := range selection.IncludedAnswers {
		out = append(out, promotedAnswerContext(s, answer))
	}
	return out
}

func promotedAnswerContext(s session.Session, answer session.SideAnswer) string {
	var b strings.Builder
	if thread, ok := findThread(s, answer.ThreadID); ok {
		fmt.Fprintf(&b, "/btw context: %s\n", digestAnchorReference(s.PlanPath, thread.Anchor))
		if strings.TrimSpace(thread.SelectedText) != "" {
			fmt.Fprintf(&b, "Selected text:\n%s\n", thread.SelectedText)
		}
	}
	fmt.Fprintf(&b, "Question:\n%s\nAnswer:\n%s", answer.Question, answer.Answer)
	return b.String()
}

func findThread(s session.Session, threadID string) (session.Thread, bool) {
	for _, thread := range s.Threads {
		if thread.ID == threadID {
			return thread, true
		}
	}
	return session.Thread{}, false
}

func digestAnchorReference(filePath string, anchor session.Anchor) string {
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
