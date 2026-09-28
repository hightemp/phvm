package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCheckPkgConfigLinksLibrary(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler is unavailable")
	}
	if _, err := exec.LookPath("pkg-config"); err != nil {
		t.Skip("pkg-config is unavailable")
	}
	ar, err := exec.LookPath("ar")
	if err != nil {
		t.Skip("ar is unavailable")
	}

	for _, tt := range []struct {
		name        string
		header      bool
		library     string
		useLDFlags  bool
		wantFound   bool
		wantProblem string
	}{
		{name: "stale metadata", header: true, wantProblem: "phvm_doctor_curl"},
		{name: "missing header", library: "void *curl_easy_init(void) { return 0; }", wantProblem: "missing curl development headers"},
		{name: "missing symbol", header: true, library: "int unrelated(void) { return 0; }", wantProblem: "curl_easy_init"},
		{name: "usable library with spaces in path", header: true, library: "void *curl_easy_init(void) { return 0; }", wantFound: true},
		{name: "linker flags from environment", header: true, library: "void *curl_easy_init(void) { return 0; }", useLDFlags: true, wantFound: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "library with spaces")
			includeDir := filepath.Join(root, "include")
			libDir := filepath.Join(root, "lib")
			pcDir := filepath.Join(root, "pkgconfig")
			for _, dir := range []string{filepath.Join(includeDir, "curl"), libDir, pcDir} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}

			t.Setenv("CC", strconv.Quote(cc))
			t.Setenv("CPPFLAGS", "")
			t.Setenv("CFLAGS", "")
			t.Setenv("LDFLAGS", "")
			t.Setenv("PKG_CONFIG_PATH", pcDir)
			t.Setenv("PKG_CONFIG_LIBDIR", pcDir)
			t.Setenv("PKG_CONFIG_SYSROOT_DIR", "")

			// The fixture cannot accidentally use headers from a host curl install.
			header := "#error missing curl development headers\n"
			if tt.header {
				header = "typedef void CURL;\nCURL *curl_easy_init(void);\n"
			}
			writeTestFile(t, filepath.Join(includeDir, "curl", "curl.h"), header)

			if tt.library != "" {
				source := filepath.Join(root, "curl.c")
				object := filepath.Join(root, "curl.o")
				writeTestFile(t, source, tt.library)
				if output, err := exec.Command(cc, "-c", source, "-o", object).CombinedOutput(); err != nil {
					t.Fatalf("build fixture: %v\n%s", err, output)
				}
				if output, err := exec.Command(ar, "rcs", filepath.Join(libDir, "libphvm_doctor_curl.a"), object).CombinedOutput(); err != nil {
					t.Fatalf("archive fixture: %v\n%s", err, output)
				}
			}

			libs := "-L\"${prefix}/lib\" -lphvm_doctor_curl"
			if tt.useLDFlags {
				libs = "-lphvm_doctor_curl"
				t.Setenv("LDFLAGS", "-L"+strconv.Quote(libDir))
			}
			writeTestFile(t, filepath.Join(pcDir, "libcurl.pc"), "prefix="+root+"\n"+
				"Name: libcurl\nDescription: doctor test fixture\nVersion: 8.4.0\n"+
				"Cflags: -I\"${prefix}/include\"\nLibs: "+libs+"\n")

			result := checkPkgConfig("libcurl", false)
			if result.Found != tt.wantFound {
				t.Fatalf("Found = %v, want %v; result: %+v", result.Found, tt.wantFound, result)
			}
			if result.Version != "8.4.0" {
				t.Errorf("Version = %q, want 8.4.0", result.Version)
			}
			if !tt.wantFound {
				if !strings.Contains(result.Problem, tt.wantProblem) {
					t.Errorf("Problem = %q, want evidence of %q", result.Problem, tt.wantProblem)
				}
				report := FormatResults(&DoctorResult{Checks: []CheckResult{result}, AllOK: true, Warnings: 1})
				if !strings.Contains(report, "UNUSABLE") || !strings.Contains(report, "compile/link") {
					t.Errorf("report must explain unusable library:\n%s", report)
				}
				if strings.Contains(report, "All dependencies are installed!") {
					t.Errorf("report falsely claims all dependencies are installed:\n%s", report)
				}
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
