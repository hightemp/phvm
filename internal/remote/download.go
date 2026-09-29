package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Download downloads a file, rechecking the cache while holding its lock.
func (d *Downloader) Download(ctx context.Context, url, filename string) (string, error) {
	return d.downloadCached(ctx, url, filename, "")
}

// DownloadVerified verifies SHA256 before publishing and before reusing cache.
func (d *Downloader) DownloadVerified(ctx context.Context, url, filename, expected string) (string, error) {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if len(expected) != 64 {
		return "", fmt.Errorf("invalid expected SHA256")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return "", fmt.Errorf("invalid expected SHA256: %w", err)
	}
	return d.downloadCached(ctx, url, filename, expected)
}

func (d *Downloader) downloadCached(ctx context.Context, url, filename, expected string) (string, error) {
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
			if expected == "" {
				return nil
			}
			root, err := os.OpenRoot(d.cacheDir)
			if err != nil {
				return err
			}
			defer root.Close()
			file, err := root.Open(filename)
			if err != nil {
				return err
			}
			defer file.Close()
			hash := sha256.New()
			if _, err := io.Copy(hash, file); err != nil {
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
			if hex.EncodeToString(hash.Sum(nil)) == expected {
				return ctx.Err()
			}
			log.Info("Cached %s failed SHA256; downloading again", filename)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return d.download(ctx, url, path, expected)
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
		return d.download(ctx, url, destPath, "")
	})
}

func (d *Downloader) download(ctx context.Context, url, destPath, expected string) error {
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
	if expected != "" {
		writer = io.MultiWriter(f, hash)
	}
	written, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if contentLength >= 0 && written != contentLength {
		return fmt.Errorf("incomplete download: got %d, expected %d", written, contentLength)
	}
	if expected != "" && hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("SHA256 mismatch for %s", name)
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
