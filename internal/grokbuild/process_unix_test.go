//go:build !windows

package grokbuild

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPreparedProcessCancellationKillsGrandchild(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(
		ctx,
		shell,
		"-c",
		`sleep 30 & child=$!; printf '%s\n' "$child" > "$1"; wait`,
		"planmaxx-process-tree",
		pidPath,
	)
	prepareProcess(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		content, readErr := os.ReadFile(pidPath)
		if readErr == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(content)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		_ = killProcessTree(command)
		_ = command.Wait()
		t.Fatal("grandchild PID was not published")
	}

	cancel()
	_ = command.Wait()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := unix.Kill(childPID, 0)
		if errors.Is(err, unix.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d survived cancellation", childPID)
}
