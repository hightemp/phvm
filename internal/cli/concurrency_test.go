package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

func TestCLIStateCommandsWaitForSamePersistentLock(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{
		{"cache", "clear"}, {"uninstall", "8.2.30"}, {"composer", "disable", "--php", "8.3.30"}, {"ext", "disable", "redis", "--php", "8.3.30"},
	} {
		t.Run(args[0], func(t *testing.T) {
			p := seedLegacyComposer(t)
			if err := os.MkdirAll(p.VersionConfD("8.3.30"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini"), []byte("extension=redis.so\n"), 0600); err != nil {
				t.Fatal(err)
			}
			lock := fsutil.NewFileLock(p.LockFile())
			if ok, err := lock.TryLock(); !ok || err != nil {
				t.Fatalf("lock %v %v", ok, err)
			}
			defer lock.Unlock()
			var out bytes.Buffer
			cmd := exec.Command(bin, append([]string{"--phvm-dir", p.Root}, args...)...)
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var early bool
			select {
			case err := <-done:
				early = true
				t.Errorf("command bypassed shared lock: %v %s", err, out.String())
			case <-time.After(100 * time.Millisecond):
			}
			if err := lock.Unlock(); err != nil {
				t.Fatal(err)
			}
			if !early {
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("after unlock: %v %s", err, out.String())
					}
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					<-done
					t.Fatal("nested lock deadlocked")
				}
			}
			if _, err := os.Stat(p.LockFile()); err != nil {
				t.Error("lock inode was removed")
			}
		})
	}
}

func TestCacheAllIncludesExtensionsAndRefusesEscapingDirectory(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, unsafe := range []bool{false, true} {
		t.Run(map[bool]string{false: "extension archives", true: "symlink"}[unsafe], func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if unsafe {
				if err := os.RemoveAll(p.Extensions); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, p.Extensions); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(p.Extensions, "redis.tgz"), []byte("archive"), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear", "--all").CombinedOutput()
			if unsafe {
				if err == nil {
					t.Errorf("escaping cache directory accepted: %s", out)
				}
				if entries, _ := os.ReadDir(outside); len(entries) != 1 {
					t.Error("outside directory modified")
				}
			} else if err != nil {
				t.Fatalf("clear all: %v %s", err, out)
			} else if files, _ := os.ReadDir(p.Extensions); len(files) != 0 {
				t.Error("extension archive cache survived --all")
			}
		})
	}
}

func TestCLIInterruptedLockWaitPreservesState(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedLegacyComposer(t)
	lock := fsutil.NewFileLock(p.LockFile())
	if ok, err := lock.TryLock(); !ok || err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	var out bytes.Buffer
	cmd := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear")
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("wait ended early: %v %s", err, out.String())
	case <-time.After(150 * time.Millisecond):
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		<-done
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("interrupted waiter returned success")
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("interrupt did not cancel wait")
	}
	if data, err := os.ReadFile(filepath.Join(p.Downloads, "composer.phar")); err != nil || string(data) != "Composer version 2.9.0\n" {
		t.Error("interrupted wait changed cache")
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "--phvm-dir", p.Root, "cache", "clear").CombinedOutput(); err != nil {
		t.Fatalf("lock leaked after cancellation: %v %s", err, out)
	}
}
