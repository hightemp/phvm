package ini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func profileFixture(t *testing.T) *core.Paths {
	t.Helper()
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.VersionBin("8.3.30"), p.VersionConfD("8.3.30")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeProfileFile(t, filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), "#!/bin/sh\nprintf 'phvm-ini-ok'\n")
	if err := os.Chmod(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), 0755); err != nil {
		t.Fatal(err)
	}
	writeProfileFile(t, p.VersionPhpIni("8.3.30"), "memory_limit=128M\n")
	return p
}

func writeProfileFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func configurationBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestProfileSwitchResolvesEnabledDisabledCounterparts(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini"), "extension=redis.so\n")
	for _, state := range []string{"disabled", "enabled", "disabled"} {
		profile := p.ProfileDir(state)
		name := "20-redis.ini"
		if state == "disabled" {
			name += ".disabled"
		}
		writeProfileFile(t, filepath.Join(profile, "php.ini"), "; "+state+"\n")
		writeProfileFile(t, filepath.Join(profile, "conf.d", name), "extension=redis.so\n")
		if err := m.Apply(state, "8.3.30", false); err != nil {
			t.Fatal(err)
		}
		other := "20-redis.ini.disabled"
		if state == "disabled" {
			other = "20-redis.ini"
		}
		if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), other)); !os.IsNotExist(err) {
			t.Errorf("%s left opposite loading file %s", state, other)
		}
	}
}

func TestFailedProfileApplyPreservesWholeConfigurationAndOldBackup(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "20-original.ini"), "; original\n")
	writeProfileFile(t, filepath.Join(p.VersionDir("8.3.30"), "etc.backup", "php.ini"), "old backup must survive\n")
	profile := p.ProfileDir("bad")
	writeProfileFile(t, filepath.Join(profile, "php.ini"), "new configuration\n")
	if err := os.MkdirAll(filepath.Join(profile, "conf.d"), 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external.ini")
	writeProfileFile(t, outside, "outside\n")
	if err := os.Symlink(outside, filepath.Join(profile, "conf.d", "20-bad.ini")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	before := configurationBytes(t, p.VersionEtc("8.3.30"))
	if err := m.Apply("bad", "8.3.30", true); err == nil {
		t.Error("invalid profile accepted")
	}
	if after := configurationBytes(t, p.VersionEtc("8.3.30")); !reflect.DeepEqual(before, after) {
		t.Errorf("failed apply left mixed config: %v", after)
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionDir("8.3.30"), "etc.backup", "php.ini")); err != nil || string(data) != "old backup must survive\n" {
		t.Error("failed apply destroyed previous backup")
	}
}

func TestProfileRejectsAmbiguousEnabledAndDisabledCopies(t *testing.T) {
	p := profileFixture(t)
	profile := p.ProfileDir("ambiguous")
	writeProfileFile(t, filepath.Join(profile, "php.ini"), "new\n")
	for _, name := range []string{"20-redis.ini", "20-redis.ini.disabled"} {
		writeProfileFile(t, filepath.Join(profile, "conf.d", name), "extension=redis.so\n")
	}
	before := configurationBytes(t, p.VersionEtc("8.3.30"))
	if err := NewProfileManager(p).Apply("ambiguous", "8.3.30", false); err == nil {
		t.Error("ambiguous profile accepted")
	}
	if after := configurationBytes(t, p.VersionEtc("8.3.30")); !reflect.DeepEqual(before, after) {
		t.Error("ambiguous profile modified configuration")
	}
}

func TestRepeatedProfileApplicationKeepsUniqueBackups(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	writeProfileFile(t, filepath.Join(p.ProfileDir("prod"), "php.ini"), "memory_limit=256M\n")
	for n := 0; n < 3; n++ {
		writeProfileFile(t, p.VersionPhpIni("8.3.30"), fmt.Sprintf("memory_limit=%dM\n", n+10))
		if err := m.Apply("prod", "8.3.30", true); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := filepath.Glob(filepath.Join(p.VersionDir("8.3.30"), "etc.backup-*"))
	if err != nil || len(backups) != 3 {
		t.Errorf("backup history overwritten: %v %v", backups, err)
	}
}

func TestSavedProfileIsExactSnapshotAndResaveRemovesStaleFiles(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini.disabled"), "extension=redis.so\n")
	if err := m.Save("saved", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini.disabled")); err != nil {
		t.Fatal(err)
	}
	if err := m.Save("saved", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.ProfileDir("saved"), "conf.d", "20-redis.ini.disabled")); !os.IsNotExist(err) {
		t.Error("resave retained removed file")
	}
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "90-extra.ini"), "; extra\n")
	writeProfileFile(t, filepath.Join(p.VersionEtc("8.3.30"), "php-fpm.conf"), "unrelated fpm config\n")
	if err := m.Apply("saved", "8.3.30", false); err != nil {
		t.Fatal(err)
	}
	if files, err := os.ReadDir(p.VersionConfD("8.3.30")); err != nil || len(files) != 0 {
		t.Errorf("snapshot retained absent files: %v %v", files, err)
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionEtc("8.3.30"), "php-fpm.conf")); err != nil || string(data) != "unrelated fpm config\n" {
		t.Error("snapshot removed unrelated etc configuration")
	}
}

func TestBuiltinProfileOnlyChangesPHPIni(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	if err := m.CreateDefaultProfiles(); err != nil {
		t.Fatal(err)
	}
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini"), "extension=redis.so\n")
	if err := m.Apply("production", "8.3.30", false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini")); err != nil || string(data) != "extension=redis.so\n" {
		t.Error("builtin profile altered extension loading files")
	}
}

func TestProfileMetadataUsesExactModuleInsteadOfIniFilename(t *testing.T) {
	p := profileFixture(t)
	meta := core.NewMetadata("8.3.30")
	meta.Extensions["redis"] = core.ExtMetadata{Module: "redis", IniFile: "20-redis.ini", Enabled: true}
	if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	writeProfileFile(t, filepath.Join(p.ProfileDir("other"), "php.ini"), "; candidate\n")
	writeProfileFile(t, filepath.Join(p.ProfileDir("other"), "conf.d", "20-redis.ini"), "extension=rediscluster.so\n")
	if err := NewProfileManager(p).Apply("other", "8.3.30", false); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Extensions["redis"].Enabled {
		t.Error("ini filename incorrectly marked redis enabled while loading rediscluster")
	}
}

func TestLateProfileValidationFailureRestoresConfiguration(t *testing.T) {
	p := profileFixture(t)
	writeProfileFile(t, filepath.Join(p.ProfileDir("candidate"), "php.ini"), "memory_limit=256M\n")
	php := filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary())
	script := "#!/bin/sh\ncase \"$PHPRC\" in\n*.ini-profile-*) printf phvm-ini-ok;;\n*) echo 'late startup failure' >&2; exit 1;;\nesac\n"
	if err := os.WriteFile(php, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	before := configurationBytes(t, p.VersionEtc("8.3.30"))
	if err := NewProfileManager(p).Apply("candidate", "8.3.30", true); err == nil {
		t.Error("late runtime failure reported success")
	}
	if after := configurationBytes(t, p.VersionEtc("8.3.30")); !reflect.DeepEqual(before, after) {
		t.Error("late failure did not restore previous configuration")
	}
	if backups, _ := filepath.Glob(filepath.Join(p.VersionDir("8.3.30"), "etc.backup-*")); len(backups) != 0 {
		t.Error("failed apply published a backup")
	}
}

func TestProfileBackupLimitAndCancellation(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	if err := m.SetBackupLimit(2); err != nil {
		t.Fatal(err)
	}
	writeProfileFile(t, filepath.Join(p.ProfileDir("prod"), "php.ini"), "; production\n")
	writeProfileFile(t, filepath.Join(p.VersionDir("8.3.30"), "etc.backup-unmanaged", "php.ini"), "leave unowned history\n")
	for n := 0; n < 4; n++ {
		if err := m.Apply("prod", "8.3.30", true); err != nil {
			t.Fatal(err)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(p.VersionDir("8.3.30"), "etc.backup-*"))
	if len(backups) != 3 {
		t.Errorf("managed backup limit/unowned preservation failed: %v", backups)
	}
	before := configurationBytes(t, p.VersionEtc("8.3.30"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.ApplyContext(ctx, "prod", "8.3.30", true); err == nil {
		t.Error("cancelled apply accepted")
	}
	if after := configurationBytes(t, p.VersionEtc("8.3.30")); !reflect.DeepEqual(before, after) {
		t.Error("cancelled apply changed configuration")
	}
}

func TestFailedProfileSavePreservesPreviousSnapshot(t *testing.T) {
	p := profileFixture(t)
	m := NewProfileManager(p)
	if err := m.Save("saved", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	before := configurationBytes(t, p.ProfileDir("saved"))
	writeProfileFile(t, p.VersionPhpIni("8.3.30"), "new\n")
	outside := filepath.Join(t.TempDir(), "external.ini")
	writeProfileFile(t, outside, "outside\n")
	if err := os.Symlink(outside, filepath.Join(p.VersionConfD("8.3.30"), "20-bad.ini")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := m.Save("saved", "8.3.30"); err == nil {
		t.Error("invalid save accepted")
	}
	if after := configurationBytes(t, p.ProfileDir("saved")); !reflect.DeepEqual(before, after) {
		t.Error("failed save modified previous profile")
	}
}

func TestSavedProfilePreservesConfigurationPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes")
	}
	p := profileFixture(t)
	m := NewProfileManager(p)
	if err := os.Chmod(p.VersionPhpIni("8.3.30"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.VersionConfD("8.3.30"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.VersionEtc("8.3.30"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := m.Save("saved", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.VersionPhpIni("8.3.30"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.VersionConfD("8.3.30"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply("saved", "8.3.30", false); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{p.VersionPhpIni("8.3.30"): 0640, p.VersionConfD("8.3.30"): 0700, p.VersionEtc("8.3.30"): 0750} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("permissions for %s=%v want %o", path, info, want)
		}
	}
	if err := m.CreateDefaultProfiles(); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply("development", "8.3.30", false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(p.VersionConfD("8.3.30")); err != nil || info.Mode().Perm() != 0700 {
		t.Error("ini-only application changed conf.d permissions")
	}
}
