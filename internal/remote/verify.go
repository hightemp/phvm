package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// ErrGPGUnavailable indicates that a signature could not be checked.
var ErrGPGUnavailable = errors.New("GPG verification unavailable")

// VerifyGPG verifies using a fresh isolated keyring from the PHP trust anchor.
func (v *Verifier) VerifyGPG(ctx context.Context, filePath, ascPath, keyringURL string) error {
	_, err := v.verifyGPG(ctx, filePath, ascPath, keyringURL)
	return err
}

func (v *Verifier) verifyGPG(ctx context.Context, filePath, ascPath, keyringURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !v.gpgEnabled {
		return "", fmt.Errorf("%w: disabled", ErrGPGUnavailable)
	}
	if !v.IsGPGAvailable() {
		return "", fmt.Errorf("%w: gpg is not installed", ErrGPGUnavailable)
	}
	if !fsutil.Exists(ascPath) {
		return "", fmt.Errorf("%w: signature file is missing", ErrGPGUnavailable)
	}
	if keyringURL != PHPKeyringURL {
		return "", fmt.Errorf("untrusted PHP keyring URL")
	}
	home, err := os.MkdirTemp("", "phvm-gpg-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(home)
	keyring := filepath.Join(home, "php-keyring.gpg")
	downloader := NewDownloader(v.client, home)
	downloader.SetShowProgress(false)
	if err := downloader.DownloadToPath(ctx, PHPKeyringURL, keyring); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: fetch PHP keyring: %v", ErrGPGUnavailable, err)
	}
	options := []string{"--batch", "--no-tty", "--no-options", "--homedir", home}
	importArgs := append(append([]string{}, options...), "--import", keyring)
	output, err := exec.CommandContext(ctx, "gpg", importArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("import trusted PHP keyring: %w: %s", err, output)
	}
	verifyArgs := append(append([]string{}, options...), "--status-fd", "1", "--verify", ascPath, filePath)
	output, err = exec.CommandContext(ctx, "gpg", verifyArgs...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("invalid GPG signature: %w: %s", err, output)
	}
	signer := ""
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "[GNUPG:]" {
			continue
		}
		switch fields[1] {
		case "BADSIG", "ERRSIG", "NO_PUBKEY", "REVKEYSIG", "KEYREVOKED", "EXPSIG", "SIGEXPIRED":
			return "", fmt.Errorf("GPG rejected signature: %s", fields[1])
		}
		if fields[1] == "VALIDSIG" {
			if len(fields) < 11 {
				return "", fmt.Errorf("incomplete GPG signature status")
			}
			fingerprint := fields[2]
			if len(fingerprint) != 40 && len(fingerprint) != 64 {
				return "", fmt.Errorf("invalid GPG signer fingerprint")
			}
			if _, err := hex.DecodeString(fingerprint); err != nil {
				return "", fmt.Errorf("invalid GPG signer fingerprint")
			}
			signer = strings.ToUpper(fingerprint)
		}
	}
	if signer != "" {
		log.Success("GPG signature verified")
		return signer, nil
	}
	return "", fmt.Errorf("GPG did not report a valid signature")
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
	GPGFingerprint string
	GPGSkipReason  string
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

	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !v.gpgEnabled {
		result.GPGSkipped = true
		result.GPGSkipReason = "disabled by configuration"
		return result, nil
	}
	fingerprint, err := v.verifyGPG(ctx, filePath, ascPath, keyringURL)
	if err != nil {
		result.Errors = append(result.Errors, err)
		if v.gpgFallback && errors.Is(err, ErrGPGUnavailable) {
			result.GPGSkipped = true
			result.GPGSkipReason = err.Error()
			log.Warn("GPG verification skipped: %s", result.GPGSkipReason)
			return result, nil
		}
		return result, err
	}
	result.GPGVerified = true
	result.GPGFingerprint = fingerprint

	return result, nil
}

// DownloadAndVerify downloads a file and verifies its integrity.
func (v *Verifier) DownloadAndVerify(ctx context.Context, info *TarballInfo, keyringURL string) (string, *VerifyResult, error) {
	downloader := NewDownloader(v.client, v.cacheDir)

	// Download tarball
	tarballPath, err := downloader.DownloadVerified(ctx, info.URL, info.Filename, info.SHA256)
	if err != nil {
		return "", nil, fmt.Errorf("download tarball: %w", err)
	}

	// Download signature
	ascFilename := info.Filename + ".asc"
	ascPath := filepath.Join(v.cacheDir, ascFilename)
	if v.gpgEnabled && info.ASCURL != "" {
		_, err = downloader.Download(ctx, info.ASCURL, ascFilename)
		if err != nil {
			log.Debug("Failed to download signature: %v", err)
			// Continue without signature
		}
	}

	// Verify
	result, err := v.Verify(ctx, tarballPath, info.SHA256, ascPath, keyringURL)
	if err != nil {
		return "", result, err
	}

	if !result.SHA256Verified {
		return "", nil, fmt.Errorf("SHA256 verification failed")
	}

	return tarballPath, result, nil
}
