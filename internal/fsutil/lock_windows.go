//go:build windows

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// FileLockWindows provides Windows-specific file locking.
type FileLockWindows struct {
	path   string
	handle windows.Handle
}

// NewFileLockWindows creates a new file lock for Windows.
func NewFileLockWindows(path string) *FileLockWindows {
	return &FileLockWindows{path: path}
}

// Lock acquires an exclusive lock on the file.
func (l *FileLockWindows) Lock(timeout time.Duration) error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	pathp, err := windows.UTF16PtrFromString(l.path)
	if err != nil {
		return fmt.Errorf("convert path: %w", err)
	}

	handle, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // No sharing
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	// LockFileEx for exclusive lock
	overlapped := &windows.Overlapped{}
	deadline := time.Now().Add(timeout)
	for {
		err = windows.LockFileEx(
			handle,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			overlapped,
		)
		if err == nil {
			l.handle = handle
			return nil
		}

		if time.Now().After(deadline) {
			windows.CloseHandle(handle)
			return fmt.Errorf("timeout waiting for lock: %s", l.path)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// TryLock attempts to acquire the lock without blocking.
func (l *FileLockWindows) TryLock() (bool, error) {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("create lock directory: %w", err)
	}

	pathp, err := windows.UTF16PtrFromString(l.path)
	if err != nil {
		return false, fmt.Errorf("convert path: %w", err)
	}

	handle, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return false, nil
	}

	overlapped := &windows.Overlapped{}
	err = windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if err != nil {
		windows.CloseHandle(handle)
		return false, nil
	}

	l.handle = handle
	return true, nil
}

// Unlock releases the lock.
func (l *FileLockWindows) Unlock() error {
	if l.handle == 0 {
		return nil
	}

	overlapped := &windows.Overlapped{}
	windows.UnlockFileEx(l.handle, 0, 1, 0, overlapped)
	windows.CloseHandle(l.handle)
	l.handle = 0
	return nil
}

var _ = unsafe.Sizeof(0) // Suppress unused import warning
