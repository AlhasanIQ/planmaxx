package patches

import "testing"

func TestResolveRecoversWrongHintsAndAppliesMultipleHunksAtomically(t *testing.T) {
	base := "# Plan\n- Name: Old\n- Keep\n- Again: Old\n- End"
	resolved, err := Resolve(base, []Hunk{{Before: "# Plan", Expected: "- Name: Old", After: "- Keep", Content: "- Name: New", StartHint: 99, EndHint: 99}, {Before: "- Keep", Expected: "- Again: Old", After: "- End", Content: "- Again: New"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "# Plan\n- Name: New\n- Keep\n- Again: New\n- End" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveCharacterRangeUsesExactAdjacentContextAndUnicodePositions(t *testing.T) {
	base := "- Name: 😀 old"
	resolved, err := Resolve(base, []Hunk{{Target: "selection", Before: "- Name: 😀 ", Expected: "old", Content: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "- Name: 😀 new" {
		t.Fatalf("got %q", got)
	}
	if got := resolved[0]; got.StartLine != 1 || got.StartChar != 11 || got.EndLine != 1 || got.EndChar != 14 {
		t.Fatalf("unexpected UTF-16 anchor %+v", got)
	}
}

func TestResolveAllowsEmptyReplacementForDeletion(t *testing.T) {
	resolved, err := Resolve("one obsolete two", []Hunk{{Before: "one ", Expected: "obsolete ", After: "two", Content: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply("one obsolete two", resolved); got != "one two" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveRemovesLineDelimiterForFullLineDeletion(t *testing.T) {
	tests := map[string]struct {
		base string
		want string
	}{
		"middle": {base: "one\nremove\nthree", want: "one\nthree"},
		"first":  {base: "remove\ntwo\nthree", want: "two\nthree"},
		"last":   {base: "one\ntwo\nremove", want: "one\ntwo"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resolved, err := Resolve(test.base, []Hunk{{Target: "lines", Expected: "remove", Content: ""}})
			if err != nil {
				t.Fatal(err)
			}
			if got := Apply(test.base, resolved); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveDoesNotExpandMidLineDeletionLabeledAsLines(t *testing.T) {
	base := "say remove\nkeep"
	resolved, err := Resolve(base, []Hunk{{Target: "lines", Expected: "remove", Content: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "say \nkeep" {
		t.Fatalf("mid-line deletion consumed a delimiter: %q", got)
	}
}

func TestResolveRemovesCRLFDelimiterForFullLineDeletion(t *testing.T) {
	base := "one\r\nremove\r\nthree"
	resolved, err := Resolve(base, []Hunk{{Target: "lines", Expected: "remove", Content: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "one\r\nthree" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveAdjacentTrailingLineDeletionsDoNotOverlap(t *testing.T) {
	base := "a\nb\nc"
	resolved, err := Resolve(base, []Hunk{
		{Target: "lines", Expected: "b", Content: ""},
		{Target: "lines", Expected: "c", Content: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveLineDeletionPreservesExistingTerminalNewline(t *testing.T) {
	tests := map[string]struct {
		base  string
		hunks []Hunk
		want  string
	}{
		"single LF":     {base: "a\nb\n", hunks: []Hunk{{Target: "lines", Expected: "b", Content: ""}}, want: "a\n"},
		"adjacent LF":   {base: "a\nb\nc\n", hunks: []Hunk{{Target: "lines", Expected: "b", Content: ""}, {Target: "lines", Expected: "c", Content: ""}}, want: "a\n"},
		"blank before":  {base: "a\n\nremove", hunks: []Hunk{{Target: "lines", Expected: "remove", Content: ""}}, want: "a\n"},
		"single CRLF":   {base: "a\r\nb\r\n", hunks: []Hunk{{Target: "lines", Expected: "b", Content: ""}}, want: "a\r\n"},
		"adjacent CRLF": {base: "a\r\nb\r\nc\r\n", hunks: []Hunk{{Target: "lines", Expected: "b", Content: ""}, {Target: "lines", Expected: "c", Content: ""}}, want: "a\r\n"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resolved, err := Resolve(test.base, test.hunks)
			if err != nil {
				t.Fatal(err)
			}
			if got := Apply(test.base, resolved); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveUsesUniqueExpectedTextWhenModelContextIsWrong(t *testing.T) {
	base := "# Plan\n- Product name: Old\n- Keep"
	resolved, err := Resolve(base, []Hunk{
		{
			Before:   "# Wrong heading",
			Expected: "- Product name: Old",
			After:    "- Also wrong",
			Content:  "- Product name: New",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Apply(base, resolved); got != "# Plan\n- Product name: New\n- Keep" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveRejectsAmbiguousAndOverlappingHunks(t *testing.T) {
	base := "before\nold\nafter\nbefore\nold\nafter"
	if _, err := Resolve(base, []Hunk{{Expected: "old", Content: "new"}}); err == nil {
		t.Fatal("expected ambiguity")
	}
	if _, err := Resolve(base, []Hunk{{Before: "wrong", Expected: "old", After: "also wrong", Content: "new"}}); err == nil {
		t.Fatal("expected ambiguous exact source to remain rejected")
	}
	base = "before\none\ntwo\nafter"
	if _, err := Resolve(base, []Hunk{{Before: "before", Expected: "one\ntwo", After: "after", Content: "x"}, {Before: "before", Expected: "one", After: "two", Content: "y"}}); err == nil {
		t.Fatal("expected overlap")
	}
}
