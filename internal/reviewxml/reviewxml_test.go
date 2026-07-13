package reviewxml

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestHandoffAnnotatesOverlappingCharacterRangesWithoutInvalidXML(t *testing.T) {
	s := session.New("review-1", "abcdef\nemoji: 😀 <tag> & value")
	s.PlanPath = "/repo/plan.md"
	first := s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, StartChar: 1, EndLine: 1, EndChar: 4}, "First <message>", "bcd")
	second := s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, StartChar: 3, EndLine: 1, EndChar: 6}, "Second & message", "def")
	third := s.AddThreadWithSelectedText(session.Anchor{StartLine: 2, StartChar: 7, EndLine: 2, EndChar: 9}, "Emoji", "😀")
	_ = third

	got := Handoff(*s)
	if err := xml.Unmarshal([]byte(got), new(any)); err != nil {
		t.Fatalf("expected valid XML, got %v\n%s", err, got)
	}
	for _, want := range []string{
		`<review_target threads="thread-1">bc</review_target>`,
		`<review_target threads="thread-1 thread-2">d</review_target>`,
		`<review_target threads="thread-2">ef</review_target>`,
		`<review_target threads="thread-3">😀</review_target>`,
		"&lt;tag&gt; &amp; value",
		"First &lt;message&gt;",
		"Second &amp; message",
		`target="1:2-1:5"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected XML to contain %q\n%s", want, got)
		}
	}
	if first.ID != "thread-1" || second.ID != "thread-2" {
		t.Fatalf("unexpected thread IDs %q and %q", first.ID, second.ID)
	}
}

func TestHandoffDeclaresHTMLFormatAndEscapesSource(t *testing.T) {
	s := session.NewWithFormat("review-1", "<h1>Plan &amp; rollout</h1>", planformat.HTML)
	s.PlanPath = "/repo/plan.html"
	got := Handoff(*s)
	if err := xml.Unmarshal([]byte(got), new(any)); err != nil {
		t.Fatalf("expected valid XML, got %v\n%s", err, got)
	}
	for _, want := range []string{`format="html"`, `file="/repo/plan.html"`, `&lt;h1&gt;Plan &amp;amp; rollout&lt;/h1&gt;`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected HTML review XML to contain %q\n%s", want, got)
		}
	}
}

func TestHandoffOmitsPrivateAndResolvedThreadsButKeepsPromotedAnswerContext(t *testing.T) {
	s := session.New("review-1", "- One\n- Two\n- Three")
	open := s.AddThreadWithSelectedText(session.Anchor{StartLine: 1, EndLine: 1}, "Open decision", "- One")
	note := s.AddThreadWithSelectedText(session.Anchor{StartLine: 2, EndLine: 2}, "Private note", "- Two")
	resolved := s.AddThreadWithSelectedText(session.Anchor{StartLine: 3, EndLine: 3}, "Resolved", "- Three")
	s.SetThreadKind(note.ID, session.ThreadKindNote)
	s.ResolveThread(resolved.ID)
	promoted := s.AddSideAnswer(note.ID, "Why keep this?", "Because it is useful.")
	s.PromoteSideAnswer(promoted.ID)

	got := Handoff(*s)
	if !strings.Contains(got, `id="`+open.ID+`"`) {
		t.Fatalf("expected open decision in handoff\n%s", got)
	}
	if strings.Contains(got, "Resolved") {
		t.Fatalf("expected resolved thread omitted\n%s", got)
	}
	if strings.Contains(got, "Private note") {
		t.Fatalf("expected private note message omitted\n%s", got)
	}
	if !strings.Contains(got, "Because it is useful.") || !strings.Contains(got, `id="`+note.ID+`"`) {
		t.Fatalf("expected promoted answer context to retain its thread\n%s", got)
	}
}

func TestIterationIncludesExactTargetAndAllRelevantReviewThreads(t *testing.T) {
	threads := []session.Thread{
		{ID: "inside", Anchor: session.Anchor{StartLine: 4, EndLine: 4}, Kind: session.ThreadKindDecision, Status: session.ThreadStatusOpen, Messages: []session.Message{{Body: "Inside"}}},
		{ID: "outside", Anchor: session.Anchor{StartLine: 8, EndLine: 8}, Kind: session.ThreadKindDecision, Status: session.ThreadStatusOpen, Messages: []session.Message{{Body: "Outside"}}},
		{ID: "private", Anchor: session.Anchor{StartLine: 3, EndLine: 3}, Kind: session.ThreadKindNote, Status: session.ThreadStatusOpen, Messages: []session.Message{{Body: "Private"}}},
	}
	got := Iteration(IterationInput{
		RevisionID:  "rev-4",
		FilePath:    "/repo/plan.md",
		Plan:        "one\ntwo\n- rough wording\nfour\nfive\nsix\nseven\neight",
		Target:      session.Anchor{StartLine: 3, StartChar: 2, EndLine: 3, EndChar: 7},
		Instruction: "Polish it",
		Threads:     threads,
	})
	for _, want := range []string{
		`revision="rev-4"`,
		`source="3:3-3:8"`,
		`<review_target target="selection">rough</review_target>`,
		"Polish it",
		`id="inside"`,
		`id="outside"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected iteration XML to contain %q\n%s", want, got)
		}
	}
	if strings.Contains(got, `id="private"`) {
		t.Fatalf("unexpected unrelated thread in iteration XML\n%s", got)
	}
}

func TestSideQuestionEscapesUntrustedText(t *testing.T) {
	got := SideQuestion(SideQuestionInput{
		FilePath:     "/repo/<plan>.md",
		Reference:    "2:3-2:6",
		SelectedText: "<CLI>",
		Question:     "Can </target> escape?",
		PlanExcerpt:  "a & b",
	})
	if err := xml.Unmarshal([]byte(got), new(any)); err != nil {
		t.Fatalf("expected valid XML, got %v\n%s", err, got)
	}
	for _, want := range []string{"&lt;CLI&gt;", "&lt;/target&gt;", "a &amp; b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected escaped value %q\n%s", want, got)
		}
	}
}

func TestHandoffUsesTheAnchoredTextOverTrimmedStoredSelection(t *testing.T) {
	s := session.New("review-1", "- keep surrounding spaces ")
	s.AddThreadWithSelectedText(
		session.Anchor{StartLine: 1, StartChar: 6, EndLine: 1, EndChar: 26},
		"Comment",
		"keep surrounding spa",
	)
	got := Handoff(*s)
	if !strings.Contains(got, `<selected_text> surrounding spaces </selected_text>`) {
		t.Fatalf("expected current anchored text, including its exact trailing space\n%s", got)
	}
}
