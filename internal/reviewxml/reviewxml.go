// Package reviewxml renders the deterministic, model-facing view of a review.
// It is deliberately not the persistence format: the session remains the
// source of truth, and this package derives escaped XML immediately before a
// prompt is sent to the model.
package reviewxml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const ProtocolVersion = "1"

type IterationInput struct {
	RevisionID  string
	FilePath    string
	Plan        string
	Target      session.Anchor
	Instruction string
	Threads     []session.Thread
	SideAnswers []session.SideAnswer
}

type SideQuestionInput struct {
	FilePath     string
	Reference    string
	SelectedText string
	PlanExcerpt  string
	Question     string
}

// Handoff returns XML that pairs an annotated copy of the plan with the
// review threads that are actually included in a handoff.
func Handoff(s session.Session) string {
	threads := includedThreads(s)
	return render("planmaxx_review", s.PlanPath, s.Plan, threads, s.SideAnswers, "", session.Anchor{}, 0, 0, "")
}

// Iteration returns XML that identifies the exact current replacement target
// and every relevant open review thread. The protocol intentionally does not
// impose a proximity window: content anchors validate any affected plan area.
func Iteration(input IterationInput) string {
	threads := includedThreads(session.Session{Threads: input.Threads, SideAnswers: input.SideAnswers})
	return render(
		"planmaxx_iteration",
		input.FilePath,
		input.Plan,
		threads,
		input.SideAnswers,
		input.RevisionID,
		input.Target,
		0,
		0,
		input.Instruction,
	)
}

// SideQuestion serializes the already-exact side-question context with the
// same escaping and data-boundary guarantees as the review/iteration views.
func SideQuestion(input SideQuestionInput) string {
	var out bytes.Buffer
	out.WriteString(`<planmaxx_side_question version="1">` + "\n  <target")
	if input.FilePath != "" {
		out.WriteString(` file="`)
		escape(&out, input.FilePath)
		out.WriteString(`"`)
	}
	out.WriteString(` reference="`)
	escape(&out, input.Reference)
	out.WriteString(`">` + "\n    <selected_text>")
	escape(&out, input.SelectedText)
	out.WriteString("</selected_text>\n  </target>\n  <question>")
	escape(&out, input.Question)
	out.WriteString("</question>\n  <plan_excerpt>")
	escape(&out, input.PlanExcerpt)
	out.WriteString("</plan_excerpt>\n</planmaxx_side_question>")
	return out.String()
}

func render(root, filePath, plan string, threads []session.Thread, answers []session.SideAnswer, revisionID string, target session.Anchor, allowedStart, allowedEnd int, instruction string) string {
	var out bytes.Buffer
	out.WriteString("<")
	out.WriteString(root)
	out.WriteString(` version="`)
	out.WriteString(ProtocolVersion)
	out.WriteString(`"`)
	if revisionID != "" {
		out.WriteString(` revision="`)
		escape(&out, revisionID)
		out.WriteString(`"`)
	}
	out.WriteString(">\n")

	if root == "planmaxx_iteration" {
		fmt.Fprintf(&out, "  <target id=\"selection\" source=\"%s\">\n", reference(target))
		out.WriteString("    <selected_text>")
		escape(&out, selectionForThread(plan, target, ""))
		out.WriteString("</selected_text>\n  </target>\n")
		out.WriteString("  <reviewer_instruction>")
		escape(&out, instruction)
		out.WriteString("</reviewer_instruction>\n")
	}

	out.WriteString("  <annotated_plan")
	if filePath != "" {
		out.WriteString(` file="`)
		escape(&out, filePath)
		out.WriteString(`"`)
	}
	out.WriteString(">\n")
	var selectionTarget *session.Anchor
	if root == "planmaxx_iteration" {
		selectionTarget = &target
	}
	writeAnnotatedPlan(&out, plan, threads, selectionTarget)
	out.WriteString("\n  </annotated_plan>\n")

	out.WriteString("  <review_threads>\n")
	for _, thread := range threads {
		fmt.Fprintf(&out, "    <thread id=\"%s\" target=\"%s\">\n", escaped(thread.ID), reference(thread.Anchor))
		out.WriteString("      <selected_text>")
		escape(&out, selectionForThread(plan, thread.Anchor, thread.SelectedText))
		out.WriteString("</selected_text>\n")
		for _, message := range messagesForThread(thread) {
			out.WriteString("      <message>")
			escape(&out, message.Body)
			out.WriteString("</message>\n")
		}
		for _, answer := range promotedAnswersForThread(answers, thread.ID) {
			out.WriteString("      <promoted_side_answer>\n        <question>")
			escape(&out, answer.Question)
			out.WriteString("</question>\n        <answer>")
			escape(&out, answer.Answer)
			out.WriteString("</answer>\n      </promoted_side_answer>\n")
		}
		out.WriteString("    </thread>\n")
	}
	out.WriteString("  </review_threads>\n")
	out.WriteString("</")
	out.WriteString(root)
	out.WriteString(">")
	return out.String()
}

func includedThreads(s session.Session) []session.Thread {
	withPromotedAnswer := map[string]bool{}
	for _, answer := range s.SideAnswers {
		if answer.Promoted {
			withPromotedAnswer[answer.ThreadID] = true
		}
	}
	var out []session.Thread
	for _, thread := range s.Threads {
		openDecision := (thread.Kind == "" || thread.Kind == session.ThreadKindDecision) &&
			(thread.Status == "" || thread.Status == session.ThreadStatusOpen)
		if openDecision || withPromotedAnswer[thread.ID] {
			out = append(out, thread)
		}
	}
	return out
}

func messagesForThread(thread session.Thread) []session.Message {
	if (thread.Kind == "" || thread.Kind == session.ThreadKindDecision) && (thread.Status == "" || thread.Status == session.ThreadStatusOpen) {
		return thread.Messages
	}
	return nil
}

func promotedAnswersForThread(answers []session.SideAnswer, threadID string) []session.SideAnswer {
	var out []session.SideAnswer
	for _, answer := range answers {
		if answer.Promoted && answer.ThreadID == threadID {
			out = append(out, answer)
		}
	}
	return out
}

type byteSpan struct {
	start    int
	end      int
	threadID string
	targetID string
}

func writeAnnotatedPlan(out *bytes.Buffer, plan string, threads []session.Thread, selectionTarget *session.Anchor) {
	var spans []byteSpan
	for _, thread := range threads {
		start, end, ok := byteRange(plan, thread.Anchor)
		if ok && start < end {
			spans = append(spans, byteSpan{start: start, end: end, threadID: thread.ID})
		}
	}
	if selectionTarget != nil {
		start, end, ok := byteRange(plan, *selectionTarget)
		if ok && start < end {
			spans = append(spans, byteSpan{start: start, end: end, targetID: "selection"})
		}
	}
	if len(spans) == 0 {
		escape(out, plan)
		return
	}

	boundaries := make([]int, 0, len(spans)*2+2)
	boundaries = append(boundaries, 0, len(plan))
	for _, span := range spans {
		boundaries = append(boundaries, span.start, span.end)
	}
	sort.Ints(boundaries)
	boundaries = compactInts(boundaries)
	for i := 0; i < len(boundaries)-1; i++ {
		start, end := boundaries[i], boundaries[i+1]
		threadIDs, targetIDs := coveringReferences(spans, start, end)
		if len(threadIDs) == 0 && len(targetIDs) == 0 {
			escape(out, plan[start:end])
			continue
		}
		out.WriteString("<review_target")
		if len(targetIDs) > 0 {
			out.WriteString(` target="`)
			escape(out, strings.Join(targetIDs, " "))
			out.WriteString(`"`)
		}
		if len(threadIDs) > 0 {
			out.WriteString(` threads="`)
			escape(out, strings.Join(threadIDs, " "))
			out.WriteString(`"`)
		}
		out.WriteString(">")
		escape(out, plan[start:end])
		out.WriteString("</review_target>")
	}
}

func compactInts(values []int) []int {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func coveringReferences(spans []byteSpan, start, end int) (threadIDs []string, targetIDs []string) {
	threads := map[string]bool{}
	targets := map[string]bool{}
	for _, span := range spans {
		if span.start <= start && end <= span.end {
			if span.threadID != "" {
				threads[span.threadID] = true
			}
			if span.targetID != "" {
				targets[span.targetID] = true
			}
		}
	}
	threadIDs = make([]string, 0, len(threads))
	for id := range threads {
		threadIDs = append(threadIDs, id)
	}
	targetIDs = make([]string, 0, len(targets))
	for id := range targets {
		targetIDs = append(targetIDs, id)
	}
	sort.Strings(threadIDs)
	sort.Strings(targetIDs)
	return threadIDs, targetIDs
}

func selectionForThread(plan string, anchor session.Anchor, stored string) string {
	start, end, ok := byteRange(plan, anchor)
	if ok && start < end {
		return plan[start:end]
	}
	return stored
}

func byteRange(plan string, anchor session.Anchor) (int, int, bool) {
	lines := strings.Split(plan, "\n")
	if anchor.StartLine < 1 || anchor.EndLine < anchor.StartLine || anchor.EndLine > len(lines) {
		return 0, 0, false
	}
	offsets := make([]int, len(lines))
	next := 0
	for i, line := range lines {
		offsets[i] = next
		next += len(line)
		if i < len(lines)-1 {
			next++
		}
	}
	if !hasCharacterRange(anchor) {
		return offsets[anchor.StartLine-1], offsets[anchor.EndLine-1] + len(lines[anchor.EndLine-1]), true
	}
	start, ok := utf16ByteIndex(lines[anchor.StartLine-1], anchor.StartChar)
	if !ok {
		return 0, 0, false
	}
	end, ok := utf16ByteIndex(lines[anchor.EndLine-1], anchor.EndChar)
	if !ok {
		return 0, 0, false
	}
	absStart := offsets[anchor.StartLine-1] + start
	absEnd := offsets[anchor.EndLine-1] + end
	return absStart, absEnd, absStart <= absEnd
}

func hasCharacterRange(anchor session.Anchor) bool {
	return anchor.StartChar != 0 || anchor.EndChar != 0
}

func utf16ByteIndex(text string, offset int) (int, bool) {
	if offset < 0 {
		return 0, false
	}
	units := 0
	for index, char := range text {
		if units == offset {
			return index, true
		}
		if char > 0xffff {
			units += 2
		} else {
			units++
		}
		if units > offset {
			return 0, false
		}
	}
	return len(text), units == offset
}

func reference(anchor session.Anchor) string {
	if !hasCharacterRange(anchor) {
		if anchor.StartLine == anchor.EndLine {
			return fmt.Sprintf("%d", anchor.StartLine)
		}
		return fmt.Sprintf("%d-%d", anchor.StartLine, anchor.EndLine)
	}
	return fmt.Sprintf("%d:%d-%d:%d", anchor.StartLine, anchor.StartChar+1, anchor.EndLine, anchor.EndChar+1)
}

func escape(out *bytes.Buffer, value string) {
	_ = xml.EscapeText(out, []byte(value))
}

func escaped(value string) string {
	var out bytes.Buffer
	escape(&out, value)
	return out.String()
}
