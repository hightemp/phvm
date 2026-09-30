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
	return atomicReplace(path, perm, func(file atomicTempFile) error {
		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		return nil
	}, createAtomicTemp, os.Rename)
}

// AtomicCopyFile copies a file atomically.
func AtomicCopyFile(src, dst string, perm os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	return atomicReplace(dst, perm, func(file atomicTempFile) error {
		if _, err := io.Copy(file, srcFile); err != nil {
			return fmt.Errorf("copy data: %w", err)
		}
		return nil
	}, createAtomicTemp, os.Rename)
}

type atomicTempFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

func createAtomicTemp(dir string) (atomicTempFile, error) {
	return os.CreateTemp(dir, ".tmp-*")
}

func atomicReplace(dst string, perm os.FileMode, write func(atomicTempFile) error, createTemp func(string) (atomicTempFile, error), rename func(string, string) error) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmpFile, err := createTemp(dir)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	published := false
	// A closed handle still leaves a temp path behind when publication fails.
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
		}
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := write(tmpFile); err != nil {
		return err
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

	if err := rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	published = true
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
