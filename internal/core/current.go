package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hightemp/phvm/internal/fsutil"
)

// CurrentManager manages the current active PHP version.
type CurrentManager struct {
	paths *Paths
}

// NewCurrentManager creates a new CurrentManager.
func NewCurrentManager(paths *Paths) *CurrentManager {
	return &CurrentManager{paths: paths}
}

// Set switches the current version.
// This atomically updates the `current` symlink.
func (m *CurrentManager) Set(version string) error {
	versionDir := m.paths.VersionDir(version)

	// Verify the version is installed
	if !fsutil.Exists(versionDir) {
		return fmt.Errorf("version %s is not installed", version)
	}

	// Verify it has a php binary
	phpBin := filepath.Join(m.paths.VersionBin(version), PHPBinary())
	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("version %s is corrupted: missing php binary", version)
	}

	// Create atomic symlink
	if err := fsutil.AtomicSymlink(versionDir, m.paths.Current); err != nil {
		return fmt.Errorf("update current symlink: %w", err)
	}

	return nil
}

// Get returns the current version.
func (m *CurrentManager) Get() (string, error) {
	if !fsutil.IsSymlink(m.paths.Current) {
		return "", fmt.Errorf("no current version set")
	}

	target, err := fsutil.SymlinkTarget(m.paths.Current)
	if err != nil {
		return "", fmt.Errorf("read current symlink: %w", err)
	}

	// Extract version from path
	version := filepath.Base(target)
	return version, nil
}

// GetPath returns the path to the current version directory.
func (m *CurrentManager) GetPath() (string, error) {
	if !fsutil.IsSymlink(m.paths.Current) {
		return "", fmt.Errorf("no current version set")
	}

	target, err := fsutil.SymlinkTarget(m.paths.Current)
	if err != nil {
		return "", fmt.Errorf("read current symlink: %w", err)
	}

	return target, nil
}

// GetBinPath returns the bin path of the current version.
func (m *CurrentManager) GetBinPath() (string, error) {
	path, err := m.GetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(path, "bin"), nil
}

// GetPHPPath returns the path to the current php binary.
func (m *CurrentManager) GetPHPPath() (string, error) {
	binPath, err := m.GetBinPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(binPath, PHPBinary()), nil
}

// IsSet returns true if a current version is set.
func (m *CurrentManager) IsSet() bool {
	return fsutil.IsSymlink(m.paths.Current)
}

// Clear removes the current symlink.
func (m *CurrentManager) Clear() error {
	if !fsutil.IsSymlink(m.paths.Current) {
		return nil
	}

	if err := os.Remove(m.paths.Current); err != nil {
		return fmt.Errorf("remove current symlink: %w", err)
	}

	return nil
}

// Verify checks if the current version is valid.
func (m *CurrentManager) Verify() error {
	if !m.IsSet() {
		return fmt.Errorf("no current version set")
	}

	path, err := m.GetPath()
	if err != nil {
		return err
	}

	if !fsutil.Exists(path) {
		return fmt.Errorf("current version directory does not exist")
	}

	phpPath, err := m.GetPHPPath()
	if err != nil {
		return err
	}

	if !fsutil.Exists(phpPath) {
		return fmt.Errorf("current php binary does not exist")
	}

	return nil
}
