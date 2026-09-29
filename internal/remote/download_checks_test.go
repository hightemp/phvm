package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckedDownloadDoesNotTreatReadErrorsAsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive")
	if err := os.WriteFile(path, []byte("known cache"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := NewClient(ClientOptions{Retries: 0, Transport: verificationTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("new bytes")), ContentLength: 9, Header: make(http.Header)}, nil
	})})
	d := NewDownloader(client, dir)
	d.SetShowProgress(false)
	_, err := d.DownloadChecked(context.Background(), "https://fixture.test/archive", "archive", DownloadChecks{Validate: func(context.Context, string) error {
		return &os.PathError{Op: "read", Path: path, Err: os.ErrPermission}
	}})
	if !errors.Is(err, os.ErrPermission) || calls != 0 {
		t.Errorf("read error triggered refresh: calls=%d err=%v", calls, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "known cache" {
		t.Error("read failure discarded cache")
	}
}

func TestCheckedDownloadBadCacheGetsOnlyOneReplacementAttempt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive")
	if err := os.WriteFile(path, []byte("bad cache"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := NewClient(ClientOptions{Retries: 0, Transport: verificationTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("bad again")), ContentLength: 9, Header: make(http.Header)}, nil
	})})
	d := NewDownloader(client, dir)
	d.SetShowProgress(false)
	_, err := d.DownloadChecked(context.Background(), "https://fixture.test/archive", "archive", DownloadChecks{Validate: func(context.Context, string) error { return errors.New("archive identity mismatch") }})
	if err == nil || calls != 1 {
		t.Errorf("replacement attempts=%d error=%v", calls, err)
	}
	if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
		t.Errorf("bad publication/staging survived: %v %v", files, err)
	}
}
