package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// The PHP archive/configure fixture never uses C; these tools isolate preflight
// from host library availability. Actual linking is covered by doctor tests.
func fakeBuildTools(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX build tool fixtures")
	}
	dir := t.TempDir()
	for name, script := range map[string]string{
		"fixture-cc":         "#!/bin/sh\ncase \"$1\" in\n--version) echo 'fixture cc 1.0';;\n-print-prog-name=ld) echo '/fixture/Homebrew/bin/ld';;\n*) exit 0;;\nesac\n",
		"fixture-pkg-config": "#!/bin/sh\nfor arg in \"$@\"; do module=$arg; done\ncase \"$1\" in\n--version) echo 1.0;;\n--exists) test \"$module\" != \"$PHVM_TEST_MISSING_MODULE\";;\n--modversion) if [ \"$module\" = \"$PHVM_TEST_VERSION_MODULE\" ]; then echo \"$PHVM_TEST_LIBRARY_VERSION\"; else echo 100.0.0; fi;;\n--variable=pcfiledir) echo '/fixture/stale/pkgconfig';;\n*) exit 0;;\nesac\n",
		"autoconf":           "#!/bin/sh\necho 'fixture 2.71'\n",
		"gpg":                "#!/bin/sh\necho 'fixture gpg 1.0'\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CC", filepath.Join(dir, "fixture-cc"))
	t.Setenv("CXX", filepath.Join(dir, "fixture-cc"))
	t.Setenv("PKG_CONFIG", filepath.Join(dir, "fixture-pkg-config"))
	for _, key := range []string{"CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS", "LIBS", "PHVM_TEST_MISSING_MODULE", "PHVM_TEST_VERSION_MODULE", "PHVM_TEST_LIBRARY_VERSION"} {
		t.Setenv(key, "")
	}
	return dir
}

func TestDoctorAcceptsVersionAndProfile(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	out, err := exec.Command(bin, "--phvm-dir", t.TempDir(), "doctor", "--php", "8.5.11", "--profile", "minimal").CombinedOutput()
	if strings.Contains(string(out), "unknown flag") || !strings.Contains(string(out), "PHP 8.5.11") || !strings.Contains(string(out), "profile: minimal") {
		t.Errorf("doctor cannot diagnose the selected PHP/profile: %v %s", err, out)
	}
}

func TestDoctorProfilesAndVersionRequirements(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	tests := []struct {
		name, version, profile, missing, module, libraryVersion string
		args                                                    []string
		fail                                                    bool
	}{
		{name: "minimal ignores curl", version: "8.5.11", profile: "minimal", missing: "libcurl"},
		{name: "common requires curl", version: "8.5.11", profile: "common", missing: "libcurl", fail: true},
		{name: "custom disable curl", version: "8.5.11", profile: "common", missing: "libcurl", args: []string{"--configure=--without-curl"}},
		{name: "full requires ICU", version: "8.5.11", profile: "full", missing: "icu-i18n", fail: true},
		{name: "PHP83 accepts curl760", version: "8.3.30", profile: "common", module: "libcurl", libraryVersion: "7.60.0"},
		{name: "PHP84 rejects curl760", version: "8.4.0", profile: "common", module: "libcurl", libraryVersion: "7.60.0", fail: true},
		{name: "PHP84 accepts curl761", version: "8.4.0", profile: "common", module: "libcurl", libraryVersion: "7.61.0"},
		{name: "PHP84 accepts sqlite377", version: "8.4.0", profile: "common", module: "sqlite3", libraryVersion: "3.7.7"},
		{name: "PHP85 rejects sqlite377", version: "8.5.11", profile: "common", module: "sqlite3", libraryVersion: "3.7.7", fail: true},
		{name: "PHP84 accepts ICU56", version: "8.4.0", profile: "full", module: "icu-uc", libraryVersion: "56.0"},
		{name: "PHP85 rejects ICU56", version: "8.5.11", profile: "full", module: "icu-uc", libraryVersion: "56.0", fail: true},
		{name: "excluded libzip", version: "8.5.11", profile: "full", module: "libzip", libraryVersion: "1.7.0", fail: true},
		{name: "invalid version", version: "not-a-version", profile: "minimal", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fakeBuildTools(t)
			t.Setenv("PHVM_TEST_MISSING_MODULE", tt.missing)
			t.Setenv("PHVM_TEST_VERSION_MODULE", tt.module)
			t.Setenv("PHVM_TEST_LIBRARY_VERSION", tt.libraryVersion)
			args := append([]string{"--phvm-dir", t.TempDir(), "doctor", "--php", tt.version, "--profile", tt.profile}, tt.args...)
			out, err := exec.Command(bin, args...).CombinedOutput()
			if (err != nil) != tt.fail {
				t.Errorf("error=%v wantFail=%v\n%s", err, tt.fail, out)
			}
			if tt.version == "not-a-version" {
				return
			}
			for _, s := range []string{filepath.Join(dir, "fixture-cc"), filepath.Join(dir, "fixture-pkg-config"), "linker selected by CC: /fixture/Homebrew/bin/ld", "PATH="} {
				if !strings.Contains(string(out), s) {
					t.Errorf("missing toolchain evidence %q: %s", s, out)
				}
			}
			if tt.fail && tt.module != "" && !strings.Contains(string(out), "version") {
				t.Errorf("version rejection lacks reason: %s", out)
			}
		})
	}
}

func TestInstallChecksRequirementsBeforeDownload(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	t.Setenv("PHVM_TEST_MISSING_MODULE", "libcurl")
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": strings.Repeat("0", 64)}}})
			return
		}
		downloads.Add(1)
		_, _ = w.Write([]byte("bad archive"))
	}))
	defer server.Close()
	root := t.TempDir()
	out, err := exec.Command(bin, "--phvm-dir", root, "install", "8.5.11", "--profile", "common", "--mirror", server.URL, "--gpg=false", "--retries", "0").CombinedOutput()
	if err == nil || downloads.Load() != 0 || !strings.Contains(string(out), "before source download") || !strings.Contains(string(out), "libcurl") {
		t.Errorf("preflight did not block download: requests=%d error=%v\n%s", downloads.Load(), err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "php", "8.5.11")); !os.IsNotExist(err) {
		t.Error("installation changed after failed preflight")
	}
}
