package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.gotmpl
var templateFS embed.FS

var promptTemplates = template.Must(template.ParseFS(templateFS, "templates/*.gotmpl"))

type handoffTemplateData struct {
	PlanBlock     string
	DigestBlock   string
	NoReviewItems bool
}

type reviewDigestTemplateData struct {
	Plan                string
	ReviewerDecisions   string
	PromotedSideAnswers string
}

type sideQuestionTemplateData struct {
	Question     string
	FilePath     string
	Reference    string
	SelectedText string
	PlanExcerpt  string
}

type SectionIterationInput struct {
	RevisionID          string
	FilePath            string
	Reference           string
	SelectedSection     string
	PlanExcerpt         string
	ReviewerInstruction string
	ReviewerDecisions   []string
	PromotedSideAnswers []string
}

type sectionIterationTemplateData struct {
	RevisionID          string
	FilePath            string
	Reference           string
	SelectedSection     string
	PlanExcerpt         string
	ReviewerInstruction string
	ReviewerDecisions   string
	PromotedSideAnswers string
}

func ApprovedHandoff(plan string, digest string, noReviewItems bool) string {
	return render("approved_handoff.gotmpl", handoffTemplateData{
		PlanBlock:     fencedBlock("markdown", plan),
		DigestBlock:   fencedBlock("json", digest),
		NoReviewItems: noReviewItems,
	})
}

func RejectedHandoff(plan string, digest string) string {
	return render("rejected_handoff.gotmpl", handoffTemplateData{
		PlanBlock:   fencedBlock("markdown", plan),
		DigestBlock: fencedBlock("json", digest),
	})
}

func ReviewDigest(plan string, reviewerDecisions []string, promotedSideAnswers []string) string {
	return render("review_digest.gotmpl", reviewDigestTemplateData{
		Plan:                plan,
		ReviewerDecisions:   strings.Join(reviewerDecisions, "\n"),
		PromotedSideAnswers: strings.Join(promotedSideAnswers, "\n"),
	})
}

func SideQuestion(question string, filePath string, reference string, selectedText string, planExcerpt string) string {
	return render("side_question.gotmpl", sideQuestionTemplateData{
		Question:     question,
		FilePath:     filePath,
		Reference:    reference,
		SelectedText: selectedText,
		PlanExcerpt:  planExcerpt,
	})
}

func SectionIteration(input SectionIterationInput) string {
	return render("section_iteration.gotmpl", sectionIterationTemplateData{
		RevisionID:          input.RevisionID,
		FilePath:            input.FilePath,
		Reference:           input.Reference,
		SelectedSection:     input.SelectedSection,
		PlanExcerpt:         input.PlanExcerpt,
		ReviewerInstruction: input.ReviewerInstruction,
		ReviewerDecisions:   strings.Join(input.ReviewerDecisions, "\n"),
		PromotedSideAnswers: strings.Join(input.PromotedSideAnswers, "\n"),
	})
}

func render(name string, data any) string {
	var out bytes.Buffer
	if err := promptTemplates.ExecuteTemplate(&out, name, data); err != nil {
		panic(fmt.Sprintf("render prompt template %s: %v", name, err))
	}
	return out.String()
}

func fencedBlock(language, content string) string {
	fence := strings.Repeat("`", longestBacktickRun(content)+1)
	if len(fence) < 3 {
		fence = "```"
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return fmt.Sprintf("%s%s\n%s%s\n", fence, language, content, fence)
}

func longestBacktickRun(content string) int {
	longest := 0
	current := 0
	for _, char := range content {
		if char == '`' {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	return longest
}
