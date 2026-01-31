package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// Download downloads a file to the cache directory.
// Returns the path to the downloaded file.
func (d *Downloader) Download(ctx context.Context, url, filename string) (string, error) {
	destPath := filepath.Join(d.cacheDir, filename)

	// Check if already cached
	if fsutil.Exists(destPath) {
		log.Debug("Using cached file: %s", destPath)
		return destPath, nil
	}

	// Ensure cache directory exists
	if err := fsutil.EnsureDir(d.cacheDir); err != nil {
		return "", fmt.Errorf("create cache directory: %w", err)
	}

	log.Info("Downloading %s", filename)

	// Start download
	body, contentLength, err := d.client.Download(ctx, url)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer body.Close()

	// Create temp file
	tmpPath := destPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	defer func() {
		tmpFile.Close()
		if fsutil.Exists(tmpPath) {
			os.Remove(tmpPath)
		}
	}()

	// Setup progress bar if enabled
	var reader io.Reader = body
	if d.showProgress && contentLength > 0 {
		progress := log.NewProgressBar(log.ProgressOptions{
			Description: filename,
			Total:       contentLength,
			ShowBytes:   true,
		})
		reader = io.TeeReader(body, progress)
		defer progress.Finish()
	}

	// Copy to file
	written, err := io.Copy(tmpFile, reader)
	if err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close file: %w", err)
	}

	// Verify size if known
	if contentLength > 0 && written != contentLength {
		return "", fmt.Errorf("incomplete download: got %d, expected %d", written, contentLength)
	}

	// Move to final location
	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("rename temp file: %w", err)
	}

	log.Success("Downloaded %s (%d bytes)", filename, written)
	return destPath, nil
}

// DownloadIfNotCached downloads a file only if not already cached.
func (d *Downloader) DownloadIfNotCached(ctx context.Context, url, filename string) (string, error) {
	destPath := filepath.Join(d.cacheDir, filename)

	if fsutil.Exists(destPath) {
		return destPath, nil
	}

	return d.Download(ctx, url, filename)
}

// DownloadToPath downloads a file to a specific path.
func (d *Downloader) DownloadToPath(ctx context.Context, url, destPath string) error {
	// Ensure parent directory exists
	if err := fsutil.EnsureDir(filepath.Dir(destPath)); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	filename := filepath.Base(destPath)
	log.Info("Downloading %s", filename)

	body, contentLength, err := d.client.Download(ctx, url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer body.Close()

	// Create temp file
	tmpPath := destPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	defer func() {
		tmpFile.Close()
		if fsutil.Exists(tmpPath) {
			os.Remove(tmpPath)
		}
	}()

	var reader io.Reader = body
	if d.showProgress && contentLength > 0 {
		progress := log.NewProgressBar(log.ProgressOptions{
			Description: filename,
			Total:       contentLength,
			ShowBytes:   true,
		})
		reader = io.TeeReader(body, progress)
		defer progress.Finish()
	}

	written, err := io.Copy(tmpFile, reader)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	if contentLength > 0 && written != contentLength {
		return fmt.Errorf("incomplete download: got %d, expected %d", written, contentLength)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

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
