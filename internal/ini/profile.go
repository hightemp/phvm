package ini

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

// ProfileManager manages ini profiles.
type ProfileManager struct {
	paths *core.Paths
}

// NewProfileManager creates a new ProfileManager.
func NewProfileManager(paths *core.Paths) *ProfileManager {
	return &ProfileManager{paths: paths}
}

// Profile represents an ini profile.
type Profile struct {
	Name    string
	Path    string
	HasIni  bool
	HasConf bool
}

// List lists all available profiles.
func (m *ProfileManager) List() ([]Profile, error) {
	entries, err := os.ReadDir(m.paths.Profiles)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles: %w", err)
	}

	var profiles []Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		profilePath := filepath.Join(m.paths.Profiles, entry.Name())
		profile := Profile{
			Name:    entry.Name(),
			Path:    profilePath,
			HasIni:  fsutil.Exists(filepath.Join(profilePath, "php.ini")),
			HasConf: fsutil.IsDir(filepath.Join(profilePath, "conf.d")),
		}
		profiles = append(profiles, profile)
	}

	return profiles, nil
}

// Get returns a profile by name.
func (m *ProfileManager) Get(name string) (*Profile, error) {
	profilePath := m.paths.ProfileDir(name)

	if !fsutil.IsDir(profilePath) {
		return nil, fmt.Errorf("profile not found: %s", name)
	}

	return &Profile{
		Name:    name,
		Path:    profilePath,
		HasIni:  fsutil.Exists(filepath.Join(profilePath, "php.ini")),
		HasConf: fsutil.IsDir(filepath.Join(profilePath, "conf.d")),
	}, nil
}

// Apply applies a profile to a PHP version.
func (m *ProfileManager) Apply(profileName, version string, backup bool) error {
	profile, err := m.Get(profileName)
	if err != nil {
		return err
	}

	versionEtc := m.paths.VersionEtc(version)
	versionIni := m.paths.VersionPhpIni(version)
	versionConfD := m.paths.VersionConfD(version)

	// Backup existing configuration if requested
	if backup {
		if err := m.backupVersion(version); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}

	// Copy php.ini if exists
	if profile.HasIni {
		srcIni := filepath.Join(profile.Path, "php.ini")
		if err := fsutil.AtomicCopyFile(srcIni, versionIni, 0644); err != nil {
			return fmt.Errorf("copy php.ini: %w", err)
		}
	}

	// Copy conf.d if exists
	if profile.HasConf {
		srcConfD := filepath.Join(profile.Path, "conf.d")

		// Ensure conf.d exists
		if err := fsutil.EnsureDir(versionConfD); err != nil {
			return fmt.Errorf("create conf.d: %w", err)
		}

		// Copy all ini files
		entries, err := os.ReadDir(srcConfD)
		if err != nil {
			return fmt.Errorf("read profile conf.d: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			srcPath := filepath.Join(srcConfD, entry.Name())
			dstPath := filepath.Join(versionConfD, entry.Name())
			if err := fsutil.AtomicCopyFile(srcPath, dstPath, 0644); err != nil {
				return fmt.Errorf("copy %s: %w", entry.Name(), err)
			}
		}
	}

	// Ensure etc directory exists
	if err := fsutil.EnsureDir(versionEtc); err != nil {
		return fmt.Errorf("create etc: %w", err)
	}

	return nil
}

// Save saves the current configuration as a profile.
func (m *ProfileManager) Save(profileName, version string) error {
	versionIni := m.paths.VersionPhpIni(version)
	versionConfD := m.paths.VersionConfD(version)

	profilePath := m.paths.ProfileDir(profileName)
	profileIni := filepath.Join(profilePath, "php.ini")
	profileConfD := filepath.Join(profilePath, "conf.d")

	// Create profile directory
	if err := fsutil.EnsureDir(profilePath); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}

	// Copy php.ini
	if fsutil.Exists(versionIni) {
		if err := fsutil.AtomicCopyFile(versionIni, profileIni, 0644); err != nil {
			return fmt.Errorf("copy php.ini: %w", err)
		}
	}

	// Copy conf.d
	if fsutil.IsDir(versionConfD) {
		if err := fsutil.CopyDir(versionConfD, profileConfD); err != nil {
			return fmt.Errorf("copy conf.d: %w", err)
		}
	}

	return nil
}

// Delete deletes a profile.
func (m *ProfileManager) Delete(name string) error {
	profilePath := m.paths.ProfileDir(name)

	if !fsutil.IsDir(profilePath) {
		return fmt.Errorf("profile not found: %s", name)
	}

	return os.RemoveAll(profilePath)
}

// backupVersion creates a backup of the version's configuration.
func (m *ProfileManager) backupVersion(version string) error {
	versionEtc := m.paths.VersionEtc(version)
	backupPath := versionEtc + ".backup"

	// Remove existing backup
	if fsutil.Exists(backupPath) {
		if err := os.RemoveAll(backupPath); err != nil {
			return err
		}
	}

	// Copy current etc to backup
	if fsutil.IsDir(versionEtc) {
		return fsutil.CopyDir(versionEtc, backupPath)
	}

	return nil
}

// CreateDefaultProfiles creates the default profiles.
func (m *ProfileManager) CreateDefaultProfiles() error {
	// Development profile
	devPath := m.paths.ProfileDir("development")
	if !fsutil.IsDir(devPath) {
		if err := fsutil.EnsureDir(devPath); err != nil {
			return err
		}

		devIni := `; Development php.ini
; Created by phvm

[PHP]
error_reporting = E_ALL
display_errors = On
display_startup_errors = On
log_errors = On
html_errors = On

[Date]
; date.timezone = UTC

[opcache]
opcache.enable=1
opcache.enable_cli=0
opcache.validate_timestamps=1
opcache.revalidate_freq=0
`
		if err := fsutil.AtomicWriteFile(filepath.Join(devPath, "php.ini"), []byte(devIni), 0644); err != nil {
			return err
		}
	}

	// Production profile
	prodPath := m.paths.ProfileDir("production")
	if !fsutil.IsDir(prodPath) {
		if err := fsutil.EnsureDir(prodPath); err != nil {
			return err
		}

		prodIni := `; Production php.ini
; Created by phvm

[PHP]
error_reporting = E_ALL & ~E_DEPRECATED & ~E_STRICT
display_errors = Off
display_startup_errors = Off
log_errors = On
html_errors = Off

[Date]
; date.timezone = UTC

[opcache]
opcache.enable=1
opcache.enable_cli=0
opcache.validate_timestamps=0
opcache.max_accelerated_files=10000
opcache.memory_consumption=128
opcache.interned_strings_buffer=16
`
		if err := fsutil.AtomicWriteFile(filepath.Join(prodPath, "php.ini"), []byte(prodIni), 0644); err != nil {
			return err
		}
	}

	return nil
}
