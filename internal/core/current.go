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

// Set atomically switches to a registered, confined installation.
func (m *CurrentManager) Set(version string) error {
	version, err := m.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	if !NewInstalledManager(m.paths).IsInstalled(version) {
		return fmt.Errorf("version %s is not installed or is corrupted", version)
	}
	root, err := m.paths.OpenDataDir(m.paths.Root, false)
	if err != nil {
		return err
	}
	defer root.Close()
	target, err := filepath.Abs(m.paths.VersionDir(version))
	if err != nil {
		return err
	}
	tmp := "current.new." + fsutil.RandomSuffix()
	defer func() { _ = root.Remove(tmp) }()
	if err := root.Symlink(target, tmp); err != nil {
		return err
	}
	return root.Rename(tmp, "current")
}

// Get returns the registered current version after verifying its target.
func (m *CurrentManager) Get() (string, error) {
	root, err := m.paths.OpenDataDir(m.paths.Root, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	target, err := root.Readlink("current")
	if err != nil {
		return "", fmt.Errorf("no current version set: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(m.paths.Root, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	version, err := NormalizeInstalledVersion(filepath.Base(target))
	if err != nil {
		return "", err
	}
	expected, err := filepath.Abs(m.paths.VersionDir(version))
	if err != nil {
		return "", err
	}
	if filepath.Clean(target) != expected {
		return "", fmt.Errorf("current target is outside the registered version directory")
	}
	if !NewInstalledManager(m.paths).IsInstalled(version) {
		return "", fmt.Errorf("current installation is missing or unsafe")
	}
	return version, nil
}

// GetPath returns the confined path of the registered current version.
func (m *CurrentManager) GetPath() (string, error) {
	version, err := m.Get()
	if err != nil {
		return "", err
	}
	return filepath.Abs(m.paths.VersionDir(version))
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
	_, err := m.Get()
	return err == nil
}

// Clear removes only the current link inside the configured root.
func (m *CurrentManager) Clear() error {
	root, err := m.paths.OpenDataDir(m.paths.Root, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat("current")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("current is not a symbolic link")
	}
	return root.Remove("current")
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
