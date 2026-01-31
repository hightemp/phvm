package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicSymlink creates a symlink atomically by creating a temporary symlink
// and then renaming it to the target path.
func AtomicSymlink(target, link string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(link)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// Create temporary symlink with random suffix
	tmpLink := link + ".new." + RandomSuffix()

	// Create the symlink
	if err := os.Symlink(target, tmpLink); err != nil {
		return fmt.Errorf("create temp symlink: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpLink, link); err != nil {
		os.Remove(tmpLink) // Cleanup on failure
		return fmt.Errorf("rename symlink: %w", err)
	}

	return nil
}

// ReadSymlink reads the target of a symlink.
func ReadSymlink(path string) (string, error) {
	return os.Readlink(path)
}

// UpdateSymlink updates an existing symlink atomically.
func UpdateSymlink(target, link string) error {
	// AtomicSymlink handles existing symlinks/files by using rename,
	// which atomically replaces the target. No need to remove first.
	return AtomicSymlink(target, link)
}

// SymlinkTarget returns the resolved target of a symlink.
// If the path is not a symlink, returns the path itself.
func SymlinkTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}

	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}

	// If relative, resolve against the symlink's directory
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}

	return filepath.Clean(target), nil
}

// ResolveSymlink recursively resolves all symlinks in a path.
func ResolveSymlink(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
