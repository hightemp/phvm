// Package fsutil provides filesystem utilities for phvm.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a file atomically.
// It writes to a temporary file first, then renames it to the target.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tmpFile = nil // Prevent deferred cleanup

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// AtomicCopyFile copies a file atomically.
func AtomicCopyFile(src, dst string, perm os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tmpFile = nil

	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// RandomSuffix generates a random suffix for temporary files.
func RandomSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// Exists checks if a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir checks if a path is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile checks if a path is a regular file.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// IsSymlink checks if a path is a symbolic link.
func IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// RemoveIfExists removes a file or directory if it exists.
func RemoveIfExists(path string) error {
	if !Exists(path) {
		return nil
	}
	return os.RemoveAll(path)
}

// CopyDir recursively copies a directory.
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source dir: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("get file info: %w", err)
			}
			if err := AtomicCopyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}
