package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigureFailureReportsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("configure fixture requires a POSIX shell")
	}
	for _, withLog := range []bool{false, true} {
		name := "without log writer"
		if withLog {
			name = "with log writer"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\nprintf 'early output\\n'\ni=0\nwhile [ $i -lt 1000 ]; do\n" +
				"  printf 'checking for another dependency with some long output...\\n'\n  i=$((i + 1))\ndone\n" +
				"printf 'configure: error: The libcurl check failed.\\n' >&2\nprintf 'linker details\\n' > config.log\nexit 1\n"
			if err := os.WriteFile(filepath.Join(dir, "configure"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			builder := &Builder{profile: CommonProfile()}
			if withLog {
				builder.SetLogWriter(&log)
			}

			err := builder.configure(context.Background(), "8.5.11", dir, dir, filepath.Join(dir, "install"))
			if err == nil {
				t.Fatal("configure should fail")
			}
			for _, want := range []string{"The libcurl check failed", filepath.Join(dir, "config.log")} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Errorf("exit error was not preserved: %v", err)
			}
			if strings.Contains(err.Error(), "early output") {
				t.Errorf("error should contain only the output tail: %v", err)
			}
			if withLog && (!strings.Contains(log.String(), "The libcurl check failed") || !strings.Contains(log.String(), "early output")) {
				t.Errorf("configure output was not written to log: %q", log.String())
			}
		})
	}
}
