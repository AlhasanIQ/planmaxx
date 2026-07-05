package diff

import "testing"

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
