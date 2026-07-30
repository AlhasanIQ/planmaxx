//go:build windows

package grokbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func openRegularFileNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}

func openRootDirectoryNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}

func openRegularFileBeneath(root *os.File, relative string) (*os.File, error) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe relative file path %q", relative)
	}
	return os.Open(filepath.Join(root.Name(), clean))
}
