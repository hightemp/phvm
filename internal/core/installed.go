package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	root, err := m.paths.OpenDataDir(m.paths.Versions, false)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
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
		canonical, err := NormalizeInstalledVersion(version)
		if err != nil || canonical != version {
			continue
		}

		// Verify it's a valid installation (has php binary)
		if m.IsInstalled(version) {
			versions = append(versions, version)
		}
	}

	SortVersions(versions)
	return versions, nil
}

// IsInstalled checks if a version is installed.
func (m *InstalledManager) IsInstalled(version string) bool {
	version, err := NormalizeInstalledVersion(version)
	if err != nil {
		return false
	}
	root, err := m.paths.OpenVersion(version, false)
	if err != nil {
		return false
	}
	defer root.Close()
	data, metadataErr := root.ReadFile(".phvm-metadata.json")
	if metadataErr != nil && !os.IsNotExist(metadataErr) {
		return false
	}
	if metadataErr == nil {
		var metadata Metadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			return false
		}
		if metadata.InstallationState != "" && (metadata.InstallationState != "ready" || metadata.Version != version) {
			return false
		}
		if metadata.InstallationState == "ready" {
			if len(metadata.InstallationID) != 16 || metadata.PHPAPI == "" {
				return false
			}
			if _, err := hex.DecodeString(metadata.InstallationID); err != nil {
				return false
			}
			for _, name := range []string{"bin/phpize", "bin/php-config", "etc/php.ini", "include/php/Zend/zend_modules.h"} {
				info, err := root.Lstat(filepath.FromSlash(name))
				if err != nil || !info.Mode().IsRegular() {
					return false
				}
			}
		}
	}
	info, err := root.Stat(filepath.Join("bin", PHPBinary()))
	return err == nil && info.Mode().IsRegular()
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
	return m.RemoveContext(context.Background(), version)
}

// RemoveContext coordinates version deletion with installs and dependent tools.
func (m *InstalledManager) RemoveContext(ctx context.Context, version string) error {
	return m.paths.WithStateLock(ctx, func(context.Context) error { return m.remove(version) })
}

func (m *InstalledManager) remove(version string) error {
	version, err := NormalizeInstalledVersion(version)
	if err != nil {
		return err
	}
	root, err := m.paths.OpenDataDir(m.paths.Versions, false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(version)
}

// GetVersionInfo returns information about an installed version.
func (m *InstalledManager) GetVersionInfo(version string) (*VersionInfo, error) {
	var err error
	version, err = m.paths.CheckVersionPath(version)
	if err != nil {
		return nil, err
	}
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
	root, err := m.paths.OpenVersion(version, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if data, err := root.ReadFile(".phvm-metadata.json"); err == nil {
		var metadata Metadata
		if json.Unmarshal(data, &metadata) == nil {
			info.Metadata = &metadata
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
