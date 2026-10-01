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
	// Launchers and PHP probes must resolve the same files from any working directory.
	resolved := *paths
	for _, path := range []*string{&resolved.Root, &resolved.Versions, &resolved.Downloads, &resolved.Composer} {
		if absolute, err := filepath.Abs(*path); err == nil {
			*path = absolute
		}
	}
	paths = &resolved
	client := *http.DefaultClient
	if client.Timeout == 0 {
		client.Timeout = 60 * time.Second
	}
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("refusing insecure Composer redirect")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many Composer redirects")
		}
		return nil
	}
	return &Manager{paths: paths, client: &client}
}

// Install installs Composer for a PHP version.
func (m *Manager) Install(ctx context.Context, phpVersion string) error {
	return m.InstallVersion(ctx, phpVersion, "")
}

// InstallVersion installs the newest compatible Composer or an exact release.
func (m *Manager) InstallVersion(ctx context.Context, phpVersion, composerVersion string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error {
		return m.install(locked, phpVersion, composerVersion)
	})
}

func (m *Manager) install(ctx context.Context, phpVersion, requested string) error {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return err
	}
	log.Info("Installing Composer for PHP %s", phpVersion)

	root, err := m.paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer root.Close()

	// Check if PHP is installed
	if info, err := root.Stat(filepath.Join("bin", core.PHPBinary())); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}
	release, err := m.selectRelease(ctx, phpVersion, requested)
	if err != nil {
		return err
	}
	storage, err := m.paths.OpenDataDir(m.phpComposerDir(phpVersion), true)
	if err != nil {
		return err
	}
	defer storage.Close()
	url, checksumURL := release.urls()
	log.Info("Downloading Composer %s...", release.Version)
	stage, err := m.stageVerified(ctx, storage, url, checksumURL)
	if err != nil {
		return fmt.Errorf("download Composer: %w", err)
	}
	defer func() { _ = storage.Remove(stage) }()
	pharPath := m.PHPPharPath(phpVersion)
	phpBin := filepath.Join(m.paths.VersionBin(phpVersion), core.PHPBinary())
	actual, err := m.composerVersion(ctx, phpBin, filepath.Join(m.phpComposerDir(phpVersion), stage))
	if err != nil {
		return fmt.Errorf("validate Composer %s with PHP %s: %w", release.Version, phpVersion, err)
	}
	if actual.Original() != release.Version {
		return fmt.Errorf("downloaded Composer version %s does not match requested %s", actual.Original(), release.Version)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	prior, err := regularPHAR(storage)
	if err != nil {
		return err
	}
	backup := ""
	if prior {
		backup, err = m.copyLegacyPHAR(ctx, storage, storage)
		if err != nil {
			return fmt.Errorf("backup previous Composer for PHP %s: %w", phpVersion, err)
		}
	}
	if err := storage.Rename(stage, pharName); err != nil {
		if backup != "" {
			_ = storage.Remove(backup)
		}
		return fmt.Errorf("publish Composer for PHP %s: %w", phpVersion, err)
	}
	wrapper := m.launcher(phpVersion, pharPath)
	if err := fsutil.AtomicWriteRoot(root, filepath.Join("bin", "composer"), wrapper, 0755); err != nil {
		var restoreErr error
		if backup != "" {
			restoreErr = storage.Rename(backup, pharName)
			if restoreErr != nil {
				if removeErr := storage.Remove(pharName); removeErr == nil {
					restoreErr = storage.Rename(backup, pharName)
				}
			}
		} else {
			restoreErr = storage.Remove(pharName)
		}
		if restoreErr != nil {
			return fmt.Errorf("create Composer wrapper: %w; previous PHAR recovery failed (backup %s): %v", err, backup, restoreErr)
		}
		return fmt.Errorf("create composer wrapper: %w", err)
	}
	if backup != "" {
		_ = storage.Remove(backup)
	}

	log.Success("Composer installed for PHP %s", phpVersion)
	return nil
}

// InstallGlobal downloads a shared seed for later per-PHP enablement.
func (m *Manager) InstallGlobal(ctx context.Context) error {
	return m.InstallGlobalVersion(ctx, "")
}

// InstallGlobalVersion downloads a shared seed for later per-PHP enablement.
func (m *Manager) InstallGlobalVersion(ctx context.Context, requested string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.installGlobal(locked, requested) })
}

func (m *Manager) installGlobal(ctx context.Context, requested string) error {
	log.Info("Installing Composer globally...")
	url, checksumURL := composerURL, composerSigURL
	if requested != "" {
		if !exactComposerVersion.MatchString(requested) {
			return fmt.Errorf("composer version must be an exact X.Y.Z release")
		}
		url, checksumURL = (composerRelease{Version: requested}).urls()
	}
	if err := m.MigrateLegacy(ctx); err != nil {
		return err
	}
	pharPath, err := m.installVerified(ctx, url, checksumURL)
	if err != nil {
		return fmt.Errorf("download composer: %w", err)
	}

	log.Success("Composer installed globally at %s", pharPath)
	return nil
}

// Enable enables Composer for a PHP version.
func (m *Manager) Enable(phpVersion string) error {
	return m.EnableContext(context.Background(), phpVersion)
}

// EnableContext coordinates launcher changes with shared Composer publication.
func (m *Manager) EnableContext(ctx context.Context, phpVersion string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.enable(locked, phpVersion) })
}

func (m *Manager) enable(ctx context.Context, phpVersion string) error {
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
	if err := m.migrateLegacy(ctx, phpVersion); err != nil {
		return err
	}
	pharPath := m.PHPPharPath(phpVersion)
	pharRoot, err := m.paths.OpenDataDir(m.phpComposerDir(phpVersion), false)
	if err != nil {
		return fmt.Errorf("composer not installed for PHP %s; run phvm composer install --php %s", phpVersion, phpVersion)
	}
	defer pharRoot.Close()
	found, err := regularPHAR(pharRoot)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("composer not installed for PHP %s; run phvm composer install --php %s", phpVersion, phpVersion)
	}

	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("PHP %s is not installed", phpVersion)
	}
	if _, err := m.composerVersion(ctx, phpBin, pharPath); err != nil {
		return fmt.Errorf("composer for PHP %s is unusable: %w", phpVersion, err)
	}

	// Create wrapper script
	wrapper := m.launcher(phpVersion, pharPath)

	if err := fsutil.AtomicWriteRoot(root, filepath.Join("bin", "composer"), wrapper, 0755); err != nil {
		return fmt.Errorf("create composer wrapper: %w", err)
	}

	log.Success("Composer enabled for PHP %s", phpVersion)
	return nil
}

// Disable removes Composer from a PHP version.
func (m *Manager) Disable(phpVersion string) error {
	return m.DisableContext(context.Background(), phpVersion)
}

// DisableContext coordinates launcher changes with shared Composer publication.
func (m *Manager) DisableContext(ctx context.Context, phpVersion string) error {
	return m.paths.WithStateLock(ctx, func(locked context.Context) error { return m.disable(locked, phpVersion) })
}

func (m *Manager) disable(ctx context.Context, phpVersion string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

// IsInstalledGlobally checks permanent and legacy regular PHAR files without mutation.
func (m *Manager) IsInstalledGlobally() bool {
	for _, dir := range []string{m.paths.Composer, m.paths.Downloads} {
		root, err := m.paths.OpenDataDir(dir, false)
		if err != nil {
			continue
		}
		found, err := regularPHAR(root)
		_ = root.Close()
		if err == nil && found {
			return true
		}
	}
	return false
}

func (m *Manager) installVerified(ctx context.Context, url, checksumURL string) (string, error) {
	root, err := m.paths.OpenDataDir(m.paths.Composer, true)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name, err := m.stageVerified(ctx, root, url, checksumURL)
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
	return m.pharPath(), nil
}

// stageVerified downloads and checks a unique file without replacing the active PHAR.
func (m *Manager) stageVerified(ctx context.Context, root *os.Root, url, checksumURL string) (name string, err error) {
	name = "composer.phar.tmp-" + fsutil.RandomSuffix()
	stageName := name
	defer func() {
		if err != nil {
			_ = root.Remove(stageName)
		}
	}()
	if err = m.download(ctx, url, root, name); err != nil {
		return "", err
	}
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	err = m.verifyChecksumReader(ctx, file, checksumURL)
	_ = file.Close()
	if err != nil {
		return "", fmt.Errorf("verify composer: %w", redact.Error(err, checksumURL))
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

func (m *Manager) verifyChecksumReader(ctx context.Context, source io.Reader, checksumURL string) error {
	// Get expected checksum
	req, err := http.NewRequestWithContext(ctx, "GET", checksumURL, nil)
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
