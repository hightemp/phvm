package composer

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

	"github.com/hightemp/phvm/internal/core"
)

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenBody) Close() error             { return nil }

func TestComposerDownloadFailuresPreserveInstallation(t *testing.T) {
	for _, tt := range []struct {
		name           string
		status         int
		broken, cancel bool
	}{
		{"HTTP error", 404, false, false}, {"broken body", 200, true, false}, {"cancelled", 200, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			phar := filepath.Join(p.Composer, "composer.phar")
			if err := os.WriteFile(phar, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			old := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = old })
			http.DefaultClient = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				body := io.NopCloser(strings.NewReader("payload"))
				if tt.broken {
					body = brokenBody{}
				}
				return &http.Response{StatusCode: tt.status, Body: body, Header: make(http.Header)}, nil
			})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			err := NewManager(p).InstallGlobal(ctx)
			if err == nil {
				t.Fatal("expected download failure")
			}
			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation identity lost: %v", err)
			}
			data, err := os.ReadFile(phar)
			if err != nil || string(data) != "old" {
				t.Errorf("installation changed: %q %v", data, err)
			}
			entries, err := os.ReadDir(p.Composer)
			if err != nil || len(entries) != 1 {
				t.Errorf("staging files remain: %v %v", entries, err)
			}
		})
	}
}

type waitingBody struct {
	step             int
	blocked, release chan struct{}
}

func (b *waitingBody) Read(p []byte) (int, error) {
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
		return 0, io.EOF
	}
}
func (*waitingBody) Close() error { return nil }

func TestConcurrentComposerVerificationPreservesGoodArtifact(t *testing.T) {
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	const good = "BBBBBBBBBBBB"
	sum := sha256.Sum256([]byte(good))
	checksum := hex.EncodeToString(sum[:]) + "  composer.phar\n"
	blocked, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		var body io.ReadCloser
		if strings.HasSuffix(r.URL.Path, "sha256sum") {
			body = io.NopCloser(strings.NewReader(checksum))
		} else if calls.Add(1) == 1 {
			body = &waitingBody{blocked: blocked, release: release}
		} else {
			body = io.NopCloser(strings.NewReader(good))
		}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})}
	m := NewManager(p)
	done := make(chan error, 1)
	go func() { done <- m.InstallGlobal(context.Background()) }()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		close(release)
		<-done
		t.Fatal("download did not reach barrier")
	}
	second := m.InstallGlobal(context.Background())
	close(release)
	first := <-done
	if second != nil {
		t.Errorf("valid concurrent install failed: %v", second)
	}
	if first == nil {
		t.Error("invalid concurrent payload was accepted")
	}
	data, err := os.ReadFile(filepath.Join(p.Composer, "composer.phar"))
	if err != nil || string(data) != good {
		t.Errorf("valid artifact was corrupted: %q %v", data, err)
	}
	entries, _ := os.ReadDir(p.Composer)
	if len(entries) != 1 {
		t.Errorf("staging files remain: %v", entries)
	}
}
