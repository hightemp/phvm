//go:build !windows

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileLock provides a file-based locking mechanism.
type FileLock struct {
	path string
	file *os.File
}

// NewFileLock creates a new file lock.
func NewFileLock(path string) *FileLock {
	return &FileLock{path: path}
}

// Lock acquires an exclusive lock on the file.
// It blocks until the lock is acquired or timeout is reached.
func (l *FileLock) Lock(timeout time.Duration) error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			l.file = f
			return nil
		}

		if time.Now().After(deadline) {
			f.Close()
			return fmt.Errorf("timeout waiting for lock: %s", l.path)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// TryLock attempts to acquire the lock without blocking.
// Returns true if the lock was acquired, false otherwise.
func (l *FileLock) TryLock() (bool, error) {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("create lock directory: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false, fmt.Errorf("open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return false, nil
		}
		return false, fmt.Errorf("acquire lock: %w", err)
	}

	l.file = f
	return true, nil
}

// Unlock releases the lock.
func (l *FileLock) Unlock() error {
	if l.file == nil {
		return nil
	}

	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	l.file = nil

	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// LockPath returns the path to the lock file.
func (l *FileLock) LockPath() string {
	return l.path
}

// WithLock executes a function while holding the lock.
func WithLock(lockPath string, timeout time.Duration, fn func() error) error {
	lock := NewFileLock(lockPath)
	if err := lock.Lock(timeout); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	return fn()
}
