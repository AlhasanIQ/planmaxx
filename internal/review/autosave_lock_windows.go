//go:build windows

package review

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const lockfileExclusive = 0x00000002

var (
	kernel32Autosave     = syscall.NewLazyDLL("kernel32.dll")
	lockFileExAutosave   = kernel32Autosave.NewProc("LockFileEx")
	unlockFileExAutosave = kernel32Autosave.NewProc("UnlockFileEx")
)

func acquireAutosaveLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create autosave lock directory: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open autosave lock: %w", err)
	}
	overlapped := syscall.Overlapped{}
	ret, _, callErr := lockFileExAutosave.Call(lock.Fd(), lockfileExclusive, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ret == 0 {
		_ = lock.Close()
		return nil, fmt.Errorf("lock autosave: %w", callErr)
	}
	return func() {
		_, _, _ = unlockFileExAutosave.Call(lock.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		_ = lock.Close()
	}, nil
}
