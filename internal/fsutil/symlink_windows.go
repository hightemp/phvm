//go:build windows

package fsutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AtomicSymlinkWindows creates a directory junction on Windows.
// Falls back to regular symlink if junction fails.
func AtomicSymlinkWindows(target, link string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(link)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// Remove existing junction/symlink
	if Exists(link) {
		// For junctions, we need to use rmdir, not os.Remove
		if IsDir(link) {
			cmd := exec.Command("cmd", "/c", "rmdir", link)
			cmd.Run()
		} else {
			os.Remove(link)
		}
	}

	// Try creating a junction first (works without admin rights)
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fall back to regular symlink (may require admin rights)
	return os.Symlink(target, link)
}

// IsJunction checks if a path is a Windows junction point.
func IsJunction(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	// Junctions appear as directories with reparse point attribute
	return info.IsDir() && info.Mode()&os.ModeSymlink != 0
}
