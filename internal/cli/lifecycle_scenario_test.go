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
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func scenarioPHPArchive(t *testing.T, script string) []byte {
	t.Helper()
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
	return archive.Bytes()
}

func TestScenarioPHPFailureRetryAndUninstallLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX source backend")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	for _, failure := range []string{"resolve", "download", "checksum", "extract", "configure", "make", "install"} {
		t.Run(failure, func(t *testing.T) {
			p := core.NewPaths(filepath.Join(t.TempDir(), "root with spaces"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			// One configure script supports both the failed attempt and the retry.
			script := phpConfigureFixture(p.VersionDir("8.5.11"), "8.5.11", "")
			if failure == "configure" {
				script = strings.Replace(script, "cat > Makefile", "[ ! -e \"$PHVM_SCENARIO_FAIL\" ] || exit 7\ncat > Makefile", 1)
			}
			if failure == "make" {
				script = strings.Replace(script, "@true", "@test ! -e \"$$PHVM_SCENARIO_FAIL\"", 1)
			}
			if failure == "install" {
				script = strings.Replace(script, "\nEOF\n", "\n\ttest ! -e \"$$PHVM_SCENARIO_FAIL\"\nEOF\n", 1)
			}
			payload := scenarioPHPArchive(t, script)
			sum := sha256.Sum256(payload)
			flag := filepath.Join(p.Root, "fail")
			if err := os.WriteFile(flag, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PHVM_SCENARIO_FAIL", flag)
			var failing atomic.Bool
			failing.Store(true)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failing.Load() && failure == "resolve" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				data := payload
				digest := hex.EncodeToString(sum[:])
				if failing.Load() && failure == "checksum" {
					digest = strings.Repeat("0", 64)
				}
				if failing.Load() && failure == "extract" {
					data = []byte("not a tar archive")
					hash := sha256.Sum256(data)
					digest = hex.EncodeToString(hash[:])
				}
				if r.URL.Path == "/releases/" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": digest}}})
					return
				}
				if failing.Load() && failure == "download" {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)+30))
					_, _ = w.Write(data[:len(data)/2])
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			cfg := fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)
			if err := os.WriteFile(p.ConfigFile(), []byte(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"install", "8.5.11", "--profile", "minimal", "--jobs", "1"}
			code, out, diagnostics := commandStreams(t, bin, p, args...)
			if code != 1 || out != "" || strings.Contains(diagnostics, "installed successfully") {
				t.Fatalf("failed %s: exit=%d stdout=%s stderr=%s", failure, code, out, diagnostics)
			}
			if versions, err := core.NewInstalledManager(p).List(); err != nil || len(versions) != 0 {
				t.Fatalf("partial publication after %s: %v %v", failure, versions, err)
			}
			if tx, _ := filepath.Glob(filepath.Join(p.Versions, ".php-install-*")); len(tx) != 0 {
				t.Fatalf("transaction not cleaned: %v", tx)
			}
			if err := os.Remove(flag); err != nil {
				t.Fatal(err)
			}
			failing.Store(false)
			code, out, diagnostics = commandStreams(t, bin, p, args...)
			if code != 0 || out != "" || !strings.Contains(diagnostics, "installed successfully") {
				t.Fatalf("retry %s: exit=%d stdout=%s stderr=%s", failure, code, out, diagnostics)
			}
			metadata, err := core.LoadMetadata(p.VersionMetadata("8.5.11"))
			if err != nil || metadata.InstallationState != "ready" || !metadata.SHA256Verified {
				t.Fatalf("unready retry: %+v %v", metadata, err)
			}
			for _, command := range [][]string{{"current"}, {"which", "php"}, {"ls"}} {
				code, out, diag := commandStreams(t, bin, p, command...)
				if code != 0 || out == "" || diag != "" {
					t.Fatalf("query after retry %v: %d %q %q", command, code, out, diag)
				}
			}
			if code, _, _ := commandStreams(t, bin, p, "uninstall", "8.5.11"); code != 1 {
				t.Fatal("current PHP removed without force")
			}
			if code, _, diag := commandStreams(t, bin, p, "cache", "clear", "--all"); code != 0 {
				t.Fatalf("cleanup after retry: %s", diag)
			}
			if !core.NewInstalledManager(p).IsInstalled("8.5.11") {
				t.Fatal("cache clear removed PHP")
			}
			if code, _, diag := commandStreams(t, bin, p, "uninstall", "8.5.11", "--force"); code != 0 {
				t.Fatalf("uninstall: %s", diag)
			}
			if core.NewInstalledManager(p).IsInstalled("8.5.11") {
				t.Fatal("uninstall left registered PHP")
			}
			if code, _, _ := commandStreams(t, bin, p, "current"); code != 1 {
				t.Fatal("current survived forced uninstall")
			}
		})
	}
}
