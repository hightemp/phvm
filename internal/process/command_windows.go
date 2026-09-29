//go:build windows

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func configureCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		tool := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
		if os.Getenv("SystemRoot") == "" {
			return errors.Join(errors.New("SystemRoot is missing; process tree termination unavailable"), cmd.Process.Kill())
		}
		err := exec.CommandContext(ctx, tool, "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		if err == nil {
			return nil
		}
		return errors.Join(err, cmd.Process.Kill())
	}
}

func configureInteractiveCancellation(cmd *exec.Cmd) { configureCancellation(cmd) }
