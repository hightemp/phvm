package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type gatedDownloadBody struct {
	blocked, release chan struct{}
	step             int
	fail             bool
}

func (b *gatedDownloadBody) Read(p []byte) (int, error) {
	switch b.step {
	case 0:
		b.step++
		return copy(p, "AAAAAA"), nil
	case 1:
		b.step++
		close(b.blocked)
		<-b.release
		return copy(p, "aaaaaa"), nil
	default:
		if b.fail {
			return 0, errors.New("broken body")
		}
		return 0, io.EOF
	}
}
func (*gatedDownloadBody) Close() error { return nil }

func concurrentDownloader(t *testing.T, body io.ReadCloser) (*Downloader, *atomic.Int32) {
	t.Helper()
	client := NewClient(DefaultClientOptions())
	calls := &atomic.Int32{}
	client.httpClient.HTTPClient.Transport = verificationTransport(func(*http.Request) (*http.Response, error) {
		response := body
		if calls.Add(1) > 1 {
			response = io.NopCloser(strings.NewReader("BBBBBBBBBBBB"))
		}
		return &http.Response{StatusCode: 200, Body: response, Header: make(http.Header), ContentLength: 12}, nil
	})
	d := NewDownloader(client, filepath.Join(t.TempDir(), "cache"))
	d.SetShowProgress(false)
	return d, calls
}

func TestConcurrentCacheDownloadsRecheckAfterWaiting(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	d, calls := concurrentDownloader(t, &gatedDownloadBody{blocked: blocked, release: release})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := d.Download(context.Background(), "https://fixture.test/archive", "archive.tgz")
		first <- err
	}()
	<-blocked
	go func() {
		_, err := d.DownloadIfNotCached(context.Background(), "https://fixture.test/archive", "archive.tgz")
		second <- err
	}()
	var early bool
	select {
	case <-second:
		early = true
		t.Error("second cache writer bypassed in-flight download")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("first download failed: %v", err)
	}
	if !early {
		if err := <-second; err != nil {
			t.Errorf("second download failed: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("cache was not rechecked after waiting: requests=%d", calls.Load())
	}
	if data, err := os.ReadFile(d.CachedPath("archive.tgz")); err != nil || string(data) != "AAAAAAaaaaaa" {
		t.Errorf("published bytes=%q error=%v", data, err)
	}
}

func TestConcurrentFailedRefreshDoesNotCorruptGoodPublication(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	d, _ := concurrentDownloader(t, &gatedDownloadBody{blocked: blocked, release: release, fail: true})
	path := filepath.Join(d.cacheDir, "artifact")
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- d.DownloadToPath(context.Background(), "https://fixture.test/bad", path) }()
	<-blocked
	go func() { second <- d.DownloadToPath(context.Background(), "https://fixture.test/good", path) }()
	var early bool
	select {
	case <-second:
		early = true
		t.Error("refresh writers did not serialize")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err == nil {
		t.Error("broken response accepted")
	}
	if !early {
		if err := <-second; err != nil {
			t.Fatalf("good refresh failed: %v", err)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "BBBBBBBBBBBB" {
		t.Errorf("good publication corrupted: %q %v", data, err)
	}
	entries, err := os.ReadDir(d.cacheDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("foreign staging remains: %v %v", entries, err)
	}
}

func TestCacheClearWaitsForInFlightDownload(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	d, _ := concurrentDownloader(t, &gatedDownloadBody{blocked: blocked, release: release})
	first, cleared := make(chan error, 1), make(chan error, 1)
	go func() { _, err := d.Download(context.Background(), "https://fixture.test/a", "a.tgz"); first <- err }()
	<-blocked
	go func() { cleared <- d.ClearCache() }()
	var early bool
	select {
	case <-cleared:
		early = true
		t.Error("cache clear deleted active staging")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("cache clear broke download: %v", err)
	}
	if !early {
		if err := <-cleared; err != nil {
			t.Fatal(err)
		}
	}
	if files, _ := os.ReadDir(d.cacheDir); len(files) != 0 {
		t.Errorf("cache not cleared after writer: %v", files)
	}
}

func TestCancelledCacheWaiterDoesNotFetchOrChangePublishedBytes(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	d, calls := concurrentDownloader(t, &gatedDownloadBody{blocked: blocked, release: release})
	done := make(chan error, 1)
	go func() { _, err := d.Download(context.Background(), "https://fixture.test/a", "a.tgz"); done <- err }()
	<-blocked
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := d.Download(ctx, "https://fixture.test/b", "a.tgz"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("wait cancellation=%v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("cancelled waiter fetched payload: %d", calls.Load())
	}
	if data, err := os.ReadFile(d.CachedPath("a.tgz")); err != nil || string(data) != "AAAAAAaaaaaa" {
		t.Errorf("active payload changed: %q %v", data, err)
	}
}

func TestFailedAndCancelledRefreshPreservesExistingFile(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "truncated", true: "cancelled"}[cancelled], func(t *testing.T) {
			client := NewClient(DefaultClientOptions())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client.httpClient.HTTPClient.Transport = verificationTransport(func(*http.Request) (*http.Response, error) {
				if cancelled {
					cancel()
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("partial")), Header: make(http.Header), ContentLength: 100}, nil
			})
			d := NewDownloader(client, t.TempDir())
			d.SetShowProgress(false)
			path := filepath.Join(d.cacheDir, "active")
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := d.DownloadToPath(ctx, "https://fixture.test/a", path); err == nil {
				t.Error("invalid refresh accepted")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
				t.Errorf("old publication changed: %q %v", data, err)
			}
			if files, _ := os.ReadDir(d.cacheDir); len(files) != 1 {
				t.Errorf("staging not cleaned: %v", files)
			}
		})
	}
}

func TestVerifiedDownloadValidatesBeforePublishingAndRechecksCachedHash(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "bad response", true: "bad cached archive"}[cached], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "php.tgz")
			const good = "valid archive"
			payload := "invalid archive"
			if cached {
				payload = good
				if err := os.WriteFile(path, []byte("old invalid cache"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := NewClient(DefaultClientOptions())
			client.httpClient.HTTPClient.Transport = verificationTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header), ContentLength: int64(len(payload))}, nil
			})
			verifier := NewVerifier(client, dir)
			verifier.SetGPGEnabled(false)
			hash := sha256.Sum256([]byte(good))
			_, _, err := verifier.DownloadAndVerify(context.Background(), &TarballInfo{URL: "https://fixture.test/php.tgz", Filename: "php.tgz", SHA256: hex.EncodeToString(hash[:])}, PHPKeyringURL)
			if cached {
				if err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != good {
					t.Error("invalid cache was not replaced by verified bytes")
				}
			} else {
				if err == nil {
					t.Error("checksum mismatch accepted")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Error("unverified payload published in cache")
				}
			}
		})
	}
}
