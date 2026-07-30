package grokbuild

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const (
	maxGrokResponseBytes = 16 << 20
	maxGrokErrorBytes    = 1 << 20
	maxGitOutputBytes    = 64 << 20
)

var errProcessOutputLimit = errors.New("subprocess output exceeded its limit")

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		return len(content), errProcessOutputLimit
	}
	if len(content) > remaining {
		_, _ = buffer.Buffer.Write(content[:remaining])
		return len(content), errProcessOutputLimit
	}
	return buffer.Buffer.Write(content)
}

func runBoundedOutput(command *exec.Cmd, stdoutLimit, stderrLimit int) ([]byte, []byte, error) {
	stdout := newBoundedBuffer(stdoutLimit)
	stderr := newBoundedBuffer(stderrLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	prepareProcess(command)
	err := command.Run()
	if errors.Is(err, errProcessOutputLimit) {
		err = fmt.Errorf("%w (%d bytes)", errProcessOutputLimit, stdoutLimit)
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func prepareProcess(command *exec.Cmd) {
	if command == nil {
		return
	}
	configureProcessGroup(command)
	command.WaitDelay = 5 * time.Second
}
