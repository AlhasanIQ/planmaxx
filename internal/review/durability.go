package review

import "os"

// syncRegularFile flushes file content before an atomic rename. Windows
// requires a writable handle for FlushFileBuffers, even when the caller only
// needs to persist data that another process wrote.
func syncRegularFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
