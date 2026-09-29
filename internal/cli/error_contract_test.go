package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func commandStreams(t *testing.T, bin string, p *core.Paths, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--phvm-dir", p.Root, "--no-color"}, args...)...)
	var out, diagnostics bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, out.String(), diagnostics.String()
}

func TestCLIErrorsReturnOneAndUseStderr(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{{"init", "unknown-shell"}, {"which", "missing"}, {"which", "../php"}, {"which", "php", "extra"}, {"ls-remote", "--mirror", "http://127.0.0.1:1", "--retries", "0"}, {"ls-remote", "--all", "--mirror", "http://127.0.0.1:1", "--retries", "0"}} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			p := seedInstalledVersions(t)
			code, out, diagnostics := commandStreams(t, bin, p, args...)
			if code != 1 || out != "" || diagnostics == "" {
				t.Errorf("exit=%d stdout=%q stderr=%q", code, out, diagnostics)
			}
		})
	}
}

func TestCLIQueryDataUsesStdout(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	for _, args := range [][]string{{"ls"}, {"current"}, {"which", "php"}, {"cache", "show"}, {"alias", "list"}, {"version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, diagnostics := commandStreams(t, bin, p, args...)
			if code != 0 || out == "" || diagnostics != "" {
				t.Errorf("query streams: exit=%d stdout=%q stderr=%q", code, out, diagnostics)
			}
		})
	}
}

func TestCLIEditorFailureIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX editor fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 7\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	code, out, diagnostics := commandStreams(t, bin, p, "ini", "open")
	if code != 1 || out != "" || !strings.Contains(diagnostics, "editor") {
		t.Errorf("editor failure exit=%d stdout=%q stderr=%q", code, out, diagnostics)
	}
}

func TestCLIEditorSharesTerminalProcessGroupAndAcceptsArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal process groups")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	editor := filepath.Join(t.TempDir(), "editor with spaces")
	script := "#!/bin/sh\n[ \"$(ps -o pgid= -p $$)\" = \"$(ps -o pgid= -p $PPID)\" ] || exit 9\n[ \"$1\" = --fixture ] || exit 8\n[ \"$2\" = \"$PHVM_TEST_INI\" ] || exit 7\n"
	if err := os.WriteFile(editor, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "'"+editor+"' --fixture")
	t.Setenv("PHVM_TEST_INI", p.VersionPhpIni("8.2.30"))
	code, out, diagnostics := commandStreams(t, bin, p, "ini", "open")
	if code != 0 || out != "" || diagnostics != "" {
		t.Errorf("interactive editor exit=%d stdout=%q stderr=%q", code, out, diagnostics)
	}
}

func TestCLIBrokenCurrentAndNonExecutableWhichFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	if err := os.Chmod(filepath.Join(p.VersionBin("8.2.30"), core.PHPBinary()), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"current"}, {"which", "php"}} {
		code, out, diagnostics := commandStreams(t, bin, p, args...)
		if code != 1 || out != "" || diagnostics == "" {
			t.Errorf("non-executable current: exit=%d out=%q err=%q", code, out, diagnostics)
		}
	}
}

func TestCLIOutputWriteFailureReturnsError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /dev/full output failure fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	for _, args := range [][]string{{"ls"}, {"current"}, {"cache", "show"}, {"version"}, {"init", "bash"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			cmd := exec.Command(bin, append([]string{"--phvm-dir", p.Root, "--no-color"}, args...)...)
			var diagnostics bytes.Buffer
			cmd.Stdout, cmd.Stderr = out, &diagnostics
			err = cmd.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || diagnostics.Len() == 0 {
				t.Errorf("failed output exit=%v stderr=%s", err, diagnostics.String())
			}
		})
	}
}

func TestCLIRejectsUnexpectedArguments(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	for _, args := range [][]string{{"ls"}, {"current"}, {"cache", "show"}, {"cache", "clear"}, {"alias", "list"}, {"ini", "path"}, {"ini", "list"}, {"ini", "profile", "list"}, {"composer", "disable"}, {"version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, diagnostics := commandStreams(t, bin, p, append(args, "unexpected")...)
			if code != 1 || out != "" || diagnostics == "" {
				t.Errorf("unexpected arguments exit=%d stdout=%q stderr=%q", code, out, diagnostics)
			}
		})
	}
}

func TestCLIBrokenStorageDoesNotReportSuccess(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name, subdir string
		args         []string
	}{
		{"versions", "versions/php", []string{"ls"}},
		{"cache", "cache", []string{"cache", "clear"}},
		{"aliases", "alias", []string{"alias", "list"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := seedInstalledVersions(t)
			path := filepath.Join(p.Root, tt.subdir)
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("broken storage"), 0600); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostics := commandStreams(t, bin, p, tt.args...)
			if code != 1 || out != "" || diagnostics == "" || strings.Contains(diagnostics, "Cache cleared") {
				t.Errorf("broken storage exit=%d stdout=%q stderr=%q", code, out, diagnostics)
			}
		})
	}
}

func TestCLIStateInspectionReportsCorruption(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, state := range []string{"current", "default", "alias list"} {
		t.Run(state, func(t *testing.T) {
			p := seedInstalledVersions(t)
			args := []string{"ls"}
			if state == "current" {
				if err := os.Remove(filepath.Join(p.VersionBin("8.2.30"), core.PHPBinary())); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(p.AliasFile("default"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if state == "alias list" {
					args = []string{"alias", "list"}
				}
			}
			code, out, diagnostics := commandStreams(t, bin, p, args...)
			if code != 1 || out != "" || diagnostics == "" {
				t.Errorf("corrupt state exit=%d stdout=%q stderr=%q", code, out, diagnostics)
			}
		})
	}
}
