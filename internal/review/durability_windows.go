//go:build windows

package review

// Windows does not provide a Go file handle that can be opened and flushed as
// a directory. The file itself is flushed before the atomic rename, so skip
// the unsupported directory flush instead of turning a committed write into
// an Access is denied error.
func syncDirectory(string) error {
	return nil
}
