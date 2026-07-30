//go:build !windows

package grokbuild

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadRegularFileBoundedRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.pipe")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Skipf("FIFO creation unavailable: %v", err)
	}
	if _, err := readRegularFileBounded(path, 1024); err == nil ||
		!strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("expected non-regular file rejection, got %v", err)
	}
}
