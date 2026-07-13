package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
	"github.com/AlhasanIQ/planmaxx/internal/reviewxml"
)

//go:embed templates/*.gotmpl
var templateFS embed.FS

var promptTemplates = template.Must(template.ParseFS(templateFS, "templates/*.gotmpl"))

type handoffTemplateData struct {
	PlanBlock             string
	DigestBlock           string
	ReviewContext         string
	ProtocolDocumentation string
	NoReviewItems         bool
}

type reviewDigestTemplateData struct {
	Plan                  string
	ReviewerDecisions     string
	PromotedSideAnswers   string
	ReviewContext         string
	ProtocolDocumentation string
}

type sideQuestionTemplateData struct {
	Protocol              string
	ProtocolDocumentation string
}

type SectionIterationInput struct {
	Protocol string
	Format   planformat.Format
}

type sectionIterationTemplateData struct {
	Protocol              string
	ProtocolDocumentation string
}

type protocolDocumentationData struct {
	Mode   string
	Format planformat.Format
}

func ApprovedHandoff(plan string, digest string, reviewContext string, noReviewItems bool, format planformat.Format) string {
	format = planformat.Normalize(format, "")
	return render("approved_handoff.gotmpl", handoffTemplateData{
		PlanBlock:             fencedBlock(string(format), plan),
		DigestBlock:           fencedBlock("json", digest),
		ReviewContext:         reviewContext,
		ProtocolDocumentation: protocolDocumentation("handoff", format),
		NoReviewItems:         noReviewItems,
	})
}

func ReviewDigest(plan string, reviewerDecisions []string, promotedSideAnswers []string, reviewContext string, format planformat.Format) string {
	format = planformat.Normalize(format, "")
	return render("review_digest.gotmpl", reviewDigestTemplateData{
		Plan:                  plan,
		ReviewerDecisions:     strings.Join(reviewerDecisions, "\n"),
		PromotedSideAnswers:   strings.Join(promotedSideAnswers, "\n"),
		ReviewContext:         reviewContext,
		ProtocolDocumentation: protocolDocumentation("review_digest", format),
	})
}

func SideQuestion(question string, filePath string, reference string, selectedText string, planExcerpt string, format planformat.Format) string {
	return render("side_question.gotmpl", sideQuestionTemplateData{Protocol: reviewxml.SideQuestion(reviewxml.SideQuestionInput{
		Question:     question,
		FilePath:     filePath,
		Reference:    reference,
		SelectedText: selectedText,
		PlanExcerpt:  planExcerpt,
	}), ProtocolDocumentation: protocolDocumentation("side_question", format)})
}

func SectionIteration(input SectionIterationInput) string {
	input.Format = planformat.Normalize(input.Format, "")
	return render("section_iteration.gotmpl", sectionIterationTemplateData{
		Protocol:              input.Protocol,
		ProtocolDocumentation: protocolDocumentation("section_iteration", input.Format),
	})
}

func protocolDocumentation(mode string, format planformat.Format) string {
	return render("protocol.gotmpl", protocolDocumentationData{Mode: mode, Format: planformat.Normalize(format, "")})
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
