package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestCLIIniProfilesSnapshotAndBackups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.VersionBin("8.3.30"), p.VersionConfD("8.3.30")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\nprintf phvm-ini-ok\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.VersionPhpIni("8.3.30"), []byte("memory_limit=128M\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := core.NewCurrentManager(p).Set("8.3.30"); err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(p.VersionConfD("8.3.30"), name), []byte("extension=redis.so\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("20-redis.ini.disabled")
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command(bin, append([]string{"--phvm-dir", p.Root, "ini", "profile"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("profile CLI: %v %s", err, out)
		}
	}
	run("save", "saved")
	if err := os.Remove(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini.disabled")); err != nil {
		t.Fatal(err)
	}
	write("20-redis.ini")
	write("99-extra.ini")
	for n := 0; n < 4; n++ {
		run("use", "saved", "--backup-keep", "2")
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini.disabled")); err != nil {
		t.Error("saved disabled configuration missing")
	}
	for _, name := range []string{"20-redis.ini", "99-extra.ini"} {
		if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), name)); !os.IsNotExist(err) {
			t.Errorf("snapshot left %s", name)
		}
	}
	if backups, _ := filepath.Glob(filepath.Join(p.VersionDir("8.3.30"), "etc.backup-*")); len(backups) != 2 {
		t.Errorf("CLI backup history=%v", backups)
	}
	run("use", "development", "--backup=false")
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini.disabled")); err != nil {
		t.Error("development default changed extension config")
	}
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "ini", "profile", "use", "saved", "--backup-keep", "0").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "backup-keep") {
		t.Errorf("invalid retention accepted: %v %s", err, out)
	}
	bad := p.ProfileDir("bad")
	if err := os.MkdirAll(bad, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "php.ini"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\necho runtime-failure >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p.VersionPhpIni("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(bin, "--phvm-dir", p.Root, "ini", "profile", "use", "bad").CombinedOutput()
	if err == nil || strings.Contains(string(out), "Applied profile") {
		t.Errorf("profile runtime failure returned success: %v %s", err, out)
	}
	if data, err := os.ReadFile(p.VersionPhpIni("8.3.30")); err != nil || string(data) != string(before) {
		t.Error("CLI failure changed current php.ini")
	}
}
