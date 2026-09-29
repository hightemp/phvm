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
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstallRequiresVerifiedComposer(t *testing.T) {
	const payload = "new composer"
	sum := sha256.Sum256([]byte(payload))
	valid := hex.EncodeToString(sum[:]) + "  composer.phar\n"
	for _, global := range []bool{false, true} {
		for _, tt := range []struct {
			name, checksum string
			status         int
			wantErr        bool
		}{
			{"valid", valid, 200, false}, {"mismatch", strings.Repeat("0", 64), 200, true},
			{"empty", "", 200, true}, {"short", "oops", 200, true}, {"nonhex", strings.Repeat("z", 64), 200, true},
			{"checksum unavailable", "error", 503, true},
		} {
			t.Run(fmt.Sprintf("global=%v/%s", global, tt.name), func(t *testing.T) {
				p := core.NewPaths(t.TempDir())
				if err := p.EnsureDirectories(); err != nil {
					t.Fatal(err)
				}
				phar := filepath.Join(p.Composer, "composer.phar")
				if err := os.WriteFile(phar, []byte("old composer"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(p.VersionBin("8.3.30"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("fixture"), 0700); err != nil {
					t.Fatal(err)
				}
				old := http.DefaultClient
				t.Cleanup(func() { http.DefaultClient = old })
				http.DefaultClient = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
					body, status := payload, 200
					if strings.HasSuffix(r.URL.Path, "sha256sum") {
						body, status = tt.checksum, tt.status
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), ContentLength: int64(len(body))}, nil
				})}
				m := NewManager(p)
				var err error
				if global {
					err = m.InstallGlobal(context.Background())
				} else {
					err = m.Install(context.Background(), "8.3.30")
				}
				if (err != nil) != tt.wantErr {
					t.Errorf("Install error=%v, wantErr=%v", err, tt.wantErr)
				}
				data, readErr := os.ReadFile(phar)
				if readErr != nil {
					t.Fatal(readErr)
				}
				want := payload
				if tt.wantErr {
					want = "old composer"
				}
				if string(data) != want {
					t.Errorf("published %q, want %q", data, want)
				}
				if tt.wantErr && !global {
					if _, err := os.Stat(filepath.Join(p.VersionBin("8.3.30"), "composer")); !os.IsNotExist(err) {
						t.Error("wrapper published after failed verification")
					}
				}
				files, _ := filepath.Glob(filepath.Join(p.Composer, "*.tmp*"))
				if len(files) > 0 {
					t.Errorf("staging files left: %v", files)
				}
			})
		}
	}
}
