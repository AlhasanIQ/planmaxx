//go:build windows

package revisions

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const lockfileExclusive = 0x00000002

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func acquireFileLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create revision lock directory: %w", err)
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open revision lock: %w", err)
	}
	overlapped := syscall.Overlapped{}
	ret, _, callErr := lockFileEx.Call(lock.Fd(), lockfileExclusive, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ret == 0 {
		_ = lock.Close()
		return nil, fmt.Errorf("lock revision store: %w", callErr)
	}
	return func() {
		_, _, _ = unlockFileEx.Call(lock.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		_ = lock.Close()
	}, nil
}
