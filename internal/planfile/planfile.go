package planfile

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/planformat"
)

type Plan struct {
	Path     string
	Markdown string
	Format   planformat.Format
}

func Load(path string) (Plan, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("read plan file: %w", err)
	}
	markdown := string(content)
	if strings.TrimSpace(markdown) == "" {
		return Plan{}, errors.New("plan file is empty")
	}
	return Plan{Path: path, Markdown: markdown, Format: planformat.Detect(path)}, nil
}
