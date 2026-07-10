package session

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type Session struct {
	ID                string           `json:"id"`
	Plan              string           `json:"plan"`
	PlanPath          string           `json:"planPath"`
	CurrentRevisionID string           `json:"currentRevisionId"`
	Revisions         []Revision       `json:"revisions"`
	PendingProposal   *SectionProposal `json:"pendingProposal,omitempty"`
	Threads           []Thread         `json:"threads"`
	SideAnswers       []SideAnswer     `json:"sideAnswers"`
	Digest            Digest           `json:"digest"`
	NextThreadID      int              `json:"nextThreadId,omitempty"`
	NextSideAnswerID  int              `json:"nextSideAnswerId,omitempty"`
	NextProposalID    int              `json:"nextProposalId,omitempty"`
	nextMessage       int
	nextRevision      int
}

const (
	ThreadKindDecision = "decision"
	ThreadKindNote     = "note"

	ThreadStatusOpen     = "open"
	ThreadStatusResolved = "resolved"
	ThreadStatusStale    = "stale"

	RevisionSourceInitial   = "initial"
	RevisionSourceTurn      = "turn"
	RevisionSourceImmediate = "immediate"
	RevisionSourceExternal  = "external"
)

type Anchor struct {
	StartLine int `json:"startLine"`
	StartChar int `json:"startChar"`
	EndLine   int `json:"endLine"`
	EndChar   int `json:"endChar"`
}

func hasCharacterRange(anchor Anchor) bool {
	return anchor.StartChar != 0 || anchor.EndChar != 0
}

type Thread struct {
	ID           string    `json:"id"`
	Anchor       Anchor    `json:"anchor"`
	SelectedText string    `json:"selectedText,omitempty"`
	Kind         string    `json:"kind"`
	Status       string    `json:"status"`
	Position     Position  `json:"position"`
	Messages     []Message `json:"messages"`
}

type Revision struct {
	ID        string    `json:"id"`
	CommitID  string    `json:"commitId,omitempty"`
	ParentID  string    `json:"parentId,omitempty"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
	Plan      string    `json:"plan"`
	Anchor    Anchor    `json:"anchor,omitempty"`
	Summary   string    `json:"summary,omitempty"`
}

type SectionProposal struct {
	ID                string        `json:"id"`
	ParentID          string        `json:"parentId"`
	ThreadID          string        `json:"threadId,omitempty"`
	Anchor            Anchor        `json:"anchor"`                  // Original reviewer selection.
	AppliedAnchor     Anchor        `json:"appliedAnchor,omitempty"` // Source-plan region affected when this proposal is applied.
	AppliedHunks      []AppliedHunk `json:"appliedHunks,omitempty"`
	ReplacementAnchor Anchor        `json:"replacementAnchor"` // Result range in ProposedPlan.
	OriginalSection   string        `json:"originalSection"`
	ProposedSection   string        `json:"proposedSection"`
	ProposedPlan      string        `json:"proposedPlan"`
	Summary           string        `json:"summary"`
	Instruction       string        `json:"instruction"`
	RawResponse       string        `json:"rawResponse"`
	IncludedThreadIDs []string      `json:"includedThreadIds,omitempty"`
	Obsolete          bool          `json:"obsolete,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
}

type AppliedHunk struct {
	Anchor    Anchor `json:"anchor"`
	Result    Anchor `json:"result"`
	LineDelta int    `json:"lineDelta"`
}

type SectionProposalInput struct {
	ThreadID          string
	Anchor            Anchor
	AppliedAnchor     Anchor
	AppliedHunks      []AppliedHunk
	ReplacementAnchor Anchor
	OriginalSection   string
	ProposedSection   string
	ProposedPlan      string
	Summary           string
	Instruction       string
	RawResponse       string
	IncludedThreadIDs []string
}

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Message struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type SideAnswer struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"threadId"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Promoted  bool      `json:"promoted"`
	CreatedAt time.Time `json:"createdAt"`
}

type Digest struct {
	Summary             string   `json:"summary"`
	ReviewerDecisions   []string `json:"reviewerDecisions"`
	PromotedSideAnswers []string `json:"promotedSideAnswers"`
}

func New(id string, plan string) *Session {
	s := &Session{ID: id, Plan: plan}
	s.addRevision("", RevisionSourceInitial, plan, Anchor{}, "Initial plan")
	return s
}

func (s *Session) AddThread(anchor Anchor, body string) Thread {
	return s.AddThreadWithSelectedText(anchor, body, "")
}

func (s *Session) AddThreadWithSelectedText(anchor Anchor, body string, selectedText string) Thread {
	thread := Thread{
		ID:           fmt.Sprintf("thread-%d", s.NextThreadID+1),
		Anchor:       anchor,
		SelectedText: selectedText,
		Kind:         ThreadKindDecision,
		Status:       ThreadStatusOpen,
		Position: Position{
			X: 720,
			Y: 120 + len(s.Threads)*96,
		},
		Messages: []Message{s.newMessage("reviewer", body)},
	}
	s.NextThreadID++
	s.Threads = append(s.Threads, thread)
	return thread
}

func (s *Session) AddReply(threadID string, body string) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads[i].Messages = append(s.Threads[i].Messages, s.newMessage("reviewer", body))
			return true
		}
	}
	return false
}

func (s *Session) DeleteThread(threadID string) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads = append(s.Threads[:i], s.Threads[i+1:]...)
			s.deleteSideAnswersForThread(threadID)
			return true
		}
	}
	return false
}

func (s *Session) SetThreadKind(threadID string, kind string) bool {
	if kind != ThreadKindDecision && kind != ThreadKindNote {
		return false
	}
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads[i].Kind = kind
			return true
		}
	}
	return false
}

func (s *Session) ResolveThread(threadID string) bool {
	return s.setThreadStatus(threadID, ThreadStatusResolved)
}

func (s *Session) MarkThreadStale(threadID string) bool {
	return s.setThreadStatus(threadID, ThreadStatusStale)
}

func (s *Session) MoveThread(threadID string, position Position) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads[i].Position = position
			return true
		}
	}
	return false
}

func (s *Session) ReanchorThread(threadID string, anchor Anchor) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads[i].Anchor = anchor
			s.Threads[i].Status = ThreadStatusOpen
			return true
		}
	}
	return false
}

func (s *Session) EditThread(threadID string, anchor Anchor, body string) bool {
	return s.EditThreadWithSelectedText(threadID, anchor, body, "")
}

func (s *Session) EditThreadWithSelectedText(threadID string, anchor Anchor, body string, selectedText string) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			if len(s.Threads[i].Messages) == 0 {
				return false
			}
			s.Threads[i].Anchor = anchor
			s.Threads[i].SelectedText = selectedText
			s.Threads[i].Status = ThreadStatusOpen
			s.Threads[i].Messages[0].Body = body
			return true
		}
	}
	return false
}

func (s *Session) AddSideAnswer(threadID string, question string, answer string) SideAnswer {
	sideAnswer := SideAnswer{
		ID:        fmt.Sprintf("side-%d", s.NextSideAnswerID+1),
		ThreadID:  threadID,
		Question:  question,
		Answer:    answer,
		CreatedAt: time.Now().UTC(),
	}
	s.NextSideAnswerID++
	s.SideAnswers = append(s.SideAnswers, sideAnswer)
	return sideAnswer
}

func (s *Session) PromoteSideAnswer(sideAnswerID string) bool {
	for i := range s.SideAnswers {
		if s.SideAnswers[i].ID == sideAnswerID {
			s.SideAnswers[i].Promoted = true
			return true
		}
	}
	return false
}

func (s *Session) UnpromoteSideAnswer(sideAnswerID string) bool {
	for i := range s.SideAnswers {
		if s.SideAnswers[i].ID == sideAnswerID {
			s.SideAnswers[i].Promoted = false
			return true
		}
	}
	return false
}

func (s *Session) SetDigest(digest Digest) {
	s.Digest = digest
}

func (s *Session) AddTurnRevision(plan string, summary string) Revision {
	s.PendingProposal = nil
	return s.addRevision(s.CurrentRevisionID, RevisionSourceTurn, plan, Anchor{}, summary)
}

// AddExternalRevision records a change made to the source file outside of
// PlanMaxx. Unlike a normal turn revision, it preserves a pending proposal so
// that it cannot disappear merely because the source document changed.
func (s *Session) AddExternalRevision(plan string, summary string) Revision {
	return s.addRevision(s.CurrentRevisionID, RevisionSourceExternal, plan, Anchor{}, summary)
}

// ReconcileExternalPlan makes an externally edited source document the active
// revision without dropping reviewer work. A comment is moved only when its
// original selected text (or full-line anchor text for older comments) has one
// unambiguous match in the new document. Otherwise the comment remains intact
// and is marked stale for the reviewer to re-anchor deliberately.
func (s *Session) ReconcileExternalPlan(previousSource string, nextSource string) {
	for i := range s.Threads {
		thread := &s.Threads[i]
		selected := thread.SelectedText
		if selected == "" {
			// Legacy full-line anchors point at the mutable working revision.
			// Once it differs from the source baseline, guessing from source
			// line numbers could attach the comment to unrelated content.
			if s.Plan != previousSource {
				thread.Status = ThreadStatusStale
				continue
			}
			selected = textForAnchor(previousSource, thread.Anchor)
		}
		anchor, ok := uniqueAnchorForText(nextSource, selected, thread.Anchor)
		if !ok {
			thread.Status = ThreadStatusStale
			continue
		}
		thread.Anchor = anchor
	}
	if s.PendingProposal != nil {
		s.PendingProposal.Obsolete = true
	}
	s.AddExternalRevision(nextSource, "Plan file changed outside PlanMaxx")
}

func (s *Session) CreateSectionProposal(input SectionProposalInput) SectionProposal {
	s.NextProposalID++
	replacementAnchor := input.ReplacementAnchor
	if replacementAnchor.StartLine == 0 {
		replacementAnchor = replacementAnchorFromSection(input.Anchor, input.ProposedSection)
	}
	proposal := SectionProposal{
		ID:                fmt.Sprintf("proposal-%d", s.NextProposalID),
		ParentID:          s.CurrentRevisionID,
		ThreadID:          input.ThreadID,
		Anchor:            input.Anchor,
		AppliedAnchor:     input.AppliedAnchor,
		AppliedHunks:      append([]AppliedHunk(nil), input.AppliedHunks...),
		ReplacementAnchor: replacementAnchor,
		OriginalSection:   input.OriginalSection,
		ProposedSection:   input.ProposedSection,
		ProposedPlan:      input.ProposedPlan,
		Summary:           input.Summary,
		Instruction:       input.Instruction,
		RawResponse:       input.RawResponse,
		IncludedThreadIDs: append([]string(nil), input.IncludedThreadIDs...),
		CreatedAt:         time.Now().UTC(),
	}
	s.PendingProposal = &proposal
	return proposal
}

func (s *Session) ApplyProposal(proposalID string) (Revision, bool) {
	if s.PendingProposal == nil || s.PendingProposal.ID != proposalID {
		return Revision{}, false
	}
	proposal := *s.PendingProposal
	if proposal.Obsolete || proposal.ParentID != s.CurrentRevisionID {
		return Revision{}, false
	}
	delta := lineCount(proposal.ProposedPlan) - lineCount(s.Plan)
	revision := s.addRevision(proposal.ParentID, RevisionSourceImmediate, proposal.ProposedPlan, proposal.Anchor, proposal.Summary)
	s.adjustThreadsForAppliedProposal(proposal, delta)
	s.PendingProposal = nil
	return revision, true
}

func (s *Session) DiscardProposal(proposalID string) bool {
	if s.PendingProposal == nil || s.PendingProposal.ID != proposalID {
		return false
	}
	s.PendingProposal = nil
	return true
}

func (s *Session) CurrentRevision() (Revision, bool) {
	for _, revision := range s.Revisions {
		if revision.ID == s.CurrentRevisionID {
			return revision, true
		}
	}
	return Revision{}, false
}

func (s *Session) RestoreCounters() {
	maxMessage := 0
	maxRevision := 0
	maxProposal := 0
	maxThread := 0
	maxSideAnswer := 0
	for _, thread := range s.Threads {
		if n, ok := numberedID(thread.ID, "thread-"); ok && n > maxThread {
			maxThread = n
		}
		for _, message := range thread.Messages {
			raw, ok := strings.CutPrefix(message.ID, "msg-")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(raw)
			if err == nil && n > maxMessage {
				maxMessage = n
			}
		}
	}
	for _, answer := range s.SideAnswers {
		if n, ok := numberedID(answer.ID, "side-"); ok && n > maxSideAnswer {
			maxSideAnswer = n
		}
	}
	for _, revision := range s.Revisions {
		raw, ok := strings.CutPrefix(revision.ID, "rev-")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > maxRevision {
			maxRevision = n
		}
	}
	if s.PendingProposal != nil {
		raw, ok := strings.CutPrefix(s.PendingProposal.ID, "proposal-")
		if ok {
			n, err := strconv.Atoi(raw)
			if err == nil && n > maxProposal {
				maxProposal = n
			}
		}
	}
	s.nextMessage = maxMessage
	s.nextRevision = maxRevision
	if s.NextProposalID < maxProposal {
		s.NextProposalID = maxProposal
	}
	if s.NextThreadID < maxThread {
		s.NextThreadID = maxThread
	}
	if s.NextSideAnswerID < maxSideAnswer {
		s.NextSideAnswerID = maxSideAnswer
	}
	s.restoreThreadDefaults()
	s.restoreRevisionDefaults()
}

func numberedID(id string, prefix string) (int, bool) {
	raw, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil
}

func (s *Session) deleteSideAnswersForThread(threadID string) {
	kept := s.SideAnswers[:0]
	for _, answer := range s.SideAnswers {
		if answer.ThreadID != threadID {
			kept = append(kept, answer)
		}
	}
	s.SideAnswers = kept
}

func (s *Session) setThreadStatus(threadID string, status string) bool {
	for i := range s.Threads {
		if s.Threads[i].ID == threadID {
			s.Threads[i].Status = status
			return true
		}
	}
	return false
}

func (s *Session) addRevision(parentID string, source string, plan string, anchor Anchor, summary string) Revision {
	s.nextRevision++
	revision := Revision{
		ID:        fmt.Sprintf("rev-%d", s.nextRevision),
		ParentID:  parentID,
		Source:    source,
		CreatedAt: time.Now().UTC(),
		Plan:      plan,
		Anchor:    anchor,
		Summary:   summary,
	}
	s.Revisions = append(s.Revisions, revision)
	s.CurrentRevisionID = revision.ID
	s.Plan = plan
	return revision
}

func (s *Session) adjustThreadsForAppliedProposal(proposal SectionProposal, delta int) {
	if len(proposal.AppliedHunks) > 0 {
		s.adjustThreadsForAppliedHunks(proposal)
		return
	}
	applied := proposal.AppliedAnchor
	if applied.StartLine == 0 {
		// Sessions created before the explicit replacement protocol used Anchor
		// for both the requested and applied range.
		applied = proposal.Anchor
	}
	included := map[string]bool{}
	for _, threadID := range proposal.IncludedThreadIDs {
		included[threadID] = true
	}
	for i := range s.Threads {
		thread := &s.Threads[i]
		switch {
		case thread.Anchor.EndLine < applied.StartLine:
			continue
		case thread.Anchor.StartLine > applied.EndLine:
			thread.Anchor = shiftAnchor(thread.Anchor, delta)
		case included[thread.ID]:
			resolveThreadAfterProposal(thread, proposal.ReplacementAnchor)
		default:
			thread.Status = ThreadStatusStale
		}
	}
}

func (s *Session) adjustThreadsForAppliedHunks(proposal SectionProposal) {
	included := map[string]bool{}
	for _, threadID := range proposal.IncludedThreadIDs {
		included[threadID] = true
	}
	for i := range s.Threads {
		thread := &s.Threads[i]
		affected := false
		for _, hunk := range proposal.AppliedHunks {
			if anchorsOverlap(thread.Anchor, hunk.Anchor) {
				affected = true
				break
			}
		}
		if affected {
			if included[thread.ID] {
				resolveThreadAfterProposal(thread, replacementAnchorForThread(thread.Anchor, proposal))
			} else {
				thread.Status = ThreadStatusStale
			}
			continue
		}
		thread.Anchor = reanchorAfterHunks(thread.Anchor, proposal.AppliedHunks)
	}
}

// resolveThreadAfterProposal removes the old character selection because it
// described text in the parent revision. The thread remains as resolved review
// history, anchored only to the resulting lines for a useful location label.
func resolveThreadAfterProposal(thread *Thread, replacement Anchor) {
	thread.Status = ThreadStatusResolved
	thread.SelectedText = ""
	if replacement.StartLine > 0 {
		thread.Anchor = lineAnchor(replacement)
	}
}

func replacementAnchorForThread(anchor Anchor, proposal SectionProposal) Anchor {
	for _, hunk := range proposal.AppliedHunks {
		if anchorsOverlap(anchor, hunk.Anchor) {
			return hunk.Result
		}
	}
	return proposal.ReplacementAnchor
}

func lineAnchor(anchor Anchor) Anchor {
	return Anchor{StartLine: anchor.StartLine, EndLine: anchor.EndLine}
}

func anchorsOverlap(left, right Anchor) bool {
	if left.EndLine < right.StartLine || right.EndLine < left.StartLine {
		return false
	}
	if !hasCharacterRange(left) || !hasCharacterRange(right) || left.StartLine != left.EndLine || right.StartLine != right.EndLine {
		return true
	}
	return left.StartChar < right.EndChar && right.StartChar < left.EndChar
}

func reanchorAfterHunks(anchor Anchor, hunks []AppliedHunk) Anchor {
	startLine, startChar := positionAfterHunks(anchor.StartLine, anchor.StartChar, hunks)
	endLine, endChar := positionAfterHunks(anchor.EndLine, anchor.EndChar, hunks)
	anchor.StartLine, anchor.StartChar = startLine, startChar
	anchor.EndLine, anchor.EndChar = endLine, endChar
	return anchor
}

func positionAfterHunks(line, char int, hunks []AppliedHunk) (int, int) {
	shift := 0
	resultChar := char
	for _, hunk := range hunks {
		source := hunk.Anchor
		if line > source.EndLine {
			shift += hunk.LineDelta
			continue
		}
		if line != source.EndLine || !hasCharacterRange(source) || char < source.EndChar {
			continue
		}
		// A position immediately after a character-range hunk follows the
		// replacement's end, including any inserted/deleted lines. Result was
		// calculated from the final proposed plan, so this also handles multiple
		// non-overlapping hunks on the same source line.
		resultChar = hunk.Result.EndChar + char - source.EndChar
		shift += hunk.LineDelta
	}
	return line + shift, resultChar
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func replacementAnchorFromSection(anchor Anchor, section string) Anchor {
	lines := strings.Split(section, "\n")
	endLine := anchor.StartLine + len(lines) - 1
	if anchor.StartChar == 0 && anchor.EndChar == 0 {
		return Anchor{
			StartLine: anchor.StartLine,
			EndLine:   endLine,
		}
	}
	if len(lines) == 1 {
		return Anchor{
			StartLine: anchor.StartLine,
			StartChar: anchor.StartChar,
			EndLine:   anchor.StartLine,
			EndChar:   anchor.StartChar + utf16Length(lines[0]),
		}
	}
	return Anchor{
		StartLine: anchor.StartLine,
		StartChar: anchor.StartChar,
		EndLine:   endLine,
		EndChar:   utf16Length(lines[len(lines)-1]),
	}
}

func textForAnchor(plan string, anchor Anchor) string {
	lines := strings.Split(plan, "\n")
	if anchor.StartLine < 1 || anchor.EndLine < anchor.StartLine || anchor.EndLine > len(lines) {
		return ""
	}
	if anchor.StartChar == 0 && anchor.EndChar == 0 {
		return strings.Join(lines[anchor.StartLine-1:anchor.EndLine], "\n")
	}
	startLine := lines[anchor.StartLine-1]
	start, ok := utf16ByteOffset(startLine, anchor.StartChar)
	if !ok {
		return ""
	}
	endLine := lines[anchor.EndLine-1]
	end, ok := utf16ByteOffset(endLine, anchor.EndChar)
	if !ok || (anchor.StartLine == anchor.EndLine && end < start) {
		return ""
	}
	if anchor.StartLine == anchor.EndLine {
		return startLine[start:end]
	}
	parts := []string{startLine[start:]}
	parts = append(parts, lines[anchor.StartLine:anchor.EndLine-1]...)
	parts = append(parts, endLine[:end])
	return strings.Join(parts, "\n")
}

func uniqueAnchorForText(plan string, selected string, original Anchor) (Anchor, bool) {
	if selected == "" {
		return Anchor{}, false
	}
	index := -1
	if original.StartChar == 0 && original.EndChar == 0 {
		index = uniqueLineSequenceIndex(plan, selected)
	} else {
		index = uniqueSubstringIndex(plan, selected)
	}
	if index < 0 {
		return Anchor{}, false
	}
	before := plan[:index]
	startLine := strings.Count(before, "\n") + 1
	startColumnStart := strings.LastIndex(before, "\n") + 1
	startChar := utf16Length(before[startColumnStart:])
	if original.StartChar == 0 && original.EndChar == 0 {
		return Anchor{StartLine: startLine, EndLine: startLine + strings.Count(selected, "\n")}, true
	}
	selectedLines := strings.Split(selected, "\n")
	if len(selectedLines) == 1 {
		return Anchor{StartLine: startLine, StartChar: startChar, EndLine: startLine, EndChar: startChar + utf16Length(selected)}, true
	}
	return Anchor{
		StartLine: startLine,
		StartChar: startChar,
		EndLine:   startLine + len(selectedLines) - 1,
		EndChar:   utf16Length(selectedLines[len(selectedLines)-1]),
	}, true
}

func uniqueSubstringIndex(text string, selected string) int {
	first := strings.Index(text, selected)
	if first < 0 {
		return -1
	}
	if strings.Index(text[first+1:], selected) >= 0 {
		return -1
	}
	return first
}

func uniqueLineSequenceIndex(plan string, selected string) int {
	planLines := strings.Split(plan, "\n")
	selectedLines := strings.Split(selected, "\n")
	matchAt := -1
	for start := 0; start+len(selectedLines) <= len(planLines); start++ {
		if strings.Join(planLines[start:start+len(selectedLines)], "\n") != selected {
			continue
		}
		if matchAt >= 0 {
			return -1
		}
		matchAt = start
	}
	if matchAt < 0 {
		return -1
	}
	if matchAt == 0 {
		return 0
	}
	return len(strings.Join(planLines[:matchAt], "\n")) + 1
}

func utf16ByteOffset(text string, offset int) (int, bool) {
	if offset < 0 {
		return 0, false
	}
	if offset == 0 {
		return 0, true
	}
	units := 0
	for index, runeValue := range text {
		if units == offset {
			return index, true
		}
		units += len(utf16.Encode([]rune{runeValue}))
		if units == offset {
			return index + len(string(runeValue)), true
		}
		if units > offset {
			return 0, false
		}
	}
	return 0, units == offset
}

func utf16Length(text string) int {
	return len(utf16.Encode([]rune(text)))
}

func shiftAnchor(anchor Anchor, delta int) Anchor {
	anchor.StartLine += delta
	anchor.EndLine += delta
	if anchor.StartLine < 1 {
		anchor.StartLine = 1
	}
	if anchor.EndLine < anchor.StartLine {
		anchor.EndLine = anchor.StartLine
	}
	return anchor
}

func (s *Session) restoreThreadDefaults() {
	for i := range s.Threads {
		if s.Threads[i].Kind == "" {
			s.Threads[i].Kind = ThreadKindDecision
		}
		if s.Threads[i].Status == "" {
			s.Threads[i].Status = ThreadStatusOpen
		}
	}
}

func (s *Session) restoreRevisionDefaults() {
	if len(s.Revisions) == 0 {
		s.addRevision("", RevisionSourceInitial, s.Plan, Anchor{}, "Initial plan")
		return
	}
	if s.CurrentRevisionID == "" {
		s.CurrentRevisionID = s.Revisions[len(s.Revisions)-1].ID
	}
	if s.nextRevision == 0 {
		s.nextRevision = len(s.Revisions)
	}
}

func (s *Session) newMessage(author string, body string) Message {
	now := time.Now().UTC()
	s.nextMessage++
	return Message{
		ID:        fmt.Sprintf("msg-%d", s.nextMessage),
		Author:    author,
		Body:      body,
		CreatedAt: now,
	}
}
