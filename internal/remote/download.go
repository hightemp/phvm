package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
)

// Downloader handles file downloads with progress.
type Downloader struct {
	client       *Client
	cacheDir     string
	showProgress bool
}

// NewDownloader creates a new Downloader.
func NewDownloader(client *Client, cacheDir string) *Downloader {
	return &Downloader{
		client:       client,
		cacheDir:     cacheDir,
		showProgress: true,
	}
}

// SetShowProgress enables or disables progress display.
func (d *Downloader) SetShowProgress(show bool) {
	d.showProgress = show
}

// Client returns the HTTP client used by this downloader.
func (d *Downloader) Client() *Client { return d.client }

// Download downloads a file, rechecking the cache while holding its lock.
func (d *Downloader) Download(ctx context.Context, url, filename string) (string, error) {
	return d.downloadCached(ctx, url, filename, DownloadChecks{})
}

// DownloadVerified verifies SHA256 before publishing and before reusing cache.
func (d *Downloader) DownloadVerified(ctx context.Context, url, filename, expected string) (string, error) {
	return d.DownloadChecked(ctx, url, filename, DownloadChecks{SHA256: expected})
}

// DownloadChecks defines checks performed before publication and cache reuse.
type DownloadChecks struct {
	SHA256   string
	Size     int64 // Zero means unspecified.
	Validate func(context.Context, string) error
}

// NormalizeSHA256 validates a required, hexadecimal SHA256 digest.
func NormalizeSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return "", fmt.Errorf("invalid expected SHA256")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid expected SHA256: %w", err)
	}
	return value, nil
}

// DownloadChecked validates a candidate before publishing it; an invalid cache
// is removed and fetched once. Failed fresh candidates are never published.
func (d *Downloader) DownloadChecked(ctx context.Context, url, filename string, checks DownloadChecks) (string, error) {
	if checks.SHA256 == "" && checks.Validate == nil {
		return "", fmt.Errorf("download verification is required")
	}
	if checks.SHA256 != "" {
		var err error
		checks.SHA256, err = NormalizeSHA256(checks.SHA256)
		if err != nil {
			return "", err
		}
	}
	if checks.Size < 0 {
		return "", fmt.Errorf("invalid expected download size")
	}
	return d.downloadCached(ctx, url, filename, checks)
}

func checkDownload(ctx context.Context, path string, checks DownloadChecks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("download must be a regular file")
	}
	if checks.Size > 0 && info.Size() != checks.Size {
		return fmt.Errorf("download size mismatch: got %d, expected %d", info.Size(), checks.Size)
	}
	if checks.SHA256 != "" {
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != checks.SHA256 {
			return fmt.Errorf("SHA256 mismatch for %s", filepath.Base(path))
		}
	}
	if checks.Validate != nil {
		if err := checks.Validate(ctx, path); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (d *Downloader) downloadCached(ctx context.Context, url, filename string, checks DownloadChecks) (string, error) {
	if err := fsutil.ValidateName(filename); err != nil {
		return "", err
	}
	path := filepath.Join(d.cacheDir, filename)
	err := fsutil.WithDirectoryLock(ctx, d.cacheDir, func() error {
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("cached download must be a regular file")
			}
			if checks.SHA256 == "" && checks.Validate == nil {
				return nil
			}
			checkErr := checkDownload(ctx, path, checks)
			if checkErr == nil {
				return nil
			}
			var pathError *os.PathError
			if errors.As(checkErr, &pathError) && !os.IsNotExist(checkErr) {
				return checkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			log.Info("Cached %s failed verification; downloading again", filename)
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return d.download(ctx, url, path, checks)
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// DownloadIfNotCached shares the locked cache check with Download.
func (d *Downloader) DownloadIfNotCached(ctx context.Context, url, filename string) (string, error) {
	return d.Download(ctx, url, filename)
}

// DownloadToPath atomically refreshes one destination without modifying its old inode.
func (d *Downloader) DownloadToPath(ctx context.Context, url, destPath string) error {
	return fsutil.WithDirectoryLock(ctx, filepath.Dir(destPath), func() error {
		return d.download(ctx, url, destPath, DownloadChecks{})
	})
}

func (d *Downloader) download(ctx context.Context, url, destPath string, checks DownloadChecks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, name := filepath.Dir(destPath), filepath.Base(destPath)
	if err := fsutil.ValidateName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("download destination must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	log.Info("Downloading %s", name)
	body, contentLength, err := d.client.Download(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	// Exclusive unique staging in the destination directory. Only this operation
	// removes this name; no descriptor ever writes to the published inode.
	tmp := ".download-" + fsutil.RandomSuffix()
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = root.Remove(tmp) }()
	var reader io.Reader = body
	if d.showProgress && contentLength > 0 {
		progress := log.NewProgressBar(log.ProgressOptions{Description: name, Total: contentLength, ShowBytes: true})
		reader = io.TeeReader(body, progress)
		defer func() { _ = progress.Finish() }()
	}
	hash := sha256.New()
	var writer io.Writer = f
	if checks.SHA256 != "" {
		writer = io.MultiWriter(f, hash)
	}
	written, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if contentLength >= 0 && written != contentLength {
		return fmt.Errorf("incomplete download: got %d, expected %d", written, contentLength)
	}
	if checks.SHA256 != "" && hex.EncodeToString(hash.Sum(nil)) != checks.SHA256 {
		return fmt.Errorf("SHA256 mismatch for %s", name)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// All semantic checks run on staging, before any consumer can reuse cache.
	if err := checkDownload(ctx, filepath.Join(dir, tmp), checks); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		return err
	}
	log.Success("Downloaded %s (%d bytes)", name, written)
	return nil
}

// IsCached checks if a file is cached.
func (d *Downloader) IsCached(filename string) bool {
	destPath := filepath.Join(d.cacheDir, filename)
	return fsutil.Exists(destPath)
}

// CachedPath returns the path to a cached file.
func (d *Downloader) CachedPath(filename string) string {
	return filepath.Join(d.cacheDir, filename)
}

// ClearCache removes all cached files.
func (d *Downloader) ClearCache() error {
	return d.ClearCacheContext(context.Background())
}

// ClearCacheContext waits for writers before clearing this cache.
func (d *Downloader) ClearCacheContext(ctx context.Context) error {
	return fsutil.WithDirectoryLock(ctx, d.cacheDir, func() error { return d.clearCache() })
}

func (d *Downloader) clearCache() error {
	entries, err := os.ReadDir(d.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		path := filepath.Join(d.cacheDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	return nil
}
