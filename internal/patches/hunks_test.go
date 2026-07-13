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
