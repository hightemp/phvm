package core

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAliasOperationsRejectUnsafeNames(t *testing.T) {
	for _, name := range []string{"../config/marker", `..\config\marker`, "/tmp/marker", `C:\marker`, ".", "..", ""} {
		t.Run(name, func(t *testing.T) {
			p := NewPaths(filepath.Join(t.TempDir(), "phvm"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(p.Config, "marker")
			if err := os.WriteFile(marker, []byte("8.3.30"), 0600); err != nil {
				t.Fatal(err)
			}
			m := NewAliasManager(p)
			if _, err := m.Get(name); err == nil {
				t.Error("Get accepted unsafe name")
			}
			if err := m.Delete(name); err == nil {
				t.Error("Delete accepted unsafe name")
			}
			if err := m.Set(name, "8.3.30"); err == nil {
				t.Error("Set accepted unsafe name")
			}
			if m.Exists(name) {
				t.Error("Exists accepted unsafe name")
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "8.3.30" {
				t.Fatalf("marker changed: %q, %v", data, err)
			}
		})
	}
}

func TestVersionOperationsRejectTraversal(t *testing.T) {
	for _, version := range []string{"../../../victim", `..\..\..\victim`, "8.3", "", "../../.."} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			p := NewPaths(filepath.Join(dir, "phvm"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(dir, "victim", "bin")
			if err := os.MkdirAll(victim, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(victim, PHPBinary())
			if err := os.WriteFile(marker, []byte("marker"), 0600); err != nil {
				t.Fatal(err)
			}
			m := NewInstalledManager(p)
			if m.IsInstalled(version) {
				t.Error("IsInstalled accepted unsafe version")
			}
			if err := NewCurrentManager(p).Set(version); err == nil {
				t.Error("Set accepted unsafe version")
			}
			if err := m.Remove(version); err == nil {
				t.Error("Remove accepted unsafe version")
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("outside file removed: %v", err)
			}
		})
	}
}

func TestAliasRejectsEscapingSymlinks(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "alias file", true: "alias directory"}[parent], func(t *testing.T) {
			dir := t.TempDir()
			p := NewPaths(filepath.Join(dir, "phvm"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(dir, "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(outside, "prod")
			if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			link, target := p.AliasFile("prod"), marker
			if parent {
				if err := os.Remove(p.Alias); err != nil {
					t.Fatal(err)
				}
				link, target = p.Alias, outside
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			m := NewAliasManager(p)
			if _, err := m.Get("prod"); err == nil {
				t.Error("read escaped alias root")
			}
			if parent {
				if err := m.Set("prod", "8.3.30"); err == nil {
					t.Error("write escaped alias root")
				}
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "outside" {
				t.Fatalf("outside alias changed: %q %v", data, err)
			}
		})
	}
}

func TestCurrentRejectsForeignTarget(t *testing.T) {
	dir := t.TempDir()
	p := NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside", "8.3.30")
	if err := os.MkdirAll(filepath.Join(outside, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, p.Current); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m := NewCurrentManager(p)
	if _, err := m.Get(); err == nil {
		t.Error("Get accepted foreign installation")
	}
	if _, err := m.GetPath(); err == nil {
		t.Error("GetPath accepted foreign installation")
	}
	if m.IsSet() {
		t.Error("foreign installation is active")
	}
}

func TestInstalledListRejectsEscapingParent(t *testing.T) {
	dir := t.TempDir()
	p := NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.Versions); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "8.3.30", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "8.3.30", "bin", PHPBinary()), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, p.Versions); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if versions, err := NewInstalledManager(p).List(); err == nil || len(versions) != 0 {
		t.Errorf("listed outside installations: %v %v", versions, err)
	}
}

func TestAliasWritesResistDirectorySymlinkSwap(t *testing.T) {
	dir := t.TempDir()
	p := NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "prod")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	if err := os.Symlink(outside, probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		parked := filepath.Join(dir, "parked")
		for {
			select {
			case <-stop:
				return
			default:
			}
			if os.Rename(p.Alias, parked) != nil {
				continue
			}
			_ = os.Symlink(outside, p.Alias)
			_ = os.Remove(p.Alias)
			if os.Rename(parked, p.Alias) != nil {
				_ = os.RemoveAll(p.Alias)
				_ = os.Rename(parked, p.Alias)
			}
		}
	}()
	for i := 0; i < 200; i++ {
		_ = NewAliasManager(p).Set("prod", "8.3.30")
	}
	close(stop)
	wg.Wait()
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "outside" {
		t.Errorf("concurrent swap changed outside file: %q %v", data, err)
	}
}

func TestManagedVersionAndAliasLifecycle(t *testing.T) {
	p := NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.VersionBin("8.3.30"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), PHPBinary()), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	aliases := NewAliasManager(p)
	if err := aliases.Set("prod", "v8.3.30"); err != nil {
		t.Fatal(err)
	}
	version, err := aliases.Resolve("prod", 10)
	if err != nil {
		t.Fatal(err)
	}
	current := NewCurrentManager(p)
	if err := current.Set(version); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink permissions: %v", err)
		}
		t.Fatal(err)
	}
	if got, err := current.Get(); err != nil || got != "8.3.30" {
		t.Fatalf("current=%q %v", got, err)
	}
	info, err := NewInstalledManager(p).GetVersionInfo("v8.3.30")
	if err != nil || info.Version != "8.3.30" {
		t.Fatalf("version info: %+v %v", info, err)
	}
	if err := current.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := NewInstalledManager(p).Remove("v8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := aliases.Delete("prod"); err != nil {
		t.Fatal(err)
	}
	if versions, err := NewInstalledManager(p).List(); err != nil || len(versions) != 0 {
		t.Errorf("remaining versions: %v %v", versions, err)
	}
}
