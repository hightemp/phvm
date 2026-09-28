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
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
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

func TestSaveMetadataRejectsUnsafeVersion(t *testing.T) {
	dir := t.TempDir()
	p := core.NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	result := &remote.VerifyResult{SHA256Verified: true, GPGSkipped: true, GPGSkipReason: "disabled"}
	if err := NewBuilder(p, nil).SaveMetadata("../../../outside", "https://example.invalid", "checksum", result, time.Second); err == nil {
		t.Error("SaveMetadata accepted unsafe version")
	}
	if _, err := os.Stat(filepath.Join(dir, "outside", ".phvm-metadata.json")); !os.IsNotExist(err) {
		t.Errorf("metadata written outside root: %v", err)
	}
}

func TestSaveMetadataUsesActualVerification(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result remote.VerifyResult
	}{
		{"verified", remote.VerifyResult{SHA256Verified: true, GPGVerified: true, GPGFingerprint: "0123456789ABCDEF0123456789ABCDEF01234567"}},
		{"disabled", remote.VerifyResult{SHA256Verified: true, GPGSkipped: true, GPGSkipReason: "disabled"}},
		{"unavailable", remote.VerifyResult{SHA256Verified: true, GPGSkipped: true, GPGSkipReason: "gpg unavailable"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			b := NewBuilder(p, nil)
			if err := b.SaveMetadata("8.3.30", "https://user:secret@example.invalid/php?token=private&version=8.3", "checksum", &tt.result, time.Second); err != nil {
				t.Fatal(err)
			}
			metadata, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
			if err != nil {
				t.Fatal(err)
			}
			if metadata.GPGVerified != tt.result.GPGVerified || metadata.GPGSkipped != tt.result.GPGSkipped || metadata.GPGFingerprint != tt.result.GPGFingerprint || metadata.GPGSkipReason != tt.result.GPGSkipReason || !metadata.SHA256Verified {
				t.Errorf("metadata does not match verification: %+v", metadata)
			}
			if strings.Contains(metadata.SourceURL, "private") || strings.Contains(metadata.SourceURL, "user:secret") {
				t.Errorf("metadata exposes URL secrets: %s", metadata.SourceURL)
			}
		})
	}
}
