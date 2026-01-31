package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
)

// Verifier handles integrity verification of downloaded files.
type Verifier struct {
	client      *Client
	cacheDir    string
	gpgEnabled  bool
	gpgFallback bool
}

// NewVerifier creates a new Verifier.
func NewVerifier(client *Client, cacheDir string) *Verifier {
	return &Verifier{
		client:      client,
		cacheDir:    cacheDir,
		gpgEnabled:  true,
		gpgFallback: true,
	}
}

// SetGPGEnabled enables or disables GPG verification.
func (v *Verifier) SetGPGEnabled(enabled bool) {
	v.gpgEnabled = enabled
}

// SetGPGFallback sets whether to fall back to SHA256 if GPG fails.
func (v *Verifier) SetGPGFallback(fallback bool) {
	v.gpgFallback = fallback
}

// VerifySHA256 verifies the SHA256 checksum of a file.
func (v *Verifier) VerifySHA256(filePath, expected string) error {
	if expected == "" {
		return fmt.Errorf("no SHA256 checksum provided")
	}

	log.Debug("Verifying SHA256 checksum...")

	computed, err := v.ComputeSHA256(filePath)
	if err != nil {
		return err
	}

	expected = strings.ToLower(strings.TrimSpace(expected))
	if computed != expected {
		return fmt.Errorf("SHA256 mismatch: got %s, expected %s", computed, expected)
	}

	log.Success("SHA256 checksum verified")
	return nil
}

// ComputeSHA256 computes the SHA256 checksum of a file.
func (v *Verifier) ComputeSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute hash: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyGPG verifies the GPG signature of a file.
func (v *Verifier) VerifyGPG(ctx context.Context, filePath, ascPath, keyringURL string) error {
	if !v.gpgEnabled {
		log.Debug("GPG verification disabled")
		return nil
	}

	// Check if gpg is available
	if !v.IsGPGAvailable() {
		if v.gpgFallback {
			log.Warn("GPG not available, skipping signature verification")
			return nil
		}
		return fmt.Errorf("GPG not available")
	}

	// Download keyring if needed
	keyringPath, err := v.ensureKeyring(ctx, keyringURL)
	if err != nil {
		if v.gpgFallback {
			log.Warn("Failed to get GPG keyring: %v", err)
			return nil
		}
		return fmt.Errorf("get keyring: %w", err)
	}

	// Download signature if not cached
	if !fsutil.Exists(ascPath) {
		return fmt.Errorf("signature file not found: %s", ascPath)
	}

	log.Debug("Verifying GPG signature...")

	// Import keyring
	importCmd := exec.CommandContext(ctx, "gpg", "--import", keyringPath)
	importCmd.Stderr = nil
	importCmd.Stdout = nil
	if err := importCmd.Run(); err != nil {
		log.Debug("GPG keyring import failed: %v", err)
		// Continue anyway, key might already be imported
	}

	// Verify signature
	verifyCmd := exec.CommandContext(ctx, "gpg", "--verify", ascPath, filePath)
	output, err := verifyCmd.CombinedOutput()
	if err != nil {
		if v.gpgFallback {
			log.Warn("GPG verification failed: %v", err)
			log.Debug("GPG output: %s", string(output))
			return nil
		}
		return fmt.Errorf("GPG verification failed: %w\nOutput: %s", err, string(output))
	}

	log.Success("GPG signature verified")
	return nil
}

// ensureKeyring downloads the keyring if not cached.
func (v *Verifier) ensureKeyring(ctx context.Context, keyringURL string) (string, error) {
	keyringPath := filepath.Join(v.cacheDir, "php-keyring.gpg")

	if fsutil.Exists(keyringPath) {
		return keyringPath, nil
	}

	log.Debug("Downloading PHP keyring...")

	downloader := NewDownloader(v.client, v.cacheDir)
	downloader.SetShowProgress(false)

	_, err := downloader.Download(ctx, keyringURL, "php-keyring.gpg")
	if err != nil {
		return "", err
	}

	return keyringPath, nil
}

// IsGPGAvailable checks if gpg is available on the system.
func (v *Verifier) IsGPGAvailable() bool {
	_, err := exec.LookPath("gpg")
	return err == nil
}

// VerifyResult holds the result of verification.
type VerifyResult struct {
	SHA256Verified bool
	GPGVerified    bool
	GPGSkipped     bool
	Errors         []error
}

// Verify performs full verification of a file.
func (v *Verifier) Verify(ctx context.Context, filePath, sha256sum, ascPath, keyringURL string) (*VerifyResult, error) {
	result := &VerifyResult{}

	// SHA256 verification (required)
	if err := v.VerifySHA256(filePath, sha256sum); err != nil {
		result.Errors = append(result.Errors, err)
		return result, fmt.Errorf("SHA256 verification failed: %w", err)
	}
	result.SHA256Verified = true

	// GPG verification (optional)
	if v.gpgEnabled {
		if fsutil.Exists(ascPath) {
			err := v.VerifyGPG(ctx, filePath, ascPath, keyringURL)
			if err != nil {
				result.Errors = append(result.Errors, err)
				if !v.gpgFallback {
					return result, fmt.Errorf("GPG verification failed: %w", err)
				}
			} else {
				result.GPGVerified = true
			}
		} else {
			log.Debug("No signature file found, skipping GPG verification")
			result.GPGSkipped = true
		}
	} else {
		result.GPGSkipped = true
	}

	return result, nil
}

// DownloadAndVerify downloads a file and verifies its integrity.
func (v *Verifier) DownloadAndVerify(ctx context.Context, info *TarballInfo, keyringURL string) (string, error) {
	downloader := NewDownloader(v.client, v.cacheDir)

	// Download tarball
	tarballPath, err := downloader.Download(ctx, info.URL, info.Filename)
	if err != nil {
		return "", fmt.Errorf("download tarball: %w", err)
	}

	// Download signature
	ascFilename := info.Filename + ".asc"
	ascPath := filepath.Join(v.cacheDir, ascFilename)
	if info.ASCURL != "" {
		_, err = downloader.Download(ctx, info.ASCURL, ascFilename)
		if err != nil {
			log.Debug("Failed to download signature: %v", err)
			// Continue without signature
		}
	}

	// Verify
	result, err := v.Verify(ctx, tarballPath, info.SHA256, ascPath, keyringURL)
	if err != nil {
		return "", err
	}

	if !result.SHA256Verified {
		return "", fmt.Errorf("SHA256 verification failed")
	}

	return tarballPath, nil
}
