package core

import (
	"os"
	"path/filepath"

	"github.com/hightemp/phvm/internal/fsutil"
)

// InstalledManager manages installed PHP versions.
type InstalledManager struct {
	paths *Paths
}

// NewInstalledManager creates a new InstalledManager.
func NewInstalledManager(paths *Paths) *InstalledManager {
	return &InstalledManager{paths: paths}
}

// List returns all installed versions.
func (m *InstalledManager) List() ([]string, error) {
	entries, err := os.ReadDir(m.paths.Versions)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		version := entry.Name()

		// Verify it's a valid installation (has php binary)
		phpBin := filepath.Join(m.paths.VersionBin(version), PHPBinary())
		if fsutil.Exists(phpBin) {
			versions = append(versions, version)
		}
	}

	SortVersions(versions)
	return versions, nil
}

// IsInstalled checks if a version is installed.
func (m *InstalledManager) IsInstalled(version string) bool {
	versionDir := m.paths.VersionDir(version)
	if !fsutil.Exists(versionDir) {
		return false
	}

	phpBin := filepath.Join(m.paths.VersionBin(version), PHPBinary())
	return fsutil.Exists(phpBin)
}

// GetLatestInstalled returns the latest installed version matching a pattern.
func (m *InstalledManager) GetLatestInstalled(pattern string) (string, error) {
	versions, err := m.List()
	if err != nil {
		return "", err
	}

	return LatestMatchingVersion(versions, pattern)
}

// Remove removes an installed version.
func (m *InstalledManager) Remove(version string) error {
	versionDir := m.paths.VersionDir(version)
	return os.RemoveAll(versionDir)
}

// GetVersionInfo returns information about an installed version.
func (m *InstalledManager) GetVersionInfo(version string) (*VersionInfo, error) {
	if !m.IsInstalled(version) {
		return nil, nil
	}

	versionDir := m.paths.VersionDir(version)
	info := &VersionInfo{
		Version:   version,
		Path:      versionDir,
		BinPath:   m.paths.VersionBin(version),
		EtcPath:   m.paths.VersionEtc(version),
		ConfDPath: m.paths.VersionConfD(version),
	}

	// Load metadata if exists
	metadataPath := m.paths.VersionMetadata(version)
	if fsutil.Exists(metadataPath) {
		metadata, err := LoadMetadata(metadataPath)
		if err == nil {
			info.Metadata = metadata
		}
	}

	return info, nil
}

// VersionInfo holds information about an installed version.
type VersionInfo struct {
	Version   string
	Path      string
	BinPath   string
	EtcPath   string
	ConfDPath string
	Metadata  *Metadata
}
