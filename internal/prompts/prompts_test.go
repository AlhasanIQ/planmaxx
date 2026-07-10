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
		"protocol.gotmpl",
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
		`<planmaxx_review version="1"/>`,
	)

	for _, want := range []string{
		"Create a compact PlanMaxx review digest.",
		"PlanMaxx protocol documentation (v2)",
		"Agent-generated plan:\n# Plan",
		"Reviewer decisions:\nUse Cobra for CLI.",
		"Promoted side-question answers:\nCobra gives clean subcommands.",
		"Model-facing review context",
		"planmaxx_review",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected digest prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestSectionIterationRendersTemplate(t *testing.T) {
	prompt := SectionIteration(SectionIterationInput{
		Protocol: `<planmaxx_iteration version="1" revision="rev-2"><target id="selection" source="4-8"/><reviewer_instruction>Clarify rollout order.</reviewer_instruction><annotated_plan>## Phase 1

- Old step</annotated_plan><review_threads><thread id="thread-1"><message>Ship CLI before UI polish.</message></thread></review_threads></planmaxx_iteration>`,
	})

	for _, want := range []string{
		"PlanMaxx section iteration",
		"PlanMaxx protocol documentation (v2)",
		"rev-2",
		"planmaxx_iteration",
		"target=\"lines\"",
		"## Phase 1",
		"Clarify rollout order.",
		"Ship CLI before UI polish.",
		"planmaxx_proposal",
		"target=\"lines\"",
		"Do not add prose",
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
		"PlanMaxx protocol documentation (v2)",
		"What should move first?",
		"/repo/plan.md",
		"/repo/plan.md:2:3-2:5",
		"UI",
		"1. CLI",
		"2. UI",
		"planmaxx_side_question",
		"<selected_text>UI</selected_text>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected side-question prompt to contain %q\n%s", want, prompt)
		}
	}
}

func TestEveryModelFacingPromptIncludesProtocolDocumentation(t *testing.T) {
	prompts := []string{
		ApprovedHandoff("# Plan", "{}", "<planmaxx_review version=\"1\"/>", false),
		RejectedHandoff("# Plan", "{}", "<planmaxx_review version=\"1\"/>"),
		ReviewDigest("# Plan", []string{"Decision"}, nil, "<planmaxx_review version=\"1\"/>"),
		SideQuestion("Why?", "/repo/plan.md", "/repo/plan.md:1", "Plan", "# Plan"),
		SectionIteration(SectionIterationInput{Protocol: `<planmaxx_iteration version="1" revision="rev-1"/>`}),
	}
	for _, prompt := range prompts {
		if !strings.Contains(prompt, "PlanMaxx protocol documentation (v2)") {
			t.Fatalf("expected model-facing prompt to include protocol documentation\n%s", prompt)
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
