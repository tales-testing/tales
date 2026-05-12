//go:build !windows

package embeddeddriver

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func acquireFileLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ensure lock dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	fd := fdAsInt(f.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("flock: %w", err)
	}

	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// fdAsInt narrows os.File.Fd (uintptr) into the int that syscall.Flock
// expects. On the unix platforms Tales runs on, fd values fit easily in
// an int, so the conversion is safe.
func fdAsInt(fd uintptr) int {
	//nolint:gosec // G115: file descriptors on POSIX systems are small ints, the uintptr-to-int conversion cannot overflow in practice.
	return int(fd)
}
