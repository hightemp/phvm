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

func (m *Manager) phpComposerDir(version string) string {
	return filepath.Join(m.paths.Composer, version)
}

// PHPPharPath returns the permanent Composer PHAR selected for one PHP release.
func (m *Manager) PHPPharPath(version string) string {
	return filepath.Join(m.phpComposerDir(version), pharName)
}

func (m *Manager) launcher(version, phar string) []byte {
	php := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	return []byte(fmt.Sprintf("#!/bin/sh\nexec %s %s \"$@\"\n", quote(php), quote(phar)))
}

func (m *Manager) legacyLauncher(version, phar string) []byte {
	php := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
	return []byte(fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", php, phar))
}

// MigrateLegacy copies old shared or cached PHARs into per-PHP storage and
// repoints managed launchers. Cache cleanup stops if a launcher cannot migrate.
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
	permanent, err := m.paths.OpenDataDir(m.paths.Composer, false)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	defer func() {
		if permanent != nil {
			_ = permanent.Close()
		}
	}()
	permanentReady := false
	if permanent != nil {
		permanentReady, err = regularPHAR(permanent)
		if err != nil {
			return err
		}
	}
	legacy, err := m.paths.OpenDataDir(m.paths.Downloads, false)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if legacy != nil {
		defer legacy.Close()
	}
	legacyReady := false
	if legacy != nil {
		legacyReady, err = regularPHAR(legacy)
		if err != nil {
			return err
		}
	}
	// A legacy global-only installation has no launcher to migrate. Keep its
	// verified seed outside the disposable download cache before cleanup.
	if legacyReady && !permanentReady {
		if permanent == nil {
			permanent, err = m.paths.OpenDataDir(m.paths.Composer, true)
			if err != nil {
				return err
			}
		}
		stage, err := m.copyLegacyPHAR(ctx, legacy, permanent)
		if err != nil {
			return err
		}
		if err := permanent.Rename(stage, pharName); err != nil {
			_ = permanent.Remove(stage)
			return err
		}
		permanentReady = true
	}
	for _, version := range versions {
		if replacement != "" && version != replacement {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		versionRoot, err := m.paths.OpenVersion(version, false)
		if err != nil {
			return err
		}
		launcherPath := filepath.Join("bin", "composer")
		data, err := versionRoot.ReadFile(launcherPath)
		if os.IsNotExist(err) && replacement == "" {
			_ = versionRoot.Close()
			continue
		}
		if err != nil {
			if !os.IsNotExist(err) {
				_ = versionRoot.Close()
				return fmt.Errorf("read Composer launcher for PHP %s: %w", version, err)
			}
			data = nil
		}
		if bytes.Equal(data, m.launcher(version, m.PHPPharPath(version))) {
			_ = versionRoot.Close()
			continue
		}
		known := bytes.Equal(data, m.launcher(version, m.pharPath())) ||
			bytes.Equal(data, m.legacyLauncher(version, m.pharPath())) ||
			bytes.Equal(data, m.launcher(version, legacyPath)) ||
			bytes.Equal(data, m.legacyLauncher(version, legacyPath))
		if replacement == "" && !known {
			_ = versionRoot.Close()
			if bytes.Contains(data, []byte(legacyPath)) || bytes.Contains(data, []byte(m.pharPath())) {
				return fmt.Errorf("unrecognized legacy composer launcher for PHP %s; run phvm composer enable --php %s before clearing downloads", version, version)
			}
			continue
		}
		target, err := m.paths.OpenDataDir(m.phpComposerDir(version), true)
		if err != nil {
			_ = versionRoot.Close()
			return err
		}
		hasTarget, err := regularPHAR(target)
		if err == nil && !hasTarget {
			var source *os.Root
			if permanentReady {
				source = permanent
			} else if legacyReady {
				source = legacy
			} else {
				err = fmt.Errorf("composer PHAR missing for PHP %s; run phvm composer install --php %s", version, version)
			}
			if source != nil {
				var stage string
				stage, err = m.copyLegacyPHAR(ctx, source, target)
				if err == nil {
					phpBin := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
					_, err = m.composerVersion(ctx, phpBin, filepath.Join(m.phpComposerDir(version), stage))
					if err == nil {
						err = target.Rename(stage, pharName)
					}
					_ = target.Remove(stage)
				}
			}
		}
		if err == nil && hasTarget {
			phpBin := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
			_, err = m.composerVersion(ctx, phpBin, m.PHPPharPath(version))
		}
		if err == nil {
			err = fsutil.AtomicWriteRoot(versionRoot, launcherPath, m.launcher(version, m.PHPPharPath(version)), 0755)
		}
		_ = target.Close()
		_ = versionRoot.Close()
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

func (m *Manager) copyLegacyPHAR(ctx context.Context, sourceRoot, target *os.Root) (string, error) {
	source, err := sourceRoot.Open(pharName)
	if err != nil {
		return "", err
	}
	defer source.Close()
	name := "composer.phar.migrate-" + fsutil.RandomSuffix()
	f, err := target.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	cleanup := func() {
		_ = f.Close()
		_ = target.Remove(name)
	}
	if _, err := io.Copy(f, source); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", err
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return "", err
	}
	return name, nil
}
