package remote

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPECLReleaseRejectsWrongIdentityAndUntrustedMetadata(t *testing.T) {
	for _, tt := range []struct{ name, pkg, version, size, url string }{
		{"wrong name", "rediscluster", "6.0.0", "100", "https://pecl.php.net/get/redis-6.0.0"},
		{"wrong version", "redis", "6.1.0", "100", "https://pecl.php.net/get/redis-6.0.0"},
		{"missing size", "redis", "6.0.0", "", "https://pecl.php.net/get/redis-6.0.0"},
		{"negative size", "redis", "6.0.0", "-1", "https://pecl.php.net/get/redis-6.0.0"},
		{"foreign origin", "redis", "6.0.0", "100", "https://evil.invalid/get/redis-6.0.0"},
		{"downgrade", "redis", "6.0.0", "100", "http://pecl.php.net/get/redis-6.0.0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(ClientOptions{Retries: 0, Transport: verificationTransport(func(*http.Request) (*http.Response, error) {
				body := fmt.Sprintf("<r><p>%s</p><c>pecl.php.net</c><v>%s</v><f>%s</f><g>%s</g></r>", tt.pkg, tt.version, tt.size, tt.url)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			if _, err := NewPECLAPI(client).GetRelease(context.Background(), "redis", "6.0.0"); err == nil {
				t.Error("untrusted release metadata accepted")
			}
		})
	}
}

func TestPECLArchiveValidationRejectsSymlinkedInput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.tgz")
	out, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	data := []byte(`<package><name>redis</name><version><release>6.0.0</release></version></package>`)
	if err := tw.WriteHeader(&tar.Header{Name: "package.xml", Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "archive.tgz")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ValidatePECLArchive(context.Background(), link, "redis", "6.0.0", nil); err == nil {
		t.Error("validator followed an outside archive symlink")
	}
}

func TestClientRejectsHTTPSRedirectDowngrade(t *testing.T) {
	calls := 0
	client := NewClient(ClientOptions{Retries: 0, Transport: verificationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://evil.invalid/payload"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("payload"))}, nil
	})})
	resp, err := client.Get(context.Background(), "https://pecl.php.net/archive")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || calls != 1 {
		t.Errorf("downgrade fetched: calls=%d err=%v", calls, err)
	}
}
