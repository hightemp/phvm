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
	root, err := m.paths.OpenDataDir(m.paths.Profiles, false)
	if os.IsNotExist(err) {
		return nil, nil
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
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles: %w", err)
	}

	var profiles []Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		profile, err := m.Get(entry.Name())
		if err != nil {
			continue
		}
		profiles = append(profiles, *profile)
	}

	return profiles, nil
}

// Get returns a profile by name.
func (m *ProfileManager) Get(name string) (*Profile, error) {
	if err := fsutil.ValidateName(name); err != nil {
		return nil, err
	}
	profilePath := m.paths.ProfileDir(name)
	root, err := m.paths.OpenDataDir(profilePath, false)
	if err != nil {
		return nil, fmt.Errorf("profile not found: %s", name)
	}
	defer root.Close()
	iniInfo, iniErr := root.Stat("php.ini")
	if iniErr != nil && !os.IsNotExist(iniErr) {
		return nil, iniErr
	}
	confInfo, confErr := root.Stat("conf.d")
	if confErr != nil && !os.IsNotExist(confErr) {
		return nil, confErr
	}

	return &Profile{
		Name:    name,
		Path:    profilePath,
		HasIni:  iniErr == nil && iniInfo.Mode().IsRegular(),
		HasConf: confErr == nil && confInfo.IsDir(),
	}, nil
}

// Apply applies a profile to a PHP version.
func (m *ProfileManager) Apply(profileName, version string, backup bool) error {
	var err error
	version, err = m.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	profile, err := m.Get(profileName)
	if err != nil {
		return err
	}

	// Backup existing configuration if requested
	if backup {
		if err := m.backupVersion(version); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}

	src, err := m.paths.OpenDataDir(profile.Path, false)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := m.paths.OpenDataDir(m.paths.VersionEtc(version), true)
	if err != nil {
		return err
	}
	defer dst.Close()
	return copyProfileConfig(src, dst)
}

// Save saves the current configuration as a profile.
func (m *ProfileManager) Save(profileName, version string) error {
	if err := fsutil.ValidateName(profileName); err != nil {
		return err
	}
	version, err := m.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	src, err := m.paths.OpenDataDir(m.paths.VersionEtc(version), false)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := m.paths.OpenDataDir(m.paths.ProfileDir(profileName), true)
	if err != nil {
		return err
	}
	defer dst.Close()
	return copyProfileConfig(src, dst)
}

// Delete deletes a profile.
func (m *ProfileManager) Delete(name string) error {
	if err := fsutil.ValidateName(name); err != nil {
		return err
	}
	root, err := m.paths.OpenDataDir(m.paths.Profiles, false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(name)
}

// backupVersion creates a backup of the version's configuration.
func (m *ProfileManager) backupVersion(version string) error {
	root, err := m.paths.OpenVersion(version, false)
	if err != nil {
		return err
	}
	defer root.Close()
	src, err := root.OpenRoot("etc")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer src.Close()
	if err := root.RemoveAll("etc.backup"); err != nil {
		return err
	}
	if err := root.Mkdir("etc.backup", 0755); err != nil {
		return err
	}
	dst, err := root.OpenRoot("etc.backup")
	if err != nil {
		return err
	}
	defer dst.Close()
	return fsutil.CopyRootTree(src, dst)
}

func copyProfileConfig(src, dst *os.Root) error {
	if info, err := src.Lstat("php.ini"); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("php.ini must be a regular file")
		}
		data, err := src.ReadFile("php.ini")
		if err != nil {
			return err
		}
		if err := fsutil.AtomicWriteRoot(dst, "php.ini", data, 0644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if info, err := src.Lstat("conf.d"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("conf.d must be a directory")
		}
		s, err := src.OpenRoot("conf.d")
		if err != nil {
			return err
		}
		defer s.Close()
		if err := dst.Mkdir("conf.d", 0755); err != nil && !os.IsExist(err) {
			return err
		}
		d, err := dst.OpenRoot("conf.d")
		if err != nil {
			return err
		}
		defer d.Close()
		return fsutil.CopyRootTree(s, d)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CreateDefaultProfiles creates the default profiles.
func (m *ProfileManager) CreateDefaultProfiles() error {
	root, err := m.paths.OpenDataDir(m.paths.Profiles, true)
	if err != nil {
		return err
	}
	defer root.Close()
	// Development profile
	devPath := m.paths.ProfileDir("development")
	if !fsutil.IsDir(devPath) {
		if err := root.MkdirAll("development", 0755); err != nil {
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
		if err := fsutil.AtomicWriteRoot(root, filepath.Join("development", "php.ini"), []byte(devIni), 0644); err != nil {
			return err
		}
	}

	// Production profile
	prodPath := m.paths.ProfileDir("production")
	if !fsutil.IsDir(prodPath) {
		if err := root.MkdirAll("production", 0755); err != nil {
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
		if err := fsutil.AtomicWriteRoot(root, filepath.Join("production", "php.ini"), []byte(prodIni), 0644); err != nil {
			return err
		}
	}

	return nil
}
