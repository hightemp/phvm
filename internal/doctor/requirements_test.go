package doctor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/toolchain"
)

func fakeDoctorTools(t *testing.T, compilerOK bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX tool fixtures")
	}
	dir := t.TempDir()
	for _, name := range []string{"make", "autoconf", "bison", "re2c", "tar", "gpg", "curl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho 'fixture 1.0'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	compiler := "#!/bin/sh\ncase \"$1\" in\n--version) echo 'fixture cc 1.0'; exit 0;;\n-print-prog-name=ld) echo '/home/linuxbrew/.linuxbrew/bin/ld'; exit 0;;\nesac\n"
	if compilerOK {
		compiler += "exit 0\n"
	} else {
		compiler += "echo 'incompatible Homebrew linker' >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "cc"), []byte(compiler), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg-config"), []byte("#!/bin/sh\ncase \"$1\" in\n--modversion) echo '100.0.0';;\n--variable=pcfiledir) echo '/usr/local/lib/pkgconfig';;\n--version) echo '1.0';;\n*) exit 0;;\nesac\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"CC", "CXX", "PKG_CONFIG", "CPPFLAGS", "CFLAGS", "LDFLAGS", "LIBS"} {
		t.Setenv(name, "")
	}
	return dir
}

func TestDoctorFailsForUnusableRequiredLibraries(t *testing.T) {
	fakeDoctorTools(t, false)
	result := Check()
	if result.AllOK || result.Errors == 0 {
		t.Errorf("common profile accepted unusable libraries: %+v", result)
	}
	for _, name := range []string{"libcurl (lib)", "libxml-2.0 (lib)", "readline (lib)"} {
		for _, check := range result.Checks {
			if check.Name == name && !check.Required {
				t.Errorf("%s must be required in common profile", name)
			}
		}
	}
}

func TestBzip2ProbeChecksLinking(t *testing.T) {
	fakeDoctorTools(t, false)
	if err := checkLibraryLink("bzip2"); err == nil || !strings.Contains(err.Error(), "incompatible Homebrew linker") {
		t.Errorf("bzip2 passed without a usable library/linker: %v", err)
	}
}

func TestPkgConfigOverrideIsUsed(t *testing.T) {
	dir := fakeDoctorTools(t, true)
	custom := filepath.Join(dir, "custom-pkg-config")
	if err := os.Rename(filepath.Join(dir, "pkg-config"), custom); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg-config"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PKG_CONFIG", custom)
	if result := checkPkgConfig("libcurl", true); !result.Found || result.Version != "100.0.0" {
		t.Errorf("custom PKG_CONFIG ignored: %+v", result)
	}
}

func TestUnusableLibraryDoesNotSuggestReinstall(t *testing.T) {
	result := &DoctorResult{AllOK: false, Errors: 1, Checks: []CheckResult{{Name: "libcurl (lib)", Required: true, Path: "/usr/local/lib/pkgconfig/libcurl.pc", Problem: "incompatible Homebrew linker"}}}
	if hint := GetInstallCommand(result); hint != "" {
		t.Errorf("unusable metadata was mistaken for missing package: %s", hint)
	}
	report := FormatResults(result)
	if strings.Contains(report, "To install missing packages") || !strings.Contains(report, "PATH") {
		t.Errorf("report must explain toolchain selection instead of reinstalling packages: %s", report)
	}
}

func TestMinimalRequiresWorkingCompiler(t *testing.T) {
	fakeDoctorTools(t, false)
	result, err := CheckFor(context.Background(), Options{PHPVersion: "8.5.11", Profile: "minimal", Paths: nil})
	if err != nil {
		t.Fatal(err)
	}
	if result.AllOK {
		t.Error("minimal profile accepted a compiler that cannot link any program")
	}
}

func TestExplicitLibraryFlagsOverrideStaleMetadata(t *testing.T) {
	dir := fakeDoctorTools(t, true)
	if err := os.WriteFile(filepath.Join(dir, "pkg-config"), []byte("#!/bin/sh\ncase \"$1\" in\n--modversion) echo 7.10.0;;\n--variable=pcfiledir) echo /fixture/stale;;\n*) exit 0;;\nesac\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURL_CFLAGS", "-I/fixture/explicit/include")
	t.Setenv("CURL_LIBS", "-L/fixture/explicit/lib -lcurl")
	probe := probeFor("libcurl")
	probe.minVersion = "7.61.0"
	if result := checkLibrary(context.Background(), toolchain.Current(), probe, true); !result.Found {
		t.Errorf("explicit configure flags were ignored: %+v", result)
	}
}

func TestPrivateLibrariesAreDeferredUntilBuilt(t *testing.T) {
	fakeDoctorTools(t, true)
	result, err := CheckFor(context.Background(), Options{PHPVersion: "7.4.33", Profile: "common", Paths: core.NewPaths(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if !result.AllOK || result.Deferred != 2 {
		t.Fatalf("private dependencies must be planned, not reported missing/usable: %+v", result)
	}
	report := FormatResults(result)
	if !strings.Contains(report, "DEFERRED") || strings.Contains(report, "All required build checks passed!") || GetInstallCommand(result) != "" {
		t.Errorf("misleading private dependency report: %s", report)
	}
}

func TestCheckForHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckFor(ctx, Options{PHPVersion: "8.5.11", Profile: "minimal", Paths: core.NewPaths(t.TempDir())}); err != context.Canceled {
		t.Errorf("cancellation ignored: %v", err)
	}
}
