package composer

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/redact"
)

// Update replaces the shared PHAR only after checksum and PHP-version validation.
func (m *Manager) Update(ctx context.Context, phpVersion string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.update(locked, phpVersion) })
}

func (m *Manager) update(ctx context.Context, phpVersion string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	phpVersion, err := m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return err
	}
	versionRoot, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer versionRoot.Close()
	for _, name := range []string{core.PHPBinary(), "composer"} {
		if info, err := versionRoot.Stat(filepath.Join("bin", name)); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s not installed for PHP %s", name, phpVersion)
		}
	}
	if err := m.MigrateLegacy(ctx); err != nil {
		return err
	}
	root, err := m.paths.OpenDataDir(m.paths.Composer, false)
	if err != nil {
		return fmt.Errorf("open Composer storage: %w", err)
	}
	defer root.Close()
	info, err := root.Lstat("composer.phar")
	if err != nil {
		return fmt.Errorf("composer PHAR missing; run phvm composer install: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("composer PHAR must be a regular file, not a symlink or directory")
	}
	phpBin := filepath.Join(m.paths.VersionBin(phpVersion), core.PHPBinary())
	active := m.pharPath()
	before, err := m.composerVersion(ctx, phpBin, active)
	if err != nil {
		return fmt.Errorf("read installed Composer version: %w", err)
	}
	log.Info("Updating Composer %s...", before.Original())
	stage, err := m.stageVerified(ctx, root)
	if err != nil {
		return fmt.Errorf("download Composer update: %w", err)
	}
	defer func() { _ = root.Remove(stage) }()
	after, err := m.composerVersion(ctx, phpBin, filepath.Join(m.paths.Composer, stage))
	if err != nil {
		return fmt.Errorf("validate downloaded Composer with PHP %s: %w", phpVersion, err)
	}
	if after.LessThan(before) {
		return fmt.Errorf("refusing Composer downgrade from %s to %s", before.Original(), after.Original())
	}
	if after.Equal(before) {
		log.Info("Composer is already up to date (%s)", before.Original())
		return nil
	}
	// The current layout shares one PHAR across every enabled PHP installation.
	versions, err := core.NewInstalledManager(m.paths).List()
	if err != nil {
		return fmt.Errorf("list PHP versions using shared Composer: %w", err)
	}
	for _, version := range versions {
		if version == phpVersion || !m.IsInstalled(version) {
			continue
		}
		otherPHP := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
		otherVersion, err := m.composerVersion(ctx, otherPHP, filepath.Join(m.paths.Composer, stage))
		if err != nil {
			return fmt.Errorf("shared Composer update is incompatible with enabled PHP %s: %w", version, err)
		}
		if !otherVersion.Equal(after) {
			return fmt.Errorf("inconsistent Composer version reported by PHP %s", version)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Rename(stage, "composer.phar"); err != nil {
		return fmt.Errorf("publish Composer update: %w", err)
	}
	log.Success("Composer updated: %s -> %s", before.Original(), after.Original())
	return nil
}

var composerVersionPattern = regexp.MustCompile(`(?m)^Composer version ([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][0-9A-Za-z.-]+)?)(?:[ \t\r\n]|$)`)

func (m *Manager) composerVersion(ctx context.Context, phpBin, phar string) (*semver.Version, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := process.CommandContext(probeCtx, phpBin, phar, "--version", "--no-ansi", "--no-interaction", "--no-plugins", "--no-scripts")
	cmd.Dir = m.paths.Root
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		if probeCtx.Err() != nil {
			return nil, probeCtx.Err()
		}
		return nil, fmt.Errorf("composer --version failed: %w: %s", redact.Error(err, ""), redact.Text(string(output)))
	}
	match := composerVersionPattern.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return nil, fmt.Errorf("unrecognized Composer version output: %s", redact.Text(string(output)))
	}
	version, err := semver.NewVersion(match[1])
	if err != nil {
		return nil, fmt.Errorf("invalid Composer version: %w", err)
	}
	return version, nil
}
