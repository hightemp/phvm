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
	"path/filepath"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

const (
	composerURL    = "https://getcomposer.org/download/latest-stable/composer.phar"
	composerSigURL = "https://getcomposer.org/download/latest-stable/composer.phar.sha256sum"
)

// Manager manages Composer installation.
type Manager struct {
	paths  *core.Paths
	client *http.Client
}

// NewManager creates a new Manager.
func NewManager(paths *core.Paths) *Manager {
	client := *http.DefaultClient
	if client.Timeout == 0 {
		client.Timeout = 60 * time.Second
	}
	return &Manager{paths: paths, client: &client}
}

// Install installs Composer for a PHP version.
func (m *Manager) Install(ctx context.Context, phpVersion string) error {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return err
	}
	log.Info("Installing Composer for PHP %s", phpVersion)

	binDir := m.paths.VersionBin(phpVersion)
	root, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer root.Close()

	// Check if PHP is installed
	phpBin := filepath.Join(binDir, core.PHPBinary())
	if _, err := root.Stat(filepath.Join("bin", core.PHPBinary())); err != nil {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}

	// Download composer.phar
	log.Info("Downloading Composer...")

	pharPath, err := m.installVerified(ctx)
	if err != nil {
		return fmt.Errorf("download composer: %w", err)
	}

	// Create wrapper script
	wrapper := fmt.Sprintf(`#!/bin/sh
exec "%s" "%s" "$@"
`, phpBin, pharPath)

	if err := fsutil.AtomicWriteRoot(root, filepath.Join("bin", "composer"), []byte(wrapper), 0755); err != nil {
		return fmt.Errorf("create composer wrapper: %w", err)
	}

	log.Success("Composer installed for PHP %s", phpVersion)
	return nil
}

// InstallGlobal installs Composer globally (shared by all versions).
func (m *Manager) InstallGlobal(ctx context.Context) error {
	log.Info("Installing Composer globally...")

	pharPath, err := m.installVerified(ctx)
	if err != nil {
		return fmt.Errorf("download composer: %w", err)
	}

	log.Success("Composer installed globally at %s", pharPath)
	return nil
}

// Enable enables Composer for a PHP version.
func (m *Manager) Enable(phpVersion string) error {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return err
	}
	root, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer root.Close()
	binDir := m.paths.VersionBin(phpVersion)
	phpBin := filepath.Join(binDir, core.PHPBinary())
	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")

	if !fsutil.Exists(pharPath) {
		return fmt.Errorf("composer not installed globally; run: phvm composer install --global")
	}

	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}

	// Create wrapper script
	wrapper := fmt.Sprintf(`#!/bin/sh
exec "%s" "%s" "$@"
`, phpBin, pharPath)

	if err := fsutil.AtomicWriteRoot(root, filepath.Join("bin", "composer"), []byte(wrapper), 0755); err != nil {
		return fmt.Errorf("create composer wrapper: %w", err)
	}

	log.Success("Composer enabled for PHP %s", phpVersion)
	return nil
}

// Disable removes Composer from a PHP version.
func (m *Manager) Disable(phpVersion string) error {
	root, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(filepath.Join("bin", "composer")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove composer: %w", err)
	}

	log.Success("Composer disabled for PHP %s", phpVersion)
	return nil
}

// IsInstalled checks if Composer is installed for a PHP version.
func (m *Manager) IsInstalled(phpVersion string) bool {
	root, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Stat(filepath.Join("bin", "composer"))
	return err == nil
}

// IsInstalledGlobally checks if Composer is installed globally.
func (m *Manager) IsInstalledGlobally() bool {
	pharPath := filepath.Join(m.paths.Downloads, "composer.phar")
	return fsutil.Exists(pharPath)
}

func (m *Manager) installVerified(ctx context.Context) (string, error) {
	root, err := m.paths.OpenDataDir(m.paths.Downloads, true)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name, err := m.stageVerified(ctx, root)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Remove(name) }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := root.Rename(name, "composer.phar"); err != nil {
		return "", err
	}
	return filepath.Join(m.paths.Downloads, "composer.phar"), nil
}

// stageVerified downloads and checks a unique file without replacing the active PHAR.
func (m *Manager) stageVerified(ctx context.Context, root *os.Root) (name string, err error) {
	name = "composer.phar.tmp-" + fsutil.RandomSuffix()
	stageName := name
	defer func() {
		if err != nil {
			_ = root.Remove(stageName)
		}
	}()
	if err = m.download(ctx, composerURL, root, name); err != nil {
		return "", err
	}
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	err = m.verifyChecksumReader(ctx, file)
	_ = file.Close()
	if err != nil {
		return "", fmt.Errorf("verify composer: %w", redact.Error(err, composerSigURL))
	}
	return name, nil
}

// download streams a file into a unique confined staging file.
func (m *Manager) download(ctx context.Context, url string, root *os.Root, name string) error {

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return redact.Error(err, url)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return redact.Error(err, url)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	defer f.Close()
	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	if resp.ContentLength > 0 && written != resp.ContentLength {
		return fmt.Errorf("incomplete composer download")
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func (m *Manager) verifyChecksumReader(ctx context.Context, source io.Reader) error {
	// Get expected checksum
	req, err := http.NewRequestWithContext(ctx, "GET", composerSigURL, nil)
	if err != nil {
		return err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get checksum: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return err
	}

	parts := strings.Fields(string(body))
	if len(body) > 4096 || len(parts) == 0 || len(parts) > 2 || len(parts[0]) != 64 {
		return fmt.Errorf("invalid composer SHA256 response")
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return fmt.Errorf("invalid composer SHA256: %w", err)
	}
	if len(parts) == 2 && strings.TrimPrefix(parts[1], "*") != "composer.phar" {
		return fmt.Errorf("unexpected composer checksum filename")
	}
	expected := strings.ToLower(parts[0])

	h := sha256.New()
	if _, err := io.Copy(h, source); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", actual, expected)
	}

	return nil
}
