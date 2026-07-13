package planformat

import (
	"path/filepath"
	"strings"
)

type Format string

const (
	Markdown Format = "markdown"
	HTML     Format = "html"
)

func Detect(path string) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return HTML
	default:
		return Markdown
	}
}

func Normalize(format Format, path string) Format {
	if format == HTML || format == Markdown {
		return format
	}
	return Detect(path)
}
