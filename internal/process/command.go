// Package process runs cancellable subprocesses with bounded tree cleanup.
package process

import (
	"context"
	"os/exec"
	"time"
)

// CommandContext creates a subprocess whose cancellation stops its process tree.
// Unix uses a dedicated process group; Windows uses the OS taskkill tree facility.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	configureCancellation(cmd)
	return cmd
}

// InteractiveCommandContext keeps Unix editors in the terminal's foreground group.
// Cancellation kills the editor itself on Unix and uses taskkill on Windows.
func InteractiveCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	configureInteractiveCancellation(cmd)
	return cmd
}
