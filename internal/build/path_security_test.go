package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestStripSystemIncludeRejectsSymlinkPaths(t *testing.T) {
	for _, pathKind := range []string{"makefile", "build-directory"} {
		t.Run(pathKind, func(t *testing.T) {
			paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
			if err := paths.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			buildDir := paths.BuildPath("8.3.30")
			externalDir := t.TempDir()
			external := filepath.Join(externalDir, "Makefile")
			const original = "CFLAGS_CLEAN = -I/usr/include -DKEEP\n"
			if err := os.WriteFile(external, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			link := buildDir
			target := externalDir
			if pathKind == "makefile" {
				if err := os.MkdirAll(buildDir, 0755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(buildDir, "Makefile")
				target = external
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := stripSystemInclude(paths, buildDir); err == nil {
				t.Error("accepted a symlink-controlled Makefile path")
			}
			got, err := os.ReadFile(external)
			if err != nil || string(got) != original {
				t.Errorf("outside Makefile changed: %q, %v", got, err)
			}
		})
	}
}

func TestStripSystemIncludeUpdatesRegularMakefile(t *testing.T) {
	paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	buildDir := paths.BuildPath("8.3.30")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(buildDir, "Makefile")
	if err := os.WriteFile(file, []byte("CFLAGS_CLEAN = -I/usr/include -DKEEP\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := stripSystemInclude(paths, buildDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "CFLAGS_CLEAN = -DKEEP\n" {
		t.Errorf("Makefile = %q, %v", got, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("Makefile permissions changed: %v, %v", info, err)
	}
}
