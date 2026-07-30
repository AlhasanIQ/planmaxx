//go:build windows

package cli

import "os"

func openSkillFileNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}
