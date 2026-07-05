package digest

import (
	"fmt"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/prompts"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func BuildPrompt(s session.Session) string {
	return prompts.ReviewDigest(s.Plan, threadMessages(s), promotedAnswers(s))
}

func DraftFromState(s session.Session) session.Digest {
	decisions := threadMessages(s)
	answers := promotedAnswers(s)
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

func threadMessages(s session.Session) []string {
	var out []string
	for _, thread := range s.Threads {
		if thread.Kind != "" && thread.Kind != session.ThreadKindDecision {
			continue
		}
		if thread.Status != "" && thread.Status != session.ThreadStatusOpen {
			continue
		}
		for _, message := range thread.Messages {
			out = append(out, message.Body)
		}
	}
	return out
}

func promotedAnswers(s session.Session) []string {
	var out []string
	for _, answer := range s.SideAnswers {
		if answer.Promoted {
			out = append(out, promotedAnswerContext(s, answer))
		}
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
