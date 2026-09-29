//go:build !windows

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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
)

func TestCLIInterruptedInstallCleansUpAndCanRetry(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	for _, signal := range []struct {
		name  string
		value os.Signal
		code  int
	}{{"interrupt", os.Interrupt, 130}, {"terminate", syscall.SIGTERM, 143}} {
		t.Run(signal.name, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			pidFile, beat, block := filepath.Join(p.Root, "pid"), filepath.Join(p.Root, "beat"), filepath.Join(p.Root, "block")
			if err := os.WriteFile(block, nil, 0600); err != nil {
				t.Fatal(err)
			}
			// Start a stubborn descendant during configure, before installation publication.
			script := phpConfigureFixture(p.VersionDir("8.5.11"), "8.5.11", "")
			barrier := "if [ \"$1\" != --help ] && [ -e \"$PHVM_TEST_BLOCK\" ]; then\nsh -c 'trap \"\" INT TERM; while :; do echo beat >> \"$PHVM_TEST_BEAT\"; sleep 0.02; done' &\necho $! > \"$PHVM_TEST_PID\"\nwait\nfi\n"
			script = strings.Replace(script, "#!/bin/sh\n", "#!/bin/sh\n"+barrier, 1)
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
			hash := sha256.Sum256(archive.Bytes())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/releases/":
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(hash[:])}}})
				case "/distributions/php-8.5.11.tar.gz":
					_, _ = w.Write(archive.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			if err := os.WriteFile(p.ConfigFile(), []byte(fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--phvm-dir", p.Root, "--no-color", "install", "8.5.11", "--profile", "minimal", "--jobs", "1"}
			cmd := exec.Command(bin, args...)
			cmd.Env = append(os.Environ(), "PHVM_TEST_BLOCK="+block, "PHVM_TEST_PID="+pidFile, "PHVM_TEST_BEAT="+beat)
			var out, diagnostics bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostics
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var pid int
			deadline := time.After(10 * time.Second)
			for pid == 0 {
				data, _ := os.ReadFile(pidFile)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if pid > 0 {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("configure barrier not reached: %v %s", err, diagnostics.String())
				case <-deadline:
					_ = cmd.Process.Kill()
					<-done
					t.Fatal("configure barrier timed out")
				case <-time.After(10 * time.Millisecond):
				}
			}
			defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
			if err := cmd.Process.Signal(signal.value); err != nil {
				_ = cmd.Process.Kill()
				<-done
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != signal.code {
					t.Errorf("signal exit=%v want=%d stderr=%s", err, signal.code, diagnostics.String())
				}
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(pid, syscall.SIGKILL)
				_ = cmd.Process.Kill()
				<-done
				t.Fatal("interrupted install did not finish")
			}
			first, _ := os.ReadFile(beat)
			time.Sleep(100 * time.Millisecond)
			second, _ := os.ReadFile(beat)
			if !bytes.Equal(first, second) {
				t.Error("descendant remains active")
			}
			if out.Len() != 0 || strings.Contains(diagnostics.String(), "installed successfully") {
				t.Errorf("interruption reported success: stdout=%s stderr=%s", &out, &diagnostics)
			}
			if core.NewInstalledManager(p).IsInstalled("8.5.11") {
				t.Error("partial PHP installation published")
			}
			if tx, _ := filepath.Glob(filepath.Join(p.Versions, ".php-install-*")); len(tx) > 0 {
				t.Errorf("transaction cleanup failed: %v", tx)
			}
			if err := os.Remove(block); err != nil {
				t.Fatal(err)
			}
			retry := exec.Command(bin, args...)
			retry.Env = cmd.Env
			if data, err := retry.CombinedOutput(); err != nil {
				t.Fatalf("retry after cancellation: %v %s", err, data)
			}
			if !core.NewInstalledManager(p).IsInstalled("8.5.11") {
				t.Error("retry did not publish PHP")
			}
		})
	}
}
