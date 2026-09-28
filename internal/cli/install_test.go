package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestInstallMetadataRecordsDisabledGPG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("configure fixture requires POSIX tools")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	p := core.NewPaths(filepath.Join(t.TempDir(), "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile(), []byte("[verify]\ngpg=false\ngpg_fallback_sha256=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	php := filepath.Join(p.VersionBin("8.5.11"), core.PHPBinary())
	script := fmt.Sprintf("#!/bin/sh\ncat > Makefile <<'EOF'\nall:\n\t@true\ninstall:\n\tmkdir -p '%s'\n\tprintf '#!/bin/sh\\necho PHP 8.5.11\\n' > '%s'\n\tchmod 755 '%s'\nEOF\n", filepath.Dir(php), php, php)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "php-8.5.11/configure", Mode: 0755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(sum[:])}}})
			return
		}
		if r.URL.Path == "/distributions/php-8.5.11.tar.gz" {
			_, _ = w.Write(archive.Bytes())
			return
		}
		t.Errorf("unexpected request with GPG disabled: %s", r.URL)
		http.NotFound(w, r)
	}))
	defer server.Close()
	if err := os.WriteFile(p.ConfigFile(), []byte(fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\ngpg_fallback_sha256=false\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "phvm-test")
	build := exec.Command("go", "build", "-o", bin, "./cmd/phvm")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "--phvm-dir", p.Root, "install", "8.5.11", "--jobs", "1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install fixture: %v\n%s", err, out)
	}
	metadata, err := core.LoadMetadata(p.VersionMetadata("8.5.11"))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.GPGVerified || !metadata.GPGSkipped || !metadata.SHA256Verified || metadata.GPGSkipReason == "" {
		t.Fatalf("incorrect verification metadata: %+v", metadata)
	}
}
