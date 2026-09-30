package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitZshAndFishRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, sh := range []string{"zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			program, err := exec.LookPath(sh)
			if err != nil {
				t.Skipf("%s unavailable", sh)
			}
			for _, explicit := range []bool{false, true} {
				name := "existing environment"
				if explicit {
					name = "explicit root"
				}
				t.Run(name, func(t *testing.T) {
					root := filepath.Join(t.TempDir(), `spaces ' " $name $(touch marker) back\slash`)
					previous := filepath.Join(t.TempDir(), "previous")
					args := []string{"init", sh}
					want := previous
					if explicit {
						args = append([]string{"--phvm-dir", root}, args...)
						want = root
					}
					generate := exec.Command(bin, args...)
					generate.Env = initEnvironment(previous)
					script, err := generate.CombinedOutput()
					if err != nil {
						t.Fatalf("generate %s: %v\n%s", sh, err, script)
					}
					scriptFile := filepath.Join(t.TempDir(), "init."+sh)
					if err := os.WriteFile(scriptFile, script, 0600); err != nil {
						t.Fatal(err)
					}
					cwd := t.TempDir()
					var check *exec.Cmd
					if sh == "zsh" {
						check = exec.Command(program, "-f", "-c", `if [[ -n $PHVM_TEST_ZSH_MODULE_DIR ]]; then module_path=("$PHVM_TEST_ZSH_MODULE_DIR" $module_path); fi; source "$1"; source "$1"; printf '%s\n%s\n' "$PHVM_DIR" "$PATH"`, "zsh", scriptFile)
					} else {
						check = exec.Command(program, "-c", `source "$argv[1]"; set -l before (complete -c phvm | count); source "$argv[1]"; printf '%s\n' "$PHVM_DIR"; string join : $PATH; printf '%s:%s\n' $before (complete -c phvm | count)`, scriptFile)
					}
					check.Env = initEnvironment(previous)
					check.Dir = cwd
					out, err := check.CombinedOutput()
					if err != nil {
						t.Fatalf("source twice: %v\n%s", err, out)
					}
					lines := strings.Split(strings.TrimSpace(string(out)), "\n")
					if len(lines) < 2 || lines[0] != want {
						t.Fatalf("PHVM_DIR: %q, want %q", out, want)
					}
					if sh == "fish" {
						counts := []string(nil)
						if len(lines) == 3 {
							counts = strings.Split(lines[2], ":")
						}
						if len(counts) != 2 || counts[0] != counts[1] {
							t.Errorf("Fish completion registrations changed after repeat init: %q", out)
						}
					}
					for _, dir := range []string{filepath.Join(want, "current", "bin"), filepath.Join(want, "bin")} {
						found := 0
						for _, entry := range strings.Split(lines[1], ":") {
							if entry == dir {
								found++
							}
						}
						if found != 1 {
							t.Errorf("PATH count for %q = %d: %q", dir, found, lines[1])
						}
					}
					if _, err := os.Stat(filepath.Join(cwd, "marker")); !os.IsNotExist(err) {
						t.Fatalf("root executed shell syntax: %v", err)
					}
				})
			}
		})
	}
}

func TestInitPowerShellRuntime(t *testing.T) {
	program, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell unavailable")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, explicit := range []bool{false, true} {
		name := "existing environment"
		if explicit {
			name = "explicit root"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "spaces ' ‘ ’ $name $(touch marker) back`tick")
			previous := filepath.Join(t.TempDir(), "previous")
			args := []string{"init", "powershell"}
			want := previous
			if explicit {
				args = append([]string{"--phvm-dir", root}, args...)
				want = root
			}
			env := make([]string, 0, len(os.Environ())+1)
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "PHVM_DIR=") {
					env = append(env, item)
				}
			}
			env = append(env, "PHVM_DIR="+previous)
			generate := exec.Command(bin, args...)
			generate.Env = env
			script, err := generate.CombinedOutput()
			if err != nil {
				t.Fatalf("generate PowerShell: %v\n%s", err, script)
			}
			scriptFile := filepath.Join(t.TempDir(), "init.ps1")
			if err := os.WriteFile(scriptFile, script, 0600); err != nil {
				t.Fatal(err)
			}
			quoted := "'" + strings.ReplaceAll(scriptFile, "'", "''") + "'"
			command := fmt.Sprintf(". %s; . %s; [Console]::WriteLine($env:PHVM_DIR); [Console]::WriteLine($env:PATH)", quoted, quoted)
			check := exec.Command(program, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
			check.Dir = t.TempDir()
			check.Env = env
			out, err := check.CombinedOutput()
			if err != nil {
				t.Fatalf("source twice: %v\n%s", err, out)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != 2 || strings.TrimSpace(lines[0]) != want {
				t.Fatalf("PHVM_DIR: %q, want %q", out, want)
			}
			for _, dir := range []string{filepath.Join(want, "current", "bin"), filepath.Join(want, "bin")} {
				found := 0
				for _, entry := range strings.Split(strings.TrimSpace(lines[1]), string(os.PathListSeparator)) {
					if entry == dir {
						found++
					}
				}
				if found != 1 {
					t.Errorf("PATH count for %q = %d: %q", dir, found, lines[1])
				}
			}
			if _, err := os.Stat(filepath.Join(check.Dir, "marker")); !os.IsNotExist(err) {
				t.Fatalf("root executed shell syntax: %v", err)
			}
		})
	}
}
