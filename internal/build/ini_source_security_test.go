package build

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestPostInstallRejectsPhpIniSourceSymlink(t *testing.T) {
	for _, pathKind := range []string{"file", "source-directory"} {
		t.Run(pathKind, func(t *testing.T) {
			paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
			if err := paths.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			const version = "8.3.30"
			sourceDir := paths.SourcePath(version)
			outsideDir := t.TempDir()
			outside := filepath.Join(outsideDir, "php.ini-production")
			if err := os.WriteFile(outside, []byte("secret=outside\n"), 0600); err != nil {
				t.Fatal(err)
			}
			link := sourceDir
			target := outsideDir
			if pathKind == "file" {
				if err := os.Mkdir(sourceDir, 0755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(sourceDir, "php.ini-production")
				target = outside
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			bin := filepath.Join(paths.VersionBin(version), core.PHPBinary())
			if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, []byte("fixture"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := NewBuilder(paths, nil).postInstall(context.Background(), version, paths.VersionDir(version)); err == nil {
				t.Error("accepted symlink-controlled php.ini source")
			}
			if _, err := os.Stat(paths.VersionPhpIni(version)); !os.IsNotExist(err) {
				t.Errorf("published contents read outside source root: %v", err)
			}
			got, err := os.ReadFile(outside)
			if err != nil || string(got) != "secret=outside\n" {
				t.Errorf("outside source changed: %q, %v", got, err)
			}
		})
	}
}

func TestPostInstallCopiesRegularProductionIni(t *testing.T) {
	paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	const version = "8.3.30"
	sourceDir := paths.SourcePath(version)
	if err := os.Mkdir(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	const production = "; production configuration\nmemory_limit=256M\n"
	if err := os.WriteFile(filepath.Join(sourceDir, "php.ini-production"), []byte(production), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(paths.VersionBin(version), core.PHPBinary())
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewBuilder(paths, nil).postInstall(context.Background(), version, paths.VersionDir(version)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(paths.VersionPhpIni(version))
	if err != nil || string(got) != production {
		t.Errorf("installed php.ini = %q, %v", got, err)
	}
}
