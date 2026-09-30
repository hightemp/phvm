package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
)

func TestInstallAppliesConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture requires POSIX configure/make")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	toolsDir := fakeBuildTools(t)
	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	makeWrapper := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> \"$PHVM_TEST_MAKE_CAPTURE\"\nexec '%s' \"$@\"\n", strings.ReplaceAll(realMake, "'", "'\\''"))
	if err := os.WriteFile(filepath.Join(toolsDir, "make"), []byte(makeWrapper), 0755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name                string
		env                 map[string]string
		args                []string
		profile, jobs, flag string
		wantErr             string
	}{
		{name: "TOML defaults", profile: "minimal", jobs: "3", flag: "--with-pear"},
		{name: "environment", env: map[string]string{"PHVM_PROFILE": "full", "PHVM_JOBS": "4", "PHVM_CONFIGURE_FLAGS": `["--without-curl"]`}, profile: "full", jobs: "4", flag: "--without-curl"},
		{name: "explicit CLI", env: map[string]string{"PHVM_PROFILE": "full", "PHVM_JOBS": "4"}, args: []string{"--profile", "common", "--jobs", "1", "--configure", "--without-zlib", "--skip-gpg"}, profile: "common", jobs: "1", flag: "--without-zlib"},
		{name: "legacy skip alias", args: []string{"--skip-verify"}, profile: "minimal", jobs: "3", flag: "--with-pear"},
		{name: "unavailable GPG fallback", env: map[string]string{"PHVM_GPG": "true", "PHVM_GPG_FALLBACK_SHA256": "true"}, profile: "minimal", jobs: "3", flag: "--with-pear"},
		{name: "unavailable GPG strict", env: map[string]string{"PHVM_GPG": "true", "PHVM_GPG_FALLBACK_SHA256": "false"}, wantErr: "GPG verification unavailable"},
		{name: "SHA256 mandatory with skip GPG", args: []string{"--skip-gpg"}, wantErr: "SHA256 mismatch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			prefix := p.VersionDir("8.5.11")
			php := filepath.Join(p.VersionBin("8.5.11"), core.PHPBinary())
			capture := filepath.Join(p.Root, "make-args")
			t.Setenv("PHVM_TEST_MAKE_CAPTURE", capture)
			script := phpConfigureFixture(p.VersionDir("8.5.11"), "8.5.11", "")
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
			checksum := hex.EncodeToString(sum[:])
			if tt.wantErr == "SHA256 mismatch" {
				checksum = strings.Repeat("0", 64)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != "config-test-agent" {
					t.Errorf("configured User-Agent not used: %s", r.Header.Get("User-Agent"))
				}
				if r.URL.Path == "/releases/" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": checksum}}})
					return
				}
				if r.URL.Path == "/distributions/php-8.5.11.tar.gz.asc" && tt.env["PHVM_GPG"] == "true" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Path == "/distributions/php-8.5.11.tar.gz" {
					_, _ = w.Write(archive.Bytes())
					return
				}
				t.Errorf("unexpected verification request: %s", r.URL)
				http.NotFound(w, r)
			}))
			defer server.Close()
			config := fmt.Sprintf("[general]\ndefault_profile='minimal'\nparallel_jobs=3\ncolor=false\n[remote]\nmirror=%q\nuser_agent='config-test-agent'\nretries=0\n[verify]\ngpg=false\n[build]\ndefault_flags=['--with-pear']\n", server.URL)
			if err := os.WriteFile(p.ConfigFile(), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--phvm-dir", p.Root, "install", "8.5.11"}, tt.args...)
			out, err := exec.Command(bin, args...).CombinedOutput()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(string(out), tt.wantErr) {
					t.Errorf("install must reject verification: %v\n%s", err, out)
				}
				if _, err := os.Stat(php); !os.IsNotExist(err) {
					t.Error("PHP published after verification failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("configured install: %v\n%s", err, out)
			}
			metadata, err := core.LoadMetadata(filepath.Join(prefix, ".phvm-metadata.json"))
			if err != nil {
				t.Fatal(err)
			}
			if metadata.BuildProfile != tt.profile {
				t.Errorf("profile=%s want=%s", metadata.BuildProfile, tt.profile)
			}
			found := false
			for _, flag := range metadata.ConfigureFlags {
				if flag == tt.flag {
					found = true
				}
			}
			if !found {
				t.Errorf("flag %s missing: %v", tt.flag, metadata.ConfigureFlags)
			}
			flags, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains("\n"+string(flags), "\n-j"+tt.jobs+"\n") {
				t.Errorf("jobs not applied to make invocation: %s", flags)
			}
			if strings.Contains(string(out), "\x1b[") {
				t.Errorf("color=false still produced ANSI: %s", out)
			}
			if metadata.GPGVerified || !metadata.GPGSkipped || !metadata.SHA256Verified {
				t.Errorf("verification settings ignored: %+v", metadata)
			}
		})
	}
}

func TestHTTPConfigurationControlsRetriesAndTimeout(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name     string
		env      map[string]string
		args     []string
		requests int32
		timeout  bool
	}{
		{name: "TOML zero retries", requests: 1},
		{name: "environment retry", env: map[string]string{"PHVM_RETRIES": "1"}, requests: 2},
		{name: "CLI zero overrides environment", env: map[string]string{"PHVM_RETRIES": "1"}, args: []string{"--retries", "0"}, requests: 1},
		{name: "environment timeout", env: map[string]string{"PHVM_TIMEOUT": "1"}, requests: 1, timeout: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tt.timeout {
					<-r.Context().Done()
					return
				}
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			p := core.NewPaths(t.TempDir())
			cfg := core.DefaultConfig()
			cfg.Remote.Mirror, cfg.Remote.Timeout, cfg.Remote.Retries = server.URL, 30, 0
			if err := cfg.Save(p.ConfigFile()); err != nil {
				t.Fatal(err)
			}
			// One retry can wait up to 30 seconds with the client's jitter policy.
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			args := append([]string{"--phvm-dir", p.Root, "install", "8.5.11"}, tt.args...)
			out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("HTTP settings did not terminate request: %v %s", err, out)
			}
			if err == nil || requests.Load() != tt.requests {
				t.Errorf("requests=%d want=%d error=%v output=%s", requests.Load(), tt.requests, err, out)
			}
			if tt.timeout && !strings.Contains(string(out), "Client.Timeout") {
				t.Errorf("timeout did not reach HTTP client: %s", out)
			}
		})
	}
}
