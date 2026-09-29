package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestCLIUninstallChecksOwnedBinaryWithCurrentPHP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX CLI fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name, module, content string
		fail                  bool
	}{
		{"owned binary", "redis", "original", false},
		{"changed binary", "redis", "changed", true},
		{"builtin", "core", "original", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{p.VersionBin("8.3.30"), p.VersionConfD("8.3.30"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions")} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\nprintf '[PHP Modules]\\nCore\\n'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := core.NewCurrentManager(p).Set("8.3.30"); err != nil {
				t.Fatal(err)
			}
			binary := "lib/extensions/" + tt.module + ".so"
			file := filepath.Join(p.VersionDir("8.3.30"), binary)
			if err := os.WriteFile(file, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			ini := filepath.Join(p.VersionConfD("8.3.30"), "20-fixture.ini")
			if err := os.WriteFile(ini, []byte("extension="+tt.module+".so\n"), 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("original"))
			meta := core.NewMetadata("8.3.30")
			meta.Extensions["fixture"] = core.ExtMetadata{Module: tt.module, Binary: binary, BinarySHA256: hex.EncodeToString(hash[:]), IniFile: "20-fixture.ini", Enabled: true}
			if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(p.VersionMetadata("8.3.30"))
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "--phvm-dir", p.Root, "ext", "uninstall", tt.module).CombinedOutput()
			if (err != nil) != tt.fail {
				t.Fatalf("CLI exit=%v fail=%v: %s", err, tt.fail, out)
			}
			if tt.fail {
				if data, err := os.ReadFile(file); err != nil || string(data) != tt.content {
					t.Error("refused uninstall changed binary")
				}
				if data, err := os.ReadFile(p.VersionMetadata("8.3.30")); err != nil || string(data) != string(before) {
					t.Error("refused uninstall changed metadata")
				}
				if _, err := os.Stat(ini); err != nil {
					t.Error("refused uninstall changed ini")
				}
			} else {
				for _, path := range []string{file, ini} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("owned file survived: %s", path)
					}
				}
			}
		})
	}
}

func TestCLIMissingExtensionDoesNotTouchSimilarlyNamedModule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX CLI fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, operation := range []string{"enable", "disable", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{p.VersionBin("8.3.30"), p.VersionConfD("8.3.30")} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			name := "20-rediscluster.ini"
			if operation == "enable" {
				name += ".disabled"
			}
			file := filepath.Join(p.VersionConfD("8.3.30"), name)
			if err := os.WriteFile(file, []byte("extension=rediscluster.so\n"), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "--phvm-dir", p.Root, "ext", operation, "redis", "--php", "8.3").CombinedOutput()
			if err == nil || !strings.Contains(string(out), "redis") {
				t.Errorf("missing extension reported CLI success: %v %s", err, out)
			}
			if _, err := os.Stat(file); err != nil {
				t.Error("rediscluster was modified by redis operation")
			}
		})
	}
}

func TestCLIExtensionListShowsDisabledMissingAndBroken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX CLI fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions")
	for _, d := range []string{dir, p.VersionBin("8.3.30"), p.VersionConfD("8.3.30")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\ncase \"$1\" in\n-r) echo '" + dir + "';;\n*) printf '[PHP Modules]\\nCore\\n[Zend Modules]\\n';;\nesac\n"
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"redis", "broken"} {
		if err := os.WriteFile(filepath.Join(dir, name+".so"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"20-redis.ini.disabled": "extension=redis.so\n", "20-broken.ini": "extension=broken.so\n", "20-missing.ini": "extension=missing.so\n"} {
		if err := os.WriteFile(filepath.Join(p.VersionConfD("8.3.30"), name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "ext", "list", "--php", "8.3").CombinedOutput()
	if err != nil {
		t.Fatalf("list: %v %s", err, out)
	}
	for _, line := range []string{"[builtin] core", "[disabled] redis", "[missing] missing", "[broken] broken"} {
		if !strings.Contains(string(out), line) {
			t.Errorf("list omitted state %s: %s", line, out)
		}
	}
}
