package composer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func seedCompatiblePHP(t *testing.T) *core.Paths {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP launcher fixture")
	}
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for version, phpID := range map[string]int{"5.2.17": 50217, "5.3.2": 50302, "5.6.40": 50640, "8.3.30": 80330} {
		if err := os.MkdirAll(p.VersionBin(version), 0755); err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf("#!/bin/sh\nIFS=: read -r composer_version min_php < \"$1\"\nif [ %d -lt \"$min_php\" ]; then echo 'incompatible PHP' >&2; exit 7; fi\nprintf 'Composer version %%s 2026-09-30 00:00:00\\n' \"$composer_version\"\n", phpID)
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), core.PHPBinary()), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestComposerExplicitVersionAndInvalidCandidatePreserveExistingPHP(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	m.client = &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/versions" {
			t.Error("explicit Composer version fetched automatic catalog")
			return nil, fmt.Errorf("unexpected catalog request")
		}
		return composerChannelTransport(t, map[string]int{"2.2.29": 50300, "2.10.3": 70205}).RoundTrip(req)
	})}
	if err := m.InstallVersion(context.Background(), "5.6.40", "2.2.29"); err != nil {
		t.Fatal(err)
	}
	active := m.PHPPharPath("5.6.40")
	before, err := os.ReadFile(active)
	if err != nil || string(before) != "2.2.29:50300\n" {
		t.Fatalf("exact Composer version was not installed: %q %v", before, err)
	}
	for _, requested := range []string{"../2.10.3", "2.2", "2.10.3/../../escape"} {
		if err := m.InstallVersion(context.Background(), "5.6.40", requested); err == nil {
			t.Errorf("unsafe or partial Composer version %q accepted", requested)
		}
	}
	if err := m.InstallVersion(context.Background(), "5.6.40", "2.10.3"); err == nil || !strings.Contains(err.Error(), "incompatible PHP") {
		t.Errorf("incompatible explicitly requested Composer accepted: %v", err)
	}
	wrong := []byte("2.2.30:50300\n")
	sum := sha256.Sum256(wrong)
	m.client = &http.Client{Transport: updateTransport(wrong, hex.EncodeToString(sum[:]), 200)}
	if err := m.InstallVersion(context.Background(), "5.6.40", "2.2.29"); err == nil || !strings.Contains(err.Error(), "does not match requested") {
		t.Errorf("wrong release at verified URL was accepted: %v", err)
	}
	after, err := os.ReadFile(active)
	if err != nil || string(after) != string(before) {
		t.Errorf("failed install changed working Composer: %q %v", after, err)
	}
}

func TestComposerUpdateLeavesOlderPHPOnItsCompatibleRelease(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	m.client = &http.Client{Transport: composerChannelTransport(t, map[string]int{"2.10.3": 70205, "2.2.30": 50300})}
	for _, version := range []string{"5.6.40", "8.3.30"} {
		if err := m.Install(context.Background(), version); err != nil {
			t.Fatal(err)
		}
	}
	base := composerChannelTransport(t, map[string]int{"2.10.4": 70205, "2.2.30": 50300})
	m.client = &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/versions" {
			body := `{"stable":[{"version":"2.2.30","min-php":50300,"path":"/download/2.2.30/composer.phar"},{"version":"2.10.4","min-php":70205,"path":"/download/2.10.4/composer.phar"}]}`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return base.RoundTrip(req)
	})}
	if err := m.Update(context.Background(), "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(context.Background(), "5.6.40"); err != nil {
		t.Fatal(err)
	}
	for version, want := range map[string]string{"5.6.40": "2.2.30", "8.3.30": "2.10.4"} {
		out, err := exec.Command(filepath.Join(p.VersionBin(version), "composer"), "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), "Composer version "+want) {
			t.Errorf("PHP %s Composer changed unexpectedly: %v %s", version, err, out)
		}
	}
}

func TestComposerRejectsPHPWithoutSupportedReleaseBeforeDownload(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	m.client = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		t.Error("unsupported PHP triggered Composer network request")
		return nil, fmt.Errorf("unexpected request")
	})}
	if err := m.Install(context.Background(), "5.2.17"); err == nil || !strings.Contains(err.Error(), "5.3.2") {
		t.Errorf("unsupported PHP was not diagnosed: %v", err)
	}
	if _, err := os.Stat(m.PHPPharPath("5.2.17")); !os.IsNotExist(err) {
		t.Errorf("unsupported PHP got a PHAR: %v", err)
	}
}

func TestComposerMigratesSharedPHARWithoutCouplingFutureInstalls(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	shared := filepath.Join(p.Composer, "composer.phar")
	if err := os.WriteFile(shared, []byte("2.2.30:50300\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"5.6.40", "8.3.30"} {
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), "composer"), m.launcher(version, shared), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.MigrateLegacy(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"5.6.40", "8.3.30"} {
		if data, err := os.ReadFile(m.PHPPharPath(version)); err != nil || string(data) != "2.2.30:50300\n" {
			t.Errorf("PHP %s not migrated independently: %q %v", version, data, err)
		}
		launcher, err := os.ReadFile(filepath.Join(p.VersionBin(version), "composer"))
		if err != nil || !strings.Contains(string(launcher), m.PHPPharPath(version)) {
			t.Errorf("PHP %s launcher still shared: %q %v", version, launcher, err)
		}
	}
	m.client = &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/versions" {
			return catalogResponse("2.10.3"), nil
		}
		return composerChannelTransport(t, map[string]int{"2.10.3": 70205}).RoundTrip(req)
	})}
	if err := m.Install(context.Background(), "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(m.PHPPharPath("5.6.40")); err != nil || string(data) != "2.2.30:50300\n" {
		t.Errorf("new PHP install changed old PHP Composer: %q %v", data, err)
	}
	if data, err := os.ReadFile(shared); err != nil || string(data) != "2.2.30:50300\n" {
		t.Errorf("migration removed the old source before completion: %q %v", data, err)
	}
}

func TestComposerMigrationPreservesCachedGlobalSeedWithoutLaunchers(t *testing.T) {
	p := seedCompatiblePHP(t)
	legacy := filepath.Join(p.Downloads, "composer.phar")
	if err := os.WriteFile(legacy, []byte("2.2.30:50300\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(p)
	if err := m.MigrateLegacy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(p.Composer, "composer.phar")); err != nil || string(data) != "2.2.30:50300\n" {
		t.Errorf("unassigned Composer seed would be lost on cache clear: %q %v", data, err)
	}
}

func TestComposerEnableRejectsIncompatibleStoredPHAR(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing launcher=%v", existing), func(t *testing.T) {
			p := seedCompatiblePHP(t)
			m := NewManager(p)
			path := m.PHPPharPath("5.6.40")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("2.10.3:70205\n"), 0600); err != nil {
				t.Fatal(err)
			}
			launcher := filepath.Join(p.VersionBin("5.6.40"), "composer")
			if existing {
				if err := os.WriteFile(launcher, m.launcher("5.6.40", path), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Enable("5.6.40"); err == nil || !strings.Contains(err.Error(), "incompatible PHP") {
				t.Errorf("incompatible stored Composer was enabled: %v", err)
			}
			if !existing {
				if _, err := os.Stat(launcher); !os.IsNotExist(err) {
					t.Errorf("failed enable published a launcher: %v", err)
				}
			}
		})
	}
}

func TestComposerInstallRollsBackPHARWhenLauncherCannotPublish(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	active := m.PHPPharPath("8.3.30")
	if err := os.MkdirAll(filepath.Dir(active), 0755); err != nil {
		t.Fatal(err)
	}
	old := []byte("2.9.0:70205\n")
	if err := os.WriteFile(active, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(p.VersionBin("8.3.30"), "composer"), 0700); err != nil {
		t.Fatal(err)
	}
	m.client = &http.Client{Transport: composerChannelTransport(t, map[string]int{"2.10.3": 70205})}
	if err := m.InstallVersion(context.Background(), "8.3.30", "2.10.3"); err == nil {
		t.Fatal("unpublishable launcher reported success")
	}
	if data, err := os.ReadFile(active); err != nil || string(data) != string(old) {
		t.Errorf("failed install replaced working PHAR: %q %v", data, err)
	}
}

func TestComposerUpdateForNewPHPIsNotBlockedByOldSharedLauncher(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	shared := filepath.Join(p.Composer, "composer.phar")
	if err := os.WriteFile(shared, []byte("2.10.3:70205\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLauncher := filepath.Join(p.VersionBin("5.6.40"), "composer")
	if err := os.WriteFile(oldLauncher, m.launcher("5.6.40", shared), 0755); err != nil {
		t.Fatal(err)
	}
	active := m.PHPPharPath("8.3.30")
	if err := os.MkdirAll(filepath.Dir(active), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, []byte("2.10.3:70205\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), "composer"), m.launcher("8.3.30", active), 0755); err != nil {
		t.Fatal(err)
	}
	m.client = &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/versions" {
			return catalogResponse("2.10.4"), nil
		}
		return composerChannelTransport(t, map[string]int{"2.10.4": 70205}).RoundTrip(req)
	})}
	if err := m.Update(context.Background(), "8.3.30"); err != nil {
		t.Fatalf("PHP 8.3 update was blocked by unrelated PHP 5.6: %v", err)
	}
	if data, err := os.ReadFile(active); err != nil || string(data) != "2.10.4:70205\n" {
		t.Errorf("selected PHP did not update: %q %v", data, err)
	}
	if data, err := os.ReadFile(oldLauncher); err != nil || string(data) != string(m.launcher("5.6.40", shared)) {
		t.Errorf("unrelated old PHP launcher changed: %q %v", data, err)
	}
}

func TestComposerRejectsDowngradeToHTTPBeforeRequest(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	insecureRequest := false
	m.client.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" {
			insecureRequest = true
			return nil, fmt.Errorf("insecure request attempted")
		}
		header := make(http.Header)
		header.Set("Location", "http://example.invalid/composer.phar")
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: header, Request: req}, nil
	})
	if err := m.Install(context.Background(), "8.3.30"); err == nil {
		t.Fatal("insecure redirect accepted")
	}
	if insecureRequest {
		t.Error("Composer followed HTTPS redirect to HTTP")
	}
}

func composerChannelTransport(t *testing.T, versions map[string]int) http.RoundTripper {
	t.Helper()
	return testTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "getcomposer.org" {
			t.Errorf("unexpected Composer host: %s", req.URL.Host)
			return nil, fmt.Errorf("unexpected host")
		}
		path := req.URL.Path
		if path == "/versions" {
			body := `{"stable":[{"version":"2.10.3","min-php":70205,"path":"/download/2.10.3/composer.phar"},{"version":"2.2.30","min-php":50300,"path":"/download/2.2.30/composer.phar"}]}`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		version := strings.TrimPrefix(path, "/download/")
		version = strings.SplitN(version, "/", 2)[0]
		if version == "latest-stable" {
			version = "2.10.3"
		}
		minimum, ok := versions[version]
		if !ok {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
		}
		payload := []byte(fmt.Sprintf("%s:%d\n", version, minimum))
		if strings.HasSuffix(path, ".sha256sum") {
			sum := sha256.Sum256(payload)
			payload = []byte(hex.EncodeToString(sum[:]) + "  composer.phar\n")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header), ContentLength: int64(len(payload))}, nil
	})
}

func TestComposerInstallKeepsIndependentCompatiblePHARs(t *testing.T) {
	p := seedCompatiblePHP(t)
	m := NewManager(p)
	m.client = &http.Client{Transport: composerChannelTransport(t, map[string]int{"2.10.3": 70205, "2.2.30": 50300})}
	for _, version := range []string{"5.3.2", "5.6.40", "8.3.30"} {
		if err := m.Install(context.Background(), version); err != nil {
			t.Fatalf("install for PHP %s: %v", version, err)
		}
	}
	for version, want := range map[string]string{"5.3.2": "2.2.30", "5.6.40": "2.2.30", "8.3.30": "2.10.3"} {
		launcher := filepath.Join(p.VersionBin(version), "composer")
		out, err := exec.Command(launcher, "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), "Composer version "+want) {
			t.Errorf("PHP %s Composer: %v %s; want %s", version, err, out, want)
		}
		phar := filepath.Join(p.Composer, version, "composer.phar")
		data, err := os.ReadFile(phar)
		if err != nil || !strings.HasPrefix(string(data), want+":") {
			t.Errorf("PHP %s has no independent PHAR: %q %v", version, data, err)
		}
	}
}
