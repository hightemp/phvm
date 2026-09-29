package composer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type gatedComposerBody struct {
	io.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (b *gatedComposerBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.Reader.Read(p)
}
func (*gatedComposerBody) Close() error { return nil }

func TestWaitingComposerUpdateRechecksVersionAndCannotDowngrade(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	higher := testComposerPHAR(t, php, "2.10.0", "")
	lower := testComposerPHAR(t, php, "2.9.0", "")
	first, p := seedComposerUpdate(t, php, old)
	second := NewManager(p)
	entered, release := make(chan struct{}), make(chan struct{})
	sum := sha256.Sum256(higher)
	first.client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(hex.EncodeToString(sum[:])))
		if !strings.HasSuffix(r.URL.Path, "sha256sum") {
			body = &gatedComposerBody{Reader: bytes.NewReader(higher), entered: entered, release: release}
		}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header), ContentLength: -1}, nil
	})}
	lowerHash := sha256.Sum256(lower)
	var lowerCalls atomic.Int32
	transport := updateTransport(lower, hex.EncodeToString(lowerHash[:]), 200)
	second.client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) { lowerCalls.Add(1); return transport(r) })}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first.Update(context.Background(), "8.3.30") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		<-firstDone
		t.Fatal("update did not reach barrier")
	}
	go func() { secondDone <- second.Update(context.Background(), "8.2.30") }()
	time.Sleep(100 * time.Millisecond)
	if lowerCalls.Load() != 0 {
		t.Error("second update fetched before acquiring shared state")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("stale update error=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(p.Composer, "composer.phar")); err != nil || !bytes.Equal(data, higher) {
		t.Error("waiting update replaced higher published version")
	}
}
