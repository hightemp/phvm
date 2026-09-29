package ext

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/remote"
)

type peclTestTransport func(*http.Request) (*http.Response, error)

func (f peclTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func peclManifestFixture(t *testing.T, archive []byte) string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var files strings.Builder
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "package.xml" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		hash := md5.Sum(data)
		fmt.Fprintf(&files, "<file name=%q md5sum=%q/>", strings.TrimPrefix(header.Name, "pecl_http-4.3.0/"), hex.EncodeToString(hash[:]))
	}
	return `<package version="2.0"><name>pecl_http</name><channel>pecl.php.net</channel><version><release>4.3.0</release></version><providesextension>http</providesextension><contents><dir name="/">` + files.String() + `</dir></contents></package>`
}

func TestPECLTrustFailureNeverRunsPhpize(t *testing.T) {
	for _, scenario := range []string{"wrong release", "size", "manifest checksum", "tampered bytes", "metadata unavailable", "truncated", "bad cache then fresh"} {
		t.Run(scenario, func(t *testing.T) {
			p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
			archive, err := os.ReadFile(p.ExtensionCachePath("pecl_http", "4.3.0"))
			if err != nil {
				t.Fatal(err)
			}
			manifest := peclManifestFixture(t, archive)
			version, size := "4.3.0", len(archive)
			if scenario == "wrong release" {
				version = "4.2.0"
			}
			if scenario == "size" {
				size++
			}
			if scenario == "manifest checksum" {
				start := strings.Index(manifest, `md5sum="`) + len(`md5sum="`)
				manifest = manifest[:start] + strings.Repeat("0", 32) + manifest[start+32:]
			}
			if scenario == "tampered bytes" {
				archive = modifiedPECLArchive(t, archive)
				size = len(archive)
				if err := os.WriteFile(p.ExtensionCachePath("pecl_http", "4.3.0"), archive, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "bad cache then fresh" || scenario == "truncated" {
				if err := os.WriteFile(p.ExtensionCachePath("pecl_http", "4.3.0"), []byte("bad cache"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(p.Root, "phpize-executed")
			if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), "phpize"), []byte("#!/bin/sh\ntouch "+shellLiteral(sentinel)+"\nexit 7\n"), 0755); err != nil {
				t.Fatal(err)
			}
			client := remote.NewClient(remote.ClientOptions{Retries: 0, Transport: peclTestTransport(func(req *http.Request) (*http.Response, error) {
				body := []byte(fmt.Sprintf("<r><p>pecl_http</p><c>pecl.php.net</c><v>%s</v><f>%d</f><g>https://pecl.php.net/get/pecl_http-4.3.0</g></r>", version, size))
				status := 200
				if strings.Contains(req.URL.Path, "package.") {
					body = []byte(manifest)
				}
				if strings.HasPrefix(req.URL.Path, "/get/") {
					body = archive
				}
				if scenario == "metadata unavailable" {
					status = 404
				}
				length := len(body)
				if scenario == "truncated" && strings.HasPrefix(req.URL.Path, "/get/") {
					body = body[:len(body)/2]
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), ContentLength: int64(length)}, nil
			})})
			err = NewInstaller(p, client).Install(context.Background(), InstallOptions{Name: "pecl_http", Version: "4.3.0", PHPVersion: "8.3.30"})
			_, statErr := os.Stat(sentinel)
			if scenario == "bad cache then fresh" {
				if statErr != nil || err == nil || !strings.Contains(err.Error(), "phpize") {
					t.Errorf("valid fresh archive did not reach phpize: %v %v", statErr, err)
				}
			} else if err == nil || !os.IsNotExist(statErr) {
				t.Errorf("trust failure reached phpize: %v %v", err, statErr)
			}
		})
	}
}

func modifiedPECLArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var out bytes.Buffer
	compressed := gzip.NewWriter(&out)
	tw := tar.NewWriter(compressed)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(header.Name, "/configure") {
			body = bytes.Replace(body, []byte("exit 0"), []byte("exit 7"), 1)
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
