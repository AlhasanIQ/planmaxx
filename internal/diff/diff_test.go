package diff

import (
	"fmt"
	"strings"
	"testing"
)

func TestLinesReturnsContextForUnchangedText(t *testing.T) {
	got := Lines("alpha\nbeta", "alpha\nbeta")

	want := []Line{
		{Kind: KindContext, Before: 1, After: 1, Text: "alpha"},
		{Kind: KindContext, Before: 2, After: 2, Text: "beta"},
	}
	assertLines(t, got, want)
}

func TestLinesMarksAdditionsAndRemovals(t *testing.T) {
	got := Lines("alpha\nbeta\ndelta", "alpha\ngamma\ndelta\nepsilon")

	want := []Line{
		{Kind: KindContext, Before: 1, After: 1, Text: "alpha"},
		{Kind: KindRemove, Before: 2, Text: "beta"},
		{Kind: KindAdd, After: 2, Text: "gamma"},
		{Kind: KindContext, Before: 3, After: 3, Text: "delta"},
		{Kind: KindAdd, After: 4, Text: "epsilon"},
	}
	assertLines(t, got, want)
}

func TestLinesPreservesTrailingBlankLineAsEmptyLine(t *testing.T) {
	got := Lines("alpha\n", "alpha\nbeta\n")

	want := []Line{
		{Kind: KindContext, Before: 1, After: 1, Text: "alpha"},
		{Kind: KindAdd, After: 2, Text: "beta"},
		{Kind: KindContext, Before: 2, After: 3, Text: ""},
	}
	assertLines(t, got, want)
}

func TestLinesRecoversLargeRepeatedPlan(t *testing.T) {
	before := repeatedPlan(8_000, "before")
	after := repeatedPlan(8_000, "after")
	lines := Lines(before, after)
	if got := reconstruct(lines, KindAdd); got != before {
		t.Fatal("removed and context lines did not recover the original plan")
	}
	if got := reconstruct(lines, KindRemove); got != after {
		t.Fatal("added and context lines did not recover the revised plan")
	}
}

func BenchmarkLinesLargePlan(b *testing.B) {
	before := repeatedPlan(12_000, "before")
	after := repeatedPlan(12_000, "after")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lines(before, after)
	}
}

func repeatedPlan(lines int, changed string) string {
	var out strings.Builder
	for index := 0; index < lines; index++ {
		if index%700 == 0 {
			fmt.Fprintf(&out, "- %s milestone %d\n", changed, index)
			continue
		}
		out.WriteString("- Repeated implementation detail\n")
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func reconstruct(lines []Line, excluded string) string {
	var out []string
	for _, line := range lines {
		if line.Kind != excluded {
			out = append(out, line.Text)
		}
	}
	return strings.Join(out, "\n")
}

func assertLines(t *testing.T, got []Line, want []Line) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d\nwant: %+v\ngot:  %+v", len(want), len(got), want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d mismatch\nwant: %+v\ngot:  %+v\nall:  %+v", i, want[i], got[i], got)
		}
	}
}
