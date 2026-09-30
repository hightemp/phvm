package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func runProjectUse(t *testing.T, bin string, paths *core.Paths, cwd string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--phvm-dir", paths.Root, "--no-color", "use"}, args...)...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatal(err)
	return 0, "", ""
}

func TestUseProjectVersionFindsNearestFileAndResolvesInstalledVersion(t *testing.T) {
	withoutConfigEnv(t)
	t.Setenv("PHVM_VERSION", "9.9.9") // Installer setting must not select PHP.
	bin := buildTestCLI(t)
	for _, tc := range []struct {
		name, parent, nearest, want string
	}{
		{"parent directory", "7.4.33\n", "", "7.4.33"},
		{"nearest directory", "7.4.33\n", "8.3\n", "8.3.30"},
		{"local alias", "prod\n", "", "8.3.30"},
		{"installed latest", "latest\n", "", "8.4.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := seedInstalledVersions(t)
			if err := core.NewAliasManager(paths).Set("prod", "8.3"); err != nil {
				t.Fatal(err)
			}
			project := filepath.Join(t.TempDir(), "project")
			child := filepath.Join(project, "nested")
			cwd := filepath.Join(child, "deeper")
			if err := os.MkdirAll(cwd, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, ".php-version"), []byte(tc.parent), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.nearest != "" {
				if err := os.WriteFile(filepath.Join(child, ".php-version"), []byte(tc.nearest), 0600); err != nil {
					t.Fatal(err)
				}
			}
			code, stdout, diagnostics := runProjectUse(t, bin, paths, cwd)
			if code != 0 || stdout != "" || !strings.Contains(diagnostics, "Now using PHP "+tc.want) {
				t.Fatalf("use project: exit=%d stdout=%q stderr=%q", code, stdout, diagnostics)
			}
			if current, err := core.NewCurrentManager(paths).Get(); err != nil || current != tc.want {
				t.Errorf("current=%q, error=%v; want %q", current, err, tc.want)
			}
		})
	}
}

func TestUseExplicitVersionIgnoresProjectFile(t *testing.T) {
	withoutConfigEnv(t)
	t.Setenv("PHVM_VERSION", "9.9.9")
	bin := buildTestCLI(t)
	paths := seedInstalledVersions(t)
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".php-version"), []byte("invalid project value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostics := runProjectUse(t, bin, paths, cwd, "8.4.5")
	if code != 0 {
		t.Fatalf("explicit version was affected by project file: %s", diagnostics)
	}
	if current, err := core.NewCurrentManager(paths).Get(); err != nil || current != "8.4.5" {
		t.Errorf("current=%q, error=%v", current, err)
	}
}

func TestUseProjectVersionErrorsPreserveCurrent(t *testing.T) {
	withoutConfigEnv(t)
	t.Setenv("PHVM_VERSION", "8.3.30")
	bin := buildTestCLI(t)
	for _, tc := range []struct {
		name, contents, wantError string
		makeDirectory             bool
		makeSymlink               bool
	}{
		{name: "missing", wantError: ".php-version"},
		{name: "empty", contents: "\n", wantError: "empty"},
		{name: "multiple values", contents: "8.3.30\n8.4.5\n", wantError: "one"},
		{name: "uninstalled", contents: "8.3.31\n", wantError: "not installed"},
		{name: "directory", makeDirectory: true, wantError: "regular file"},
		{name: "symlink", makeSymlink: true, wantError: "regular file"},
		{name: "oversized", contents: strings.Repeat("8", 5000), wantError: "too large"},
		{name: "nearest invalid masks parent", contents: "not-a-version", wantError: ".php-version"},
		{name: "secrets redacted", contents: "https://phvm-user:phvm-pass@example.invalid/?token=phvm-token", wantError: ".php-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := seedInstalledVersions(t)
			project := filepath.Join(t.TempDir(), "project")
			cwd := filepath.Join(project, "nested")
			if err := os.MkdirAll(cwd, 0755); err != nil {
				t.Fatal(err)
			}
			if tc.name == "nearest invalid masks parent" {
				if err := os.WriteFile(filepath.Join(project, ".php-version"), []byte("8.4.5\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(cwd, ".php-version")
			if tc.makeDirectory {
				if err := os.Mkdir(file, 0700); err != nil {
					t.Fatal(err)
				}
			} else if tc.makeSymlink {
				outside := filepath.Join(t.TempDir(), "outside-version")
				if err := os.WriteFile(outside, []byte("8.3.30\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, file); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if tc.name != "missing" {
				if err := os.WriteFile(file, []byte(tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			code, stdout, diagnostics := runProjectUse(t, bin, paths, cwd)
			if code != 1 || stdout != "" || !strings.Contains(diagnostics, tc.wantError) {
				t.Errorf("use error: exit=%d stdout=%q stderr=%q", code, stdout, diagnostics)
			}
			if tc.name == "secrets redacted" {
				for _, secret := range []string{"phvm-user", "phvm-pass", "phvm-token"} {
					if strings.Contains(diagnostics, secret) {
						t.Errorf("diagnostics exposed %q", secret)
					}
				}
			}
			if current, err := core.NewCurrentManager(paths).Get(); err != nil || current != "8.2.30" {
				t.Errorf("failed use changed current to %q: %v", current, err)
			}
		})
	}
}
