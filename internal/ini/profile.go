package ini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

// ProfileManager manages ini profiles.
type ProfileManager struct {
	paths       *core.Paths
	backupLimit int
}

// NewProfileManager creates a new ProfileManager.
func NewProfileManager(paths *core.Paths) *ProfileManager {
	return &ProfileManager{paths: paths, backupLimit: 5}
}

// Profile represents an ini profile.
type Profile struct {
	Name    string
	Path    string
	HasIni  bool
	HasConf bool
	Mode    string
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
	iniInfo, iniErr := root.Lstat("php.ini")
	if iniErr != nil && !os.IsNotExist(iniErr) {
		return nil, iniErr
	}
	confInfo, confErr := root.Lstat("conf.d")
	if confErr != nil && !os.IsNotExist(confErr) {
		return nil, confErr
	}

	mode := "ini-only"
	if confErr == nil && confInfo.IsDir() {
		mode = "snapshot"
	}
	data, err := root.ReadFile("profile.json")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		var manifest profileManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parse profile manifest: %w", err)
		}
		if manifest.Format != 1 || (manifest.Mode != "snapshot" && manifest.Mode != "ini-only") {
			return nil, fmt.Errorf("invalid profile manifest")
		}
		mode = manifest.Mode
	}
	return &Profile{
		Mode:    mode,
		Name:    name,
		Path:    profilePath,
		HasIni:  iniErr == nil && iniInfo.Mode().IsRegular(),
		HasConf: confErr == nil && confInfo.IsDir(),
	}, nil
}

// Apply applies a saved snapshot or a php.ini-only profile.
func (m *ProfileManager) Apply(name, version string, backup bool) error {
	return m.ApplyContext(context.Background(), name, version, backup)
}

// ApplyContext coordinates application, validation, rollback and backup history.
func (m *ProfileManager) ApplyContext(ctx context.Context, name, version string, backup bool) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.apply(locked, name, version, backup) })
}

// Save replaces a saved profile with an exact configuration snapshot.
func (m *ProfileManager) Save(name, version string) error {
	return m.SaveContext(context.Background(), name, version)
}

// SaveContext saves a complete snapshot after validation, retaining the old profile on error.
func (m *ProfileManager) SaveContext(ctx context.Context, name, version string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.save(locked, name, version) })
}

// SetBackupLimit selects the retained history size for successful applications.
func (m *ProfileManager) SetBackupLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("backup-keep must be between 1 and 100")
	}
	m.backupLimit = limit
	return nil
}

// Delete deletes a profile under the shared state lock.
func (m *ProfileManager) Delete(name string) error {
	return m.DeleteContext(context.Background(), name)
}

// DeleteContext deletes only the named managed profile.
func (m *ProfileManager) DeleteContext(ctx context.Context, name string) error {
	return m.paths.WithStateLock(ctx, func(context.Context) error {
		if err := fsutil.ValidateName(name); err != nil {
			return err
		}
		root, err := m.paths.OpenDataDir(m.paths.Profiles, false)
		if err != nil {
			return err
		}
		defer root.Close()
		return root.RemoveAll(name)
	})
}

// CreateDefaultProfiles creates the default profiles.
func (m *ProfileManager) CreateDefaultProfiles() error {
	return m.CreateDefaultProfilesContext(context.Background())
}

// CreateDefaultProfilesContext creates php.ini-only defaults without replacing saved profiles.
func (m *ProfileManager) CreateDefaultProfilesContext(ctx context.Context) error {
	return m.paths.WithStateLock(ctx, func(context.Context) error { return m.createDefaultProfiles() })
}

func (m *ProfileManager) createDefaultProfiles() error {
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
		if err := fsutil.AtomicWriteRoot(root, filepath.Join("development", "profile.json"), []byte(`{"format":1,"mode":"ini-only"}`), 0644); err != nil {
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
error_reporting = E_ALL & ~E_DEPRECATED
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
		if err := fsutil.AtomicWriteRoot(root, filepath.Join("production", "profile.json"), []byte(`{"format":1,"mode":"ini-only"}`), 0644); err != nil {
			return err
		}
	}

	return nil
}
