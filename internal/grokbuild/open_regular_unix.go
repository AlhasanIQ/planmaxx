//go:build !windows

package grokbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openRegularFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openRootDirectoryNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing non-directory root: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	root := os.NewFile(uintptr(fd), path)
	opened, err := root.Stat()
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if !opened.IsDir() || !os.SameFile(before, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("directory root changed while opening: %s", path)
	}
	return root, nil
}

func openRegularFileBeneath(root *os.File, relative string) (*os.File, error) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe relative file path %q", relative)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	directoryFD, err := unix.Dup(int(root.Fd()))
	if err != nil {
		return nil, err
	}
	for _, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(
			directoryFD,
			part,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW,
			0,
		)
		_ = unix.Close(directoryFD)
		if openErr != nil {
			return nil, openErr
		}
		directoryFD = nextFD
	}
	fd, err := unix.Openat(
		directoryFD,
		parts[len(parts)-1],
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	_ = unix.Close(directoryFD)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(root.Name(), clean)), nil
}
