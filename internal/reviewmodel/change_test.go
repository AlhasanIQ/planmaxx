package reviewmodel

import (
	"strings"
	"testing"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

func TestBuildReconstructsDocumentsAndTracksTerminalNewlines(t *testing.T) {
	before := "alpha\r\nbeta\r\n"
	after := "alpha\r\ngamma\r\n"
	view := Build(BuildInput{Mode: ModeRevision, BaseID: "rev-1", TargetID: "rev-2", Format: "markdown", Before: before, After: after})
	if view.Before.Text != before || view.After.Text != after {
		t.Fatal("snapshot changed original document text")
	}
	if !view.Before.TerminalNewline || !view.After.TerminalNewline {
		t.Fatal("expected explicit terminal newline metadata")
	}
	if got := reconstruct(view.Rows, false); got != "alpha\nbeta\n" {
		t.Fatalf("before reconstruction = %q", got)
	}
	if got := reconstruct(view.Rows, true); got != "alpha\ngamma\n" {
		t.Fatalf("after reconstruction = %q", got)
	}
}

func TestBuildClustersBlankContextAndPlacesOverlappingThreadsAfterAddition(t *testing.T) {
	before := "head\nremove one\n\nremove two\ntail"
	after := "head\nadd one\n\nadd two\ntail"
	threads := []session.Thread{
		{ID: "thread-1", Anchor: session.Anchor{StartLine: 2, EndLine: 3}},
		{ID: "thread-2", Anchor: session.Anchor{StartLine: 3, EndLine: 4}},
	}
	view := Build(BuildInput{Mode: ModeProposal, BaseID: "rev-1", TargetID: "proposal-1", Before: before, After: after, Threads: threads})
	if len(view.Clusters) != 1 {
		t.Fatalf("clusters = %+v", view.Clusters)
	}
	if len(view.ThreadPlacements) != 2 {
		t.Fatalf("placements = %+v", view.ThreadPlacements)
	}
	last := view.Clusters[0].LastRow
	for _, placement := range view.ThreadPlacements {
		if placement.RowIndex != last || view.Rows[placement.RowIndex].Kind != "add" {
			t.Fatalf("thread placed before complete replacement: %+v", placement)
		}
	}
}

func TestBuildPlacesFeedbackAfterChangedCluster(t *testing.T) {
	feedback := []session.RevisionFeedback{{
		RevisionID: "rev-2", ThreadID: "thread-1",
		Anchor: session.Anchor{StartLine: 2, EndLine: 2}, ResultAnchor: session.Anchor{StartLine: 2, EndLine: 2},
	}}
	view := Build(BuildInput{Mode: ModeRevision, IsDirect: true, Before: "a\nold\nz", After: "a\nnew\nz", Feedback: feedback})
	if len(view.FeedbackPlacements) != 1 || view.FeedbackPlacements[0].RowIndex != view.Clusters[0].LastRow {
		t.Fatalf("feedback placements = %+v", view.FeedbackPlacements)
	}
}

func TestBuildKeepsMultiRevisionFeedbackAsSummaryOnly(t *testing.T) {
	feedback := []session.RevisionFeedback{{
		RevisionID: "rev-2", ThreadID: "thread-1",
		Anchor: session.Anchor{StartLine: 2, EndLine: 2}, ResultAnchor: session.Anchor{StartLine: 2, EndLine: 2},
	}}
	view := Build(BuildInput{Mode: ModeRevision, IsDirect: false, Before: "a\nold\nz", After: "a\nnew\nz", Feedback: feedback})
	if len(view.Feedback) != 1 || len(view.FeedbackPlacements) != 0 {
		t.Fatalf("multi-hop feedback should remain available without misleading row coordinates: %+v", view)
	}
}

func reconstruct(rows []ChangeRow, after bool) string {
	parts := []string{}
	for _, row := range rows {
		if (!after && row.Kind == "add") || (after && row.Kind == "remove") {
			continue
		}
		parts = append(parts, row.Text)
	}
	return strings.Join(parts, "\n")
}

func FuzzBuildReconstructsNormalizedDocuments(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"a\n", "a\nb\n"}, {"\nstart", "start\n"}, {"same\n\nend", "same\nchanged\nend"}, {"emoji 😀", "emoji 🚀"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, before, after string) {
		if strings.Contains(before, "\r") || strings.Contains(after, "\r") {
			t.Skip()
		}
		view := Build(BuildInput{Mode: ModeRevision, Before: before, After: after})
		if got := reconstruct(view.Rows, false); got != before {
			t.Fatalf("before reconstruction mismatch: %q != %q", got, before)
		}
		if got := reconstruct(view.Rows, true); got != after {
			t.Fatalf("after reconstruction mismatch: %q != %q", got, after)
		}
	})
}
