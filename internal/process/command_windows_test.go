//go:build windows

package process

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This uses native Go subprocesses so Windows CI does not need a POSIX shell.
func TestWindowsTreeHelper(t *testing.T) {
	mode := os.Getenv("PHVM_TREE_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		for {
			file, err := os.OpenFile(os.Getenv("PHVM_TREE_BEAT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(2)
			}
			_, _ = file.WriteString("beat\n")
			_ = file.Close()
			time.Sleep(20 * time.Millisecond)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	child := exec.Command(exe, "-test.run=^TestWindowsTreeHelper$")
	child.Env = append(os.Environ(), "PHVM_TREE_HELPER=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("PHVM_TREE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		_ = child.Process.Kill()
		os.Exit(2)
	}
	_ = child.Wait()
	os.Exit(0)
}

func TestWindowsCommandCancellationStopsDescendants(t *testing.T) {
	if os.Getenv("SystemRoot") == "" {
		t.Fatal("SystemRoot required for native Windows tree cancellation")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidFile, beat := filepath.Join(dir, "pid"), filepath.Join(dir, "beat")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := CommandContext(ctx, exe, "-test.run=^TestWindowsTreeHelper$")
	cmd.Env = append(os.Environ(), "PHVM_TREE_HELPER=parent", "PHVM_TREE_PID="+pidFile, "PHVM_TREE_BEAT="+beat)
	var diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &diagnostics, &diagnostics
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var pid int
	deadline := time.After(10 * time.Second)
	for pid == 0 {
		data, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("helper exited before barrier: %v %s", err, &diagnostics)
		case <-deadline:
			cancel()
			<-done
			t.Fatal("child barrier timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() {
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
			_ = child.Release()
		}
	}()
	// Verify the descendant actually started before asking for cancellation.
	for {
		if data, _ := os.ReadFile(beat); len(data) > 0 {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("descendant did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("cancelled command returned success")
		}
	case <-time.After(6 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("tree cleanup timed out")
	}
	first, _ := os.ReadFile(beat)
	time.Sleep(150 * time.Millisecond)
	second, _ := os.ReadFile(beat)
	if !bytes.Equal(first, second) {
		t.Error("Windows descendant survived cancellation")
	}
}
