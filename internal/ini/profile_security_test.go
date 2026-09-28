package ini

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestProfileOperationsRejectTraversal(t *testing.T) {
	for _, name := range []string{"../../../outside", `..\..\..\outside`, ".", "..", "", "/tmp/profile", `C:\profile`} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := core.NewPaths(filepath.Join(dir, "phvm"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(dir, "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(outside, "php.ini")
			if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(p.VersionEtc("8.3.30"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p.VersionPhpIni("8.3.30"), []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			m := NewProfileManager(p)
			if _, err := m.Get(name); err == nil {
				t.Error("Get accepted unsafe profile")
			}
			if err := m.Save(name, "8.3.30"); err == nil {
				t.Error("Save accepted unsafe profile")
			}
			if err := m.Apply(name, "8.3.30", false); err == nil {
				t.Error("Apply accepted unsafe profile")
			}
			if err := m.Delete(name); err == nil {
				t.Error("Delete accepted unsafe profile")
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "outside" {
				t.Fatalf("outside profile changed: %q %v", data, err)
			}
		})
	}
}

func TestProfileSaveRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	p := core.NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, p.ProfileDir("prod")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.MkdirAll(p.VersionEtc("8.3.30"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.VersionPhpIni("8.3.30"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewProfileManager(p).Save("prod", "8.3.30"); err == nil {
		t.Error("Save accepted escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "php.ini")); !os.IsNotExist(err) {
		t.Errorf("outside profile written: %v", err)
	}
}
