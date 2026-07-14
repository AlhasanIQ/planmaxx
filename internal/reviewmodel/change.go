package reviewmodel

import (
	"fmt"
	"strings"

	plandiff "github.com/AlhasanIQ/planmaxx/internal/diff"
	"github.com/AlhasanIQ/planmaxx/internal/session"
)

const (
	ModeProposal = "proposal"
	ModeRevision = "revision"
)

// DocumentSnapshot keeps the original document intact while exposing the
// normalized logical-line contract used by review projections.
type DocumentSnapshot struct {
	Format          string   `json:"format"`
	Text            string   `json:"text"`
	Lines           []string `json:"lines"`
	TerminalNewline bool     `json:"terminalNewline"`
}

type ChangeView struct {
	Mode               string                     `json:"mode"`
	IsDirect           bool                       `json:"isDirect"`
	BaseID             string                     `json:"baseId"`
	TargetID           string                     `json:"targetId"`
	Before             DocumentSnapshot           `json:"before"`
	After              DocumentSnapshot           `json:"after"`
	Rows               []ChangeRow                `json:"rows"`
	Clusters           []ChangeCluster            `json:"clusters"`
	ThreadPlacements   []ThreadPlacement          `json:"threadPlacements"`
	Feedback           []session.RevisionFeedback `json:"feedback"`
	FeedbackPlacements []FeedbackPlacement        `json:"feedbackPlacements"`
}

type ChangeRow struct {
	ID        string `json:"id"`
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	Before    int    `json:"before,omitempty"`
	After     int    `json:"after,omitempty"`
	Text      string `json:"text"`
	ClusterID string `json:"clusterId,omitempty"`
}

type ChangeCluster struct {
	ID          string `json:"id"`
	FirstRow    int    `json:"firstRow"`
	LastRow     int    `json:"lastRow"`
	BeforeStart int    `json:"beforeStart,omitempty"`
	BeforeEnd   int    `json:"beforeEnd,omitempty"`
	AfterStart  int    `json:"afterStart,omitempty"`
	AfterEnd    int    `json:"afterEnd,omitempty"`
}

type ThreadPlacement struct {
	ThreadID string `json:"threadId"`
	RowID    string `json:"rowId"`
	RowIndex int    `json:"rowIndex"`
}

type FeedbackPlacement struct {
	RevisionID string `json:"revisionId"`
	ThreadID   string `json:"threadId"`
	RowID      string `json:"rowId"`
	RowIndex   int    `json:"rowIndex"`
}

type BuildInput struct {
	Mode     string
	IsDirect bool
	BaseID   string
	TargetID string
	Format   string
	Before   string
	After    string
	Threads  []session.Thread
	Feedback []session.RevisionFeedback
}

func Build(input BuildInput) ChangeView {
	view := ChangeView{
		Mode:               input.Mode,
		IsDirect:           input.IsDirect,
		BaseID:             input.BaseID,
		TargetID:           input.TargetID,
		Before:             snapshot(input.Format, input.Before),
		After:              snapshot(input.Format, input.After),
		Rows:               []ChangeRow{},
		Clusters:           []ChangeCluster{},
		ThreadPlacements:   []ThreadPlacement{},
		Feedback:           cloneFeedback(input.Feedback),
		FeedbackPlacements: []FeedbackPlacement{},
	}
	for index, line := range plandiff.Lines(normalizeNewlines(input.Before), normalizeNewlines(input.After)) {
		view.Rows = append(view.Rows, ChangeRow{
			ID:     fmt.Sprintf("row-%d", index+1),
			Index:  index,
			Kind:   line.Kind,
			Before: line.Before,
			After:  line.After,
			Text:   line.Text,
		})
	}
	view.Clusters = buildClusters(view.Rows)
	for _, cluster := range view.Clusters {
		for index := cluster.FirstRow; index <= cluster.LastRow; index++ {
			view.Rows[index].ClusterID = cluster.ID
		}
	}
	view.ThreadPlacements = placeThreads(input.Mode, input.Threads, view.Rows, view.Clusters)
	if input.Mode != ModeRevision || input.IsDirect {
		view.FeedbackPlacements = placeFeedback(view.Feedback, view.Rows, view.Clusters)
	}
	return view
}

// WithThreadPlacements overlays mutable current-review threads onto an
// otherwise immutable revision ChangeView. This keeps cached revision diffs
// independent from comments added after the comparison was first requested.
func WithThreadPlacements(view ChangeView, threads []session.Thread) ChangeView {
	view.ThreadPlacements = placeThreads(view.Mode, threads, view.Rows, view.Clusters)
	return view
}

func snapshot(format, text string) DocumentSnapshot {
	normalized := normalizeNewlines(text)
	terminal := strings.HasSuffix(normalized, "\n")
	logical := normalized
	if terminal {
		logical = strings.TrimSuffix(logical, "\n")
	}
	lines := []string{}
	if logical != "" {
		lines = strings.Split(logical, "\n")
	}
	return DocumentSnapshot{Format: format, Text: text, Lines: lines, TerminalNewline: terminal}
}

func normalizeNewlines(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func buildClusters(rows []ChangeRow) []ChangeCluster {
	clusters := []ChangeCluster{}
	for index := 0; index < len(rows); {
		if rows[index].Kind == plandiff.KindContext {
			index++
			continue
		}
		start, end := index, index
		for end+1 < len(rows) {
			if rows[end+1].Kind != plandiff.KindContext {
				end++
				continue
			}
			nextChange := end + 1
			for nextChange < len(rows) && rows[nextChange].Kind == plandiff.KindContext && strings.TrimSpace(rows[nextChange].Text) == "" {
				nextChange++
			}
			if nextChange < len(rows) && rows[nextChange].Kind != plandiff.KindContext {
				end = nextChange
				continue
			}
			break
		}
		cluster := ChangeCluster{ID: fmt.Sprintf("cluster-%d", len(clusters)+1), FirstRow: start, LastRow: end}
		for rowIndex := start; rowIndex <= end; rowIndex++ {
			row := rows[rowIndex]
			if row.Before > 0 {
				cluster.BeforeStart = firstPositive(cluster.BeforeStart, row.Before)
				cluster.BeforeEnd = max(cluster.BeforeEnd, row.Before)
			}
			if row.After > 0 {
				cluster.AfterStart = firstPositive(cluster.AfterStart, row.After)
				cluster.AfterEnd = max(cluster.AfterEnd, row.After)
			}
		}
		clusters = append(clusters, cluster)
		index = end + 1
	}
	return clusters
}

func placeThreads(mode string, threads []session.Thread, rows []ChangeRow, clusters []ChangeCluster) []ThreadPlacement {
	placements := make([]ThreadPlacement, 0, len(threads))
	for _, thread := range threads {
		rowIndex := -1
		for _, cluster := range clusters {
			clusterStart, clusterEnd := cluster.BeforeStart, cluster.BeforeEnd
			if mode == ModeRevision {
				clusterStart, clusterEnd = cluster.AfterStart, cluster.AfterEnd
			}
			if overlaps(thread.Anchor.StartLine, thread.Anchor.EndLine, clusterStart, clusterEnd) {
				rowIndex = max(rowIndex, cluster.LastRow)
			}
		}
		if rowIndex < 0 {
			for index, row := range rows {
				line := row.Before
				valid := row.Kind != plandiff.KindAdd
				if mode == ModeRevision {
					line, valid = row.After, row.Kind != plandiff.KindRemove
				}
				if valid && line == thread.Anchor.EndLine {
					rowIndex = index
				}
			}
		}
		if rowIndex >= 0 {
			placements = append(placements, ThreadPlacement{ThreadID: thread.ID, RowID: rows[rowIndex].ID, RowIndex: rowIndex})
		}
	}
	return placements
}

func placeFeedback(feedback []session.RevisionFeedback, rows []ChangeRow, clusters []ChangeCluster) []FeedbackPlacement {
	placements := make([]FeedbackPlacement, 0, len(feedback))
	for _, item := range feedback {
		rowIndex := -1
		for _, cluster := range clusters {
			if overlaps(item.Anchor.StartLine, item.Anchor.EndLine, cluster.BeforeStart, cluster.BeforeEnd) ||
				overlaps(item.ResultAnchor.StartLine, item.ResultAnchor.EndLine, cluster.AfterStart, cluster.AfterEnd) {
				rowIndex = max(rowIndex, cluster.LastRow)
			}
		}
		if rowIndex < 0 && item.ResultAnchor.EndLine > 0 {
			for index, row := range rows {
				if row.After == item.ResultAnchor.EndLine {
					rowIndex = index
				}
			}
		}
		if rowIndex >= 0 {
			placements = append(placements, FeedbackPlacement{RevisionID: item.RevisionID, ThreadID: item.ThreadID, RowID: rows[rowIndex].ID, RowIndex: rowIndex})
		}
	}
	return placements
}

func cloneFeedback(source []session.RevisionFeedback) []session.RevisionFeedback {
	out := make([]session.RevisionFeedback, len(source))
	copy(out, source)
	for index := range out {
		out[index].Messages = append([]session.Message(nil), source[index].Messages...)
	}
	return out
}

func overlaps(start, end, otherStart, otherEnd int) bool {
	return start > 0 && end >= start && otherStart > 0 && otherEnd >= otherStart && start <= otherEnd && otherStart <= end
}

func firstPositive(current, candidate int) int {
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}
