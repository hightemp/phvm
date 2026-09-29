package composer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

const pharName = "composer.phar"

func (m *Manager) pharPath() string { return filepath.Join(m.paths.Composer, pharName) }

func (m *Manager) launcher(version, phar string) []byte {
	php := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	return []byte(fmt.Sprintf("#!/bin/sh\nexec %s %s \"$@\"\n", quote(php), quote(phar)))
}

func (m *Manager) legacyLauncher(version, phar string) []byte {
	php := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
	return []byte(fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", php, phar))
}

// MigrateLegacy preserves the old cached PHAR and repoints managed launchers.
// Cache cleanup must not proceed if any legacy launcher cannot be migrated.
func (m *Manager) MigrateLegacy(ctx context.Context) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.migrateLegacy(locked, "") })
}

func (m *Manager) migrateLegacy(ctx context.Context, replacement string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	legacyPath := filepath.Join(m.paths.Downloads, pharName)
	versions, err := core.NewInstalledManager(m.paths).List()
	if err != nil {
		return fmt.Errorf("list PHP versions for Composer migration: %w", err)
	}
	var migrate []string
	for _, version := range versions {
		root, err := m.paths.OpenVersion(version, false)
		if err != nil {
			return err
		}
		data, err := root.ReadFile(filepath.Join("bin", "composer"))
		_ = root.Close()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read Composer launcher for PHP %s: %w", version, err)
		}
		if bytes.Equal(data, m.legacyLauncher(version, legacyPath)) {
			migrate = append(migrate, version)
		} else if replacement == "" && bytes.Contains(data, []byte(legacyPath)) {
			return fmt.Errorf("unrecognized legacy composer launcher for PHP %s; run phvm composer enable --php %s before clearing downloads", version, version)
		}
	}
	permanent, err := m.paths.OpenDataDir(m.paths.Composer, false)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	active := false
	if permanent != nil {
		active, err = regularPHAR(permanent)
		_ = permanent.Close()
		if err != nil {
			return err
		}
	}
	if !active {
		legacy, err := m.paths.OpenDataDir(m.paths.Downloads, false)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if legacy != nil {
			defer legacy.Close()
			found, err := regularPHAR(legacy)
			if err != nil {
				return err
			}
			if found {
				if err := m.copyLegacyPHAR(ctx, legacy); err != nil {
					return err
				}
				active = true
			}
		}
	}
	if !active {
		if len(migrate) > 0 {
			return fmt.Errorf("legacy composer PHAR missing; run phvm composer install --global before clearing downloads")
		}
		return nil
	}
	for _, version := range migrate {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, err := m.paths.OpenVersion(version, false)
		if err != nil {
			return err
		}
		err = fsutil.AtomicWriteRoot(root, filepath.Join("bin", "composer"), m.launcher(version, m.pharPath()), 0755)
		_ = root.Close()
		if err != nil {
			return fmt.Errorf("migrate Composer launcher for PHP %s: %w", version, err)
		}
	}
	return nil
}

func regularPHAR(root *os.Root) (bool, error) {
	info, err := root.Lstat(pharName)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("composer PHAR must be a regular file, not a symlink or directory")
	}
	return true, nil
}

func (m *Manager) copyLegacyPHAR(ctx context.Context, legacy *os.Root) error {
	source, err := legacy.Open(pharName)
	if err != nil {
		return err
	}
	defer source.Close()
	root, err := m.paths.OpenDataDir(m.paths.Composer, true)
	if err != nil {
		return err
	}
	defer root.Close()
	name := "composer.phar.migrate-" + fsutil.RandomSuffix()
	defer func() { _ = root.Remove(name) }()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, source); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return root.Rename(name, pharName)
}
