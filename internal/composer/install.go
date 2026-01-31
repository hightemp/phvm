// Package composer provides Composer management.
package composer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
)

const (
	composerURL    = "https://getcomposer.org/download/latest-stable/composer.phar"
	composerSigURL = "https://getcomposer.org/download/latest-stable/composer.phar.sha256sum"
)

// Manager manages Composer installation.
type Manager struct {
	paths *core.Paths
}

// NewManager creates a new Manager.
func NewManager(paths *core.Paths) *Manager {
	return &Manager{paths: paths}
}

// Install installs Composer for a PHP version.
func (m *Manager) Install(ctx context.Context, phpVersion string) error {
	log.Info("Installing Composer for PHP %s", phpVersion)

	phpDir := m.paths.VersionDir(phpVersion)
	binDir := filepath.Join(phpDir, "bin")
	composerPath := filepath.Join(binDir, "composer")

	// Check if PHP is installed
	phpBin := filepath.Join(binDir, core.PHPBinary())
	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}

	// Download composer.phar
	log.Info("Downloading Composer...")

	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")
	if err := m.download(ctx, composerURL, pharPath); err != nil {
		return fmt.Errorf("download composer: %w", err)
	}

	// Verify checksum
	if err := m.verifyChecksum(ctx, pharPath); err != nil {
		log.Warn("Checksum verification failed: %v", err)
		// Continue anyway, it's just a warning
	}

	// Create wrapper script
	wrapper := fmt.Sprintf(`#!/bin/sh
exec "%s" "%s" "$@"
`, phpBin, pharPath)

	if err := fsutil.AtomicWriteFile(composerPath, []byte(wrapper), 0755); err != nil {
		return fmt.Errorf("create composer wrapper: %w", err)
	}

	log.Success("Composer installed for PHP %s", phpVersion)
	return nil
}

// InstallGlobal installs Composer globally (shared by all versions).
func (m *Manager) InstallGlobal(ctx context.Context) error {
	log.Info("Installing Composer globally...")

	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")

	// Download
	if err := m.download(ctx, composerURL, pharPath); err != nil {
		return fmt.Errorf("download composer: %w", err)
	}

	// Verify
	if err := m.verifyChecksum(ctx, pharPath); err != nil {
		log.Warn("Checksum verification failed: %v", err)
	}

	log.Success("Composer installed globally at %s", pharPath)
	return nil
}

// Update updates Composer.
func (m *Manager) Update(ctx context.Context, phpVersion string) error {
	binDir := m.paths.VersionBin(phpVersion)
	composerPath := filepath.Join(binDir, "composer")

	if !fsutil.Exists(composerPath) {
		return fmt.Errorf("Composer not installed for PHP %s", phpVersion)
	}

	phpBin := filepath.Join(binDir, core.PHPBinary())

	log.Info("Updating Composer...")

	cmd := exec.CommandContext(ctx, phpBin, composerPath, "self-update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("composer self-update: %w", err)
	}

	log.Success("Composer updated")
	return nil
}

// Enable enables Composer for a PHP version.
func (m *Manager) Enable(phpVersion string) error {
	binDir := m.paths.VersionBin(phpVersion)
	composerPath := filepath.Join(binDir, "composer")
	phpBin := filepath.Join(binDir, core.PHPBinary())
	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")

	if !fsutil.Exists(pharPath) {
		return fmt.Errorf("Composer not installed globally. Run: phvm composer install --global")
	}

	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}

	// Create wrapper script
	wrapper := fmt.Sprintf(`#!/bin/sh
exec "%s" "%s" "$@"
`, phpBin, pharPath)

	if err := fsutil.AtomicWriteFile(composerPath, []byte(wrapper), 0755); err != nil {
		return fmt.Errorf("create composer wrapper: %w", err)
	}

	log.Success("Composer enabled for PHP %s", phpVersion)
	return nil
}

// Disable removes Composer from a PHP version.
func (m *Manager) Disable(phpVersion string) error {
	binDir := m.paths.VersionBin(phpVersion)
	composerPath := filepath.Join(binDir, "composer")

	if fsutil.Exists(composerPath) {
		if err := os.Remove(composerPath); err != nil {
			return fmt.Errorf("remove composer: %w", err)
		}
	}

	log.Success("Composer disabled for PHP %s", phpVersion)
	return nil
}

// IsInstalled checks if Composer is installed for a PHP version.
func (m *Manager) IsInstalled(phpVersion string) bool {
	binDir := m.paths.VersionBin(phpVersion)
	composerPath := filepath.Join(binDir, "composer")
	return fsutil.Exists(composerPath)
}

// IsInstalledGlobally checks if Composer is installed globally.
func (m *Manager) IsInstalledGlobally() bool {
	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")
	return fsutil.Exists(pharPath)
}

// download downloads a file.
func (m *Manager) download(ctx context.Context, url, destPath string) error {
	if err := fsutil.EnsureDir(filepath.Dir(destPath)); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, destPath)
}

// verifyChecksum verifies the Composer checksum.
func (m *Manager) verifyChecksum(ctx context.Context, pharPath string) error {
	// Get expected checksum
	req, err := http.NewRequestWithContext(ctx, "GET", composerSigURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get checksum: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse checksum (format: "hash  filename")
	parts := string(body)
	expected := ""
	for i := 0; i < len(parts); i++ {
		if parts[i] == ' ' || parts[i] == '\t' || parts[i] == '\n' {
			expected = parts[:i]
			break
		}
	}
	if expected == "" {
		expected = string(body[:64])
	}

	// Compute actual checksum
	f, err := os.Open(pharPath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", actual, expected)
	}

	return nil
}
