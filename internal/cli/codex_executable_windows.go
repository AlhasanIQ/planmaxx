//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexDesktopExecutable struct {
	path    string
	updated time.Time
}

func codexDesktopExecutableCandidates() []string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return nil
	}
	root := filepath.Join(localAppData, "OpenAI", "Codex", "bin")
	patterns := []string{
		filepath.Join(root, "codex.exe"),
		filepath.Join(root, "*", "codex.exe"),
	}
	var found []codexDesktopExecutable
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			found = append(found, codexDesktopExecutable{path: match, updated: info.ModTime()})
		}
	}
	sort.SliceStable(found, func(left, right int) bool {
		return found[left].updated.After(found[right].updated)
	})
	candidates := make([]string, 0, len(found))
	for _, executable := range found {
		candidates = append(candidates, executable.path)
	}
	return candidates
}
