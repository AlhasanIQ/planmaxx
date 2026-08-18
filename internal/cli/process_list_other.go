//go:build !windows

package cli

import "os/exec"

func platformProcessList() ([]byte, error) {
	return exec.Command("ps", "-axo", "pid=,command=").Output()
}
