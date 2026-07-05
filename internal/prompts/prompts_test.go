package prompts

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPromptTemplatesAreReviewableFiles(t *testing.T) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)

	want := []string{
		"approved_handoff.gotmpl",
		"rejected_handoff.gotmpl",
		"review_digest.gotmpl",
		"section_iteration.gotmpl",
		"side_question.gotmpl",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("expected prompt templates %v, got %v", want, names)
	}
}

func TestAgentPromptTextStaysInTemplates(t *testing.T) {
	root := repoRoot(t)
	checks := map[string][]string{
		"internal/appserver/client.go": {
			"PlanMaxx side question",
			"Answer the reviewer question about the selected plan text.",
		},
		"internal/digest/digest.go": {
			"Create a compact PlanMaxx review digest.",
			"Keep explicit reviewer decisions separate from agent-generated plan text.",
		},
		"internal/handoff/handoff.go": {
			"Continue from this approved PlanMaxx review.",
			"PlanMaxx review rejected.",
			"The user rejected this plan with comments.",
		},
	}

	for rel, markers := range checks {
		content, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if strings.Contains(string(content), marker) {
				t.Fatalf("expected %q prompt text to live in internal/prompts/templates, found in %s", marker, rel)
			}
		}
	}
}

func TestReviewDigestRendersTemplate(t *testing.T) {
	prompt := ReviewDigest(
		"# Plan",
		[]string{"Use Cobra for CLI."},
		[]string{"Cobra gives clean subcommands."},
	)

	for _, want := range []string{
		"Create a compact PlanMaxx review digest.",
		"Agent-generated plan:\n# Plan",
		"Reviewer decisions:\nUse Cobra for CLI.",
		"Promoted side-question answers:\nCobra gives clean subcommands.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected digest prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestSectionIterationRendersTemplate(t *testing.T) {
	prompt := SectionIteration(SectionIterationInput{
		RevisionID:          "rev-2",
		FilePath:            "/repo/plan.md",
		Reference:           "/repo/plan.md:4-8",
		SelectedSection:     "## Phase 1\n\n- Old step",
		PlanExcerpt:         "## Phase 1\n\n- Old step\n\n## Phase 2",
		ReviewerInstruction: "Clarify rollout order.",
		ReviewerDecisions:   []string{"Ship CLI before UI polish."},
		PromotedSideAnswers: []string{"Question:\nWhy CLI?\nAnswer:\nIt gates the UI."},
	})

	for _, want := range []string{
		"PlanMaxx section iteration",
		"rev-2",
		"/repo/plan.md",
		"/repo/plan.md:4-8",
		"## Phase 1",
		"Clarify rollout order.",
		"Ship CLI before UI polish.",
		"Why CLI?",
		"Return only",
		"fenced markdown replacement section",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected section iteration prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestSideQuestionRendersTemplate(t *testing.T) {
	prompt := SideQuestion(
		"What should move first?",
		"/repo/plan.md",
		"/repo/plan.md:2:3-2:5",
		"UI",
		"1. CLI\n2. UI",
	)

	for _, want := range []string{
		"PlanMaxx side question",
		"What should move first?",
		"/repo/plan.md",
		"/repo/plan.md:2:3-2:5",
		"UI",
		"1. CLI",
		"2. UI",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected side-question prompt to contain %q\n%s", want, prompt)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
