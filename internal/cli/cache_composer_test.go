package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
)

func seedLegacyComposer(t *testing.T, names ...string) *core.Paths {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher fixture")
	}
	name := "phvm with spaces"
	if len(names) > 0 {
		name = names[0]
	}
	p := core.NewPaths(filepath.Join(t.TempDir(), name))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(p.Downloads, "composer.phar")
	if err := os.WriteFile(old, []byte("Composer version 2.9.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"8.3.30", "8.2.30"} {
		if err := os.MkdirAll(p.VersionBin(version), 0755); err != nil {
			t.Fatal(err)
		}
		php := filepath.Join(p.VersionBin(version), core.PHPBinary())
		if err := os.WriteFile(php, []byte("#!/bin/sh\ncat \"$1\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
		wrapper := fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", php, old)
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), "composer"), []byte(wrapper), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestRelativePHVMRootUnderSymlinkedParentSharesStateLock(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t)
	linkedParent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(filepath.Dir(p.Root), linkedParent); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	linkedRoot := filepath.Join(linkedParent, filepath.Base(p.Root))
	legacyPHAR := filepath.Join(linkedRoot, "cache", "downloads", "composer.phar")
	for _, version := range []string{"8.3.30", "8.2.30"} {
		php := filepath.Join(linkedRoot, "versions", "php", version, "bin", core.PHPBinary())
		wrapper := fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", php, legacyPHAR)
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), "composer"), []byte(wrapper), 0755); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--phvm-dir", filepath.Base(p.Root), "cache", "clear", "--downloads")
	cmd.Dir = linkedParent
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "PWD=") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "PWD="+linkedParent)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("relative root migration timed out or failed: %v, context=%v\n%s", err, ctx.Err(), out)
	}
	launcher := filepath.Join(p.VersionBin("8.3.30"), "composer")
	cmd = exec.Command(launcher, "--version")
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "Composer version 2.9.0\n" {
		t.Errorf("migrated Composer launcher failed from another directory: %v %s", err, out)
	}
}

func TestMigratedComposerTreatsPathsLiterally(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t, "phvm's $literal `echo changed`")
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear", "--downloads").CombinedOutput()
	if err != nil {
		t.Fatalf("migrate special path: %v %s", err, out)
	}
	out, err = exec.Command(filepath.Join(p.VersionBin("8.3.30"), "composer"), "--version").CombinedOutput()
	if err != nil || string(out) != "Composer version 2.9.0\n" {
		t.Errorf("launcher expanded configured paths instead of using them literally: %v %s", err, out)
	}
}

func TestRelativePHVMRootMigratesToAbsoluteLaunchers(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--phvm-dir", filepath.Base(p.Root), "cache", "clear", "--downloads")
	cmd.Dir = filepath.Dir(p.Root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("relative root migration: %v, context=%v %s", err, ctx.Err(), out)
	}
	cmd = exec.Command(filepath.Join(p.VersionBin("8.3.30"), "composer"), "--version")
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "Composer version 2.9.0\n" {
		t.Errorf("Composer depended on caller working directory: %v %s", err, out)
	}
}

func TestCacheClearPreservesAndMigratesComposer(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{nil, {"--downloads"}, {"--all"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p := seedLegacyComposer(t)
			marker := filepath.Join(p.Downloads, "disposable.tar.gz")
			if err := os.WriteFile(marker, []byte("cached download"), 0600); err != nil {
				t.Fatal(err)
			}
			command := append([]string{"--phvm-dir", p.Root, "cache", "clear"}, args...)
			out, err := exec.Command(bin, command...).CombinedOutput()
			if err != nil {
				t.Fatalf("cache clear: %v %s", err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("disposable cache was not removed")
			}
			for _, version := range []string{"8.3.30", "8.2.30"} {
				permanent := filepath.Join(p.Composer, version, "composer.phar")
				if data, err := os.ReadFile(permanent); err != nil || string(data) != "Composer version 2.9.0\n" {
					t.Errorf("Composer for PHP %s not preserved outside cache: %s %v", version, data, err)
				}
				wrapper := filepath.Join(p.VersionBin(version), "composer")
				out, err := exec.Command(wrapper, "--version").CombinedOutput()
				if err != nil || string(out) != "Composer version 2.9.0\n" {
					t.Errorf("Composer %s broken after cache clear: %v %s", version, err, out)
				}
				data, err := os.ReadFile(wrapper)
				if err != nil || strings.Contains(string(data), p.Downloads) || !strings.Contains(string(data), permanent) {
					t.Errorf("legacy launcher not migrated: %s %v", data, err)
				}
			}
			// Migration and cleanup must be repeatable after legacy bytes are gone.
			if out, err := exec.Command(bin, command...).CombinedOutput(); err != nil {
				t.Fatalf("repeat clear: %v %s", err, out)
			}
		})
	}
}

func TestCacheClearStopsBeforeDeletingUnmigratedComposer(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t)
	launcher := filepath.Join(p.VersionBin("8.2.30"), "composer")
	before, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	custom := append([]byte("# user-customized legacy launcher\n"), before...)
	if err := os.WriteFile(launcher, custom, 0755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear", "--downloads").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "composer") {
		t.Errorf("unmigrated legacy launcher was ignored: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(p.Downloads, "composer.phar")); err != nil {
		t.Error("legacy working PHAR deleted after migration failure")
	}
	after, err := os.ReadFile(launcher)
	if err != nil || string(after) != string(custom) {
		t.Error("custom launcher overwritten during migration")
	}
	// Explicit enable authorizes replacing this launcher, so the diagnostic is actionable.
	out, err = exec.Command(bin, "--phvm-dir", p.Root, "composer", "enable", "--php", "8.2.30").CombinedOutput()
	if err != nil {
		t.Fatalf("repair legacy launcher: %v %s", err, out)
	}
	out, err = exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear", "--downloads").CombinedOutput()
	if err != nil {
		t.Fatalf("clear after repair: %v %s", err, out)
	}
}

func TestCacheClearKeepsAuthoritativePermanentComposer(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t)
	permanentDir := filepath.Join(p.Root, "tools", "composer")
	if err := os.MkdirAll(permanentDir, 0755); err != nil {
		t.Fatal(err)
	}
	permanent := filepath.Join(permanentDir, "composer.phar")
	if err := os.WriteFile(permanent, []byte("Composer version 2.10.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear").CombinedOutput()
	if err != nil {
		t.Fatalf("clear: %v %s", err, out)
	}
	out, err = exec.Command(filepath.Join(p.VersionBin("8.3.30"), "composer"), "--version").CombinedOutput()
	if err != nil || string(out) != "Composer version 2.10.0\n" {
		t.Errorf("migration overwrote permanent Composer with legacy bytes: %v %s", err, out)
	}
}

func TestCacheClearRejectsUnsafeComposerStorage(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, kind := range []string{"tools symlink", "PHAR symlink"} {
		t.Run(kind, func(t *testing.T) {
			p := seedLegacyComposer(t)
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "marker"), []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			tools := filepath.Join(p.Root, "tools")
			if kind == "tools symlink" {
				if err := os.RemoveAll(tools); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, tools); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(tools, "composer"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "marker"), filepath.Join(tools, "composer", "composer.phar")); err != nil {
					t.Fatal(err)
				}
			}
			out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear", "--downloads").CombinedOutput()
			if err == nil {
				t.Errorf("unsafe permanent storage accepted: %s", out)
			}
			if _, err := os.Stat(filepath.Join(p.Downloads, "composer.phar")); err != nil {
				t.Error("working legacy Composer removed on storage failure")
			}
			data, err := os.ReadFile(filepath.Join(outside, "marker"))
			if err != nil || string(data) != "outside" {
				t.Error("outside file changed")
			}
		})
	}
}
