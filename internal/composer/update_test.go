package composer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
)

func realPHP(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP launcher fixture")
	}
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP unavailable")
	}
	if out, err := exec.Command(php, "-r", `exit(class_exists('Phar') ? 0 : 1);`).CombinedOutput(); err != nil {
		t.Skipf("Phar extension unavailable: %s", out)
	}
	return php
}

func testComposerPHAR(t *testing.T, php, version, behavior string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composer.phar")
	program := fmt.Sprintf(`<?php
if (in_array('self-update', $argv, true)) { file_put_contents(getenv('PHVM_TEST_SELF_UPDATE'), 'unexpected'); exit(0); }
file_put_contents(getenv('PHVM_TEST_VERSION_CALLS'), $argv[0]."\n", FILE_APPEND);
if (%q === 'exit') { fwrite(STDERR, "incompatible PHP runtime\n"); exit(7); }
if (%q === 'invalid') { echo "not Composer version output\n"; exit(0); }
if (%q === 'wait') { sleep(10); }
if (%q === 'incompatibleother' && getenv('PHVM_TEST_PHP_VERSION') === '8.2.30') { fwrite(STDERR, "incompatible enabled PHP\n"); exit(7); }
echo "Composer version %s 2026-09-29 00:00:00\n";
`, behavior, behavior, behavior, behavior, version)
	code := `$p=new Phar($argv[1], 0, 'phvm-test.phar'); $p['cli.php']=$argv[2]; $p->setStub("<?php Phar::mapPhar('phvm-test.phar'); require 'phar://phvm-test.phar/cli.php'; __HALT_COMPILER(); ?>"); $p->setSignatureAlgorithm(Phar::SHA256);`
	if out, err := exec.Command(php, "-d", "phar.readonly=0", "-r", code, path, program).CombinedOutput(); err != nil {
		t.Fatalf("create PHAR: %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func seedComposerUpdate(t *testing.T, php string, old []byte) (*Manager, *core.Paths) {
	t.Helper()
	p := core.NewPaths(filepath.Join(t.TempDir(), "phvm with spaces"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"8.3.30", "8.2.30"} {
		if err := os.MkdirAll(p.VersionBin(version), 0755); err != nil {
			t.Fatal(err)
		}
		// This regular launcher delegates to real PHP, preserving confinement checks.
		script := "#!/bin/sh\nexport PHVM_TEST_PHP_VERSION=" + version + "\nexec '" + strings.ReplaceAll(php, "'", "'\\''") + "' \"$@\"\n"
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), core.PHPBinary()), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.Downloads, "composer.phar"), old, 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(p)
	for _, version := range []string{"8.3.30", "8.2.30"} {
		if err := m.Enable(version); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PHVM_TEST_SELF_UPDATE", filepath.Join(p.Root, "self-update-called"))
	t.Setenv("PHVM_TEST_VERSION_CALLS", filepath.Join(p.Root, "version-calls"))
	return m, p
}

func updateTransport(payload []byte, checksum string, status int) testTransport {
	return func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		data := string(payload)
		if strings.HasSuffix(r.URL.Path, "sha256sum") {
			data = checksum
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header), ContentLength: int64(len(data))}, nil
	}
}

func TestUpdateRunsVerifiedPHARWithRealPHP(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	newPHAR := testComposerPHAR(t, php, "2.9.0", "")
	m, p := seedComposerUpdate(t, php, old)
	sum := sha256.Sum256(newPHAR)
	m.client = &http.Client{Transport: updateTransport(newPHAR, hex.EncodeToString(sum[:]), 200)}
	if err := m.Update(context.Background(), "8.3.30"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar"))
	if err != nil || string(data) != string(newPHAR) {
		t.Error("Update reported success without publishing the verified new PHAR")
	}
	calls, err := os.ReadFile(filepath.Join(p.Root, "version-calls"))
	if err != nil || !strings.Contains(string(calls), "composer.phar") || !strings.Contains(string(calls), "tmp-") {
		t.Errorf("old/new PHAR versions were not queried: %s %v", calls, err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "self-update-called")); !os.IsNotExist(err) {
		t.Error("Update delegated unverified writes to self-update")
	}
	for _, version := range []string{"8.3.30", "8.2.30"} {
		out, err := exec.Command(filepath.Join(p.VersionBin(version), "composer"), "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), "Composer version 2.9.0") {
			t.Errorf("shared wrapper %s did not see updated PHAR: %v %s", version, err, out)
		}
	}
}

func TestUpdateFailuresPreserveWorkingPHAR(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	for _, tt := range []struct {
		name, version, behavior, checksum string
		status                            int
	}{
		{name: "checksum mismatch", version: "2.9.0", checksum: strings.Repeat("0", 64), status: 200},
		{name: "download HTTP error", version: "2.9.0", status: 503},
		{name: "new runtime fails", version: "2.9.0", behavior: "exit", status: 200},
		{name: "other enabled PHP incompatible", version: "2.9.0", behavior: "incompatibleother", status: 200},
		{name: "unparseable version", version: "2.9.0", behavior: "invalid", status: 200},
		{name: "older candidate", version: "2.7.9", status: 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := testComposerPHAR(t, php, tt.version, tt.behavior)
			m, p := seedComposerUpdate(t, php, old)
			sum := sha256.Sum256(payload)
			checksum := tt.checksum
			if checksum == "" {
				checksum = hex.EncodeToString(sum[:])
			}
			m.client = &http.Client{Transport: updateTransport(payload, checksum, tt.status)}
			wrapper := filepath.Join(p.VersionBin("8.3.30"), "composer")
			before, err := os.ReadFile(wrapper)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Update(context.Background(), "8.3.30"); err == nil {
				t.Error("failed update reported success")
			}
			if tt.name == "checksum mismatch" {
				calls, err := os.ReadFile(filepath.Join(p.Root, "version-calls"))
				if err != nil || strings.Contains(string(calls), "tmp-") {
					t.Errorf("unverified download was executed: %s %v", calls, err)
				}
			}
			data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar"))
			if err != nil || string(data) != string(old) {
				t.Error("failed update replaced working PHAR")
			}
			after, err := os.ReadFile(wrapper)
			if err != nil || string(after) != string(before) {
				t.Error("failed update changed the wrapper")
			}
			out, err := exec.Command(wrapper, "--version").CombinedOutput()
			if err != nil || !strings.Contains(string(out), "Composer version 2.8.1") {
				t.Errorf("old Composer stopped working: %v %s", err, out)
			}
			files, _ := filepath.Glob(filepath.Join(p.Downloads, "*.tmp*"))
			if len(files) != 0 {
				t.Errorf("staging files remain: %v", files)
			}
		})
	}
}

func TestUpdateRejectsMissingOrUnsafeInstallation(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	for _, kind := range []string{"missing PHP", "missing launcher", "missing PHAR", "symlink PHAR", "directory PHAR", "bad version"} {
		t.Run(kind, func(t *testing.T) {
			m, p := seedComposerUpdate(t, php, old)
			active := filepath.Join(p.Downloads, "composer.phar")
			version := "8.3.30"
			switch kind {
			case "missing PHP":
				if err := os.Remove(filepath.Join(p.VersionBin(version), core.PHPBinary())); err != nil {
					t.Fatal(err)
				}
			case "missing launcher":
				if err := m.Disable(version); err != nil {
					t.Fatal(err)
				}
			case "missing PHAR":
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
			case "symlink PHAR":
				outside := filepath.Join(t.TempDir(), "outside.phar")
				if err := os.WriteFile(outside, old, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, active); err != nil {
					t.Fatal(err)
				}
			case "directory PHAR":
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(active, 0700); err != nil {
					t.Fatal(err)
				}
			case "bad version":
				version = "../../outside"
			}
			m.client = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				t.Error("invalid local installation triggered a network request")
				return nil, fmt.Errorf("unexpected network")
			})}
			if err := m.Update(context.Background(), version); err == nil {
				t.Error("invalid local installation accepted")
			}
		})
	}
}

func TestUpdateAlreadyCurrentDoesNotClaimVersionChange(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.9.0", "")
	m, p := seedComposerUpdate(t, php, old)
	sum := sha256.Sum256(old)
	m.client = &http.Client{Transport: updateTransport(old, hex.EncodeToString(sum[:]), 200)}
	var output bytes.Buffer
	previous := log.Default()
	t.Cleanup(func() { log.SetDefault(previous) })
	log.SetDefault(log.New(&output, log.LevelNormal))
	if err := m.Update(context.Background(), "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Composer updated") || !strings.Contains(output.String(), "already up to date (2.9.0)") {
		t.Errorf("misleading update result: %s", output.String())
	}
	data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar"))
	if err != nil || string(data) != string(old) {
		t.Error("no-op changed active PHAR")
	}
	files, _ := filepath.Glob(filepath.Join(p.Downloads, "*.tmp*"))
	if len(files) != 0 {
		t.Errorf("no-op left staging files: %v", files)
	}
}

func TestUpdateCancellationPreservesWorkingPHAR(t *testing.T) {
	php := realPHP(t)
	for _, step := range []string{"before start", "current version", "download"} {
		t.Run(step, func(t *testing.T) {
			behavior := ""
			if step == "current version" {
				behavior = "wait"
			}
			old := testComposerPHAR(t, php, "2.8.1", behavior)
			m, p := seedComposerUpdate(t, php, old)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) { cancel(); return nil, context.Canceled })}
			if step == "before start" {
				cancel()
			}
			if step == "current version" {
				time.AfterFunc(100*time.Millisecond, cancel)
			}
			if err := m.Update(ctx, "8.3.30"); !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation not returned: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar"))
			if err != nil || string(data) != string(old) {
				t.Error("cancelled update changed active PHAR")
			}
			files, _ := filepath.Glob(filepath.Join(p.Downloads, "*.tmp*"))
			if len(files) != 0 {
				t.Errorf("cancelled update left staging files: %v", files)
			}
		})
	}
}

func TestConcurrentFailedUpdatePreservesPublishedPHAR(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	newPHAR := testComposerPHAR(t, php, "2.9.0", "")
	m, p := seedComposerUpdate(t, php, old)
	sum := sha256.Sum256(newPHAR)
	blocked, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	m.client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		var body io.ReadCloser
		if strings.HasSuffix(r.URL.Path, "sha256sum") {
			body = io.NopCloser(strings.NewReader(hex.EncodeToString(sum[:])))
		} else if calls.Add(1) == 1 {
			body = &waitingBody{blocked: blocked, release: release}
		} else {
			body = io.NopCloser(bytes.NewReader(newPHAR))
		}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})}
	done := make(chan error, 1)
	go func() { done <- m.Update(context.Background(), "8.3.30") }()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		close(release)
		<-done
		t.Fatal("update did not reach download barrier")
	}
	second := m.Update(context.Background(), "8.2.30")
	close(release)
	first := <-done
	if second != nil || first == nil {
		t.Fatalf("valid update=%v invalid update=%v", second, first)
	}
	data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar"))
	if err != nil || !bytes.Equal(data, newPHAR) {
		t.Error("failed concurrent update changed the published PHAR")
	}
	files, _ := filepath.Glob(filepath.Join(p.Downloads, "*.tmp*"))
	if len(files) != 0 {
		t.Errorf("concurrent update left staging files: %v", files)
	}
}
