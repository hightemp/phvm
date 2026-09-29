//go:build !windows

package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConfigureCancellationStopsDescendantAndClosesPipes(t *testing.T) {
	dir := t.TempDir()
	childFile, beat := filepath.Join(dir, "child-pid"), filepath.Join(dir, "heartbeat")
	script := fmt.Sprintf("#!/bin/sh\nsh -c 'trap \"\" INT TERM; while :; do echo beat >> %s; sleep 0.02; done' &\necho $! > %s\nwait\n", quoteShell(beat), quoteShell(childFile))
	if err := os.WriteFile(filepath.Join(dir, "configure"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	b := &Builder{profile: MinimalProfile()}
	go func() { done <- b.configure(ctx, "8.3.30", dir, dir, filepath.Join(dir, "install")) }()
	var pid int
	deadline := time.After(5 * time.Second)
	for {
		data, err := os.ReadFile(childFile)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("child barrier not reached")
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() {
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
		}
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("cancelled configure returned success")
		}
	case <-time.After(time.Second):
		t.Error("cancelled configure stuck waiting on descendant pipe")
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("configure did not finish after fixture cleanup")
		}
	}
	first, _ := os.ReadFile(beat)
	time.Sleep(100 * time.Millisecond)
	second, _ := os.ReadFile(beat)
	if string(first) != string(second) {
		t.Error("descendant continued modifying files after cancellation")
	}
}
