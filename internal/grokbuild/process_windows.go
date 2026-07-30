//go:build windows

package grokbuild

import (
	"os"
	"os/exec"
)

func configureProcessGroup(command *exec.Cmd) {
	command.Cancel = func() error {
		return killProcessTree(command)
	}
}

func killProcessTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return os.ErrProcessDone
	}
	return command.Process.Kill()
}
