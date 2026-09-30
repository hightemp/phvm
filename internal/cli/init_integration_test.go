package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/shell"
)

func initEnvironment(root string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "PHVM_DIR=") && !strings.HasPrefix(item, "PATH=") && !strings.HasPrefix(item, "XDG_DATA_HOME=") {
			env = append(env, item)
		}
	}
	return append(env, "PHVM_DIR="+root, "PATH=/usr/bin:/bin")
}

func TestInitQuotesRootsForEveryShell(t *testing.T) {
	root := `a'b\c $dollar $(touch marker) "quoted" ‘curly’`
	for _, tc := range []struct {
		shell shell.Shell
		want  string
	}{
		{shell.Bash, `a'\''b\c $dollar $(touch marker) "quoted" ‘curly’`},
		{shell.Zsh, `a'\''b\c $dollar $(touch marker) "quoted" ‘curly’`},
		{shell.Fish, `a\'b\\c $dollar $(touch marker) "quoted" ‘curly’`},
		{shell.PowerShell, `a''b\c $dollar $(touch marker) "quoted" ‘‘curly’’`},
	} {
		t.Run(string(tc.shell), func(t *testing.T) {
			script := shell.GetInitScript(tc.shell, root)
			if !strings.Contains(script, "'"+tc.want+"'") {
				t.Errorf("root is not a literal shell string: %q", script)
			}
		})
	}
}

func TestInitCompletionUsesInstalledStateWithoutHardcodedVersions(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	if err := core.NewAliasManager(p).Set("prod", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := core.NewAliasManager(p).Set("broken", "missing"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
		omit string
	}{
		{[]string{"use", "8.3"}, "8.3.30", "8.9.99"},
		{[]string{"use", "pr"}, "prod", ""},
		{[]string{"use", "bro"}, "", "broken"},
		{[]string{"uninstall", "8.3"}, "8.3.30", "8.9.99"},
		{[]string{"install", "la"}, "latest", "8.1"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			command := exec.Command(bin, append([]string{"__complete"}, tc.args...)...)
			command.Env = initEnvironment(p.Root)
			out, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("complete: %v\n%s", err, out)
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			found := false
			for _, line := range lines {
				if line == tc.want {
					found = true
				}
				if line == tc.omit && tc.omit != "" {
					t.Errorf("incomplete version suggested: %q", out)
				}
			}
			if tc.want != "" && !found {
				t.Errorf("%q not suggested: %q", tc.want, out)
			}
		})
	}
}

func TestInitBashUsesEffectiveRootAndIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash runtime")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tc := range []struct {
		name, root, previous, want, generateDir string
		explicit, partialPath                   bool
	}{
		{name: "existing environment", root: filepath.Join(t.TempDir(), "unused"), previous: filepath.Join(t.TempDir(), "selected"), want: "previous"},
		{name: "partially initialized PATH", previous: filepath.Join(t.TempDir(), "selected"), want: "previous", partialPath: true},
		{name: "explicit flag overrides environment", root: filepath.Join(t.TempDir(), `spaces ' " $backtick\ $(touch marker)`), previous: filepath.Join(t.TempDir(), "old"), want: "root", explicit: true},
		{name: "relative explicit root", root: "relative root", previous: filepath.Join(t.TempDir(), "old"), want: "root", generateDir: t.TempDir(), explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"init", "bash"}
			if tc.explicit {
				args = append([]string{"--phvm-dir", tc.root}, args...)
			}
			generate := exec.Command(bin, args...)
			generate.Env = initEnvironment(tc.previous)
			if tc.generateDir != "" {
				generate.Dir = tc.generateDir
			}
			script, err := generate.CombinedOutput()
			if err != nil {
				t.Fatalf("generate init: %v\n%s", err, script)
			}
			scriptFile := filepath.Join(t.TempDir(), "init.bash")
			if err := os.WriteFile(scriptFile, script, 0600); err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			check := exec.Command("bash", "--noprofile", "--norc", "-c", `source "$1"; source "$1"; printf '%s\n%s\n' "$PHVM_DIR" "$PATH"`, "bash", scriptFile)
			check.Dir = cwd
			check.Env = initEnvironment(tc.previous)
			if tc.partialPath {
				check.Env[len(check.Env)-1] = "PATH=" + filepath.Join(tc.previous, "current", "bin") + ":/usr/bin:/bin"
			}
			output, err := check.CombinedOutput()
			if err != nil {
				t.Fatalf("source twice: %v\n%s", err, output)
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) != 2 {
				t.Fatalf("unexpected shell output: %q", output)
			}
			want := tc.previous
			if tc.want == "root" {
				want = tc.root
				if tc.generateDir != "" {
					parent, err := filepath.EvalSymlinks(tc.generateDir)
					if err != nil {
						t.Fatal(err)
					}
					want = filepath.Join(parent, tc.root)
				}
			}
			if lines[0] != want {
				t.Errorf("PHVM_DIR=%q want %q", lines[0], want)
			}
			for _, dir := range []string{filepath.Join(want, "current", "bin"), filepath.Join(want, "bin")} {
				count := 0
				for _, part := range strings.Split(lines[1], ":") {
					if part == dir {
						count++
					}
				}
				if count != 1 {
					t.Errorf("PATH contains %q %d times: %q", dir, count, lines[1])
				}
			}
			if _, err := os.Stat(filepath.Join(cwd, "marker")); !os.IsNotExist(err) {
				t.Fatalf("root ran as shell code: %v", err)
			}
		})
	}
}

func TestInitUsesCobraCompletionAndQuotesEveryShell(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	rootName := `root ' " $dollar $(touch marker) back\slash`
	if runtime.GOOS == "windows" {
		rootName = "root ' $dollar $(touch marker) back`tick"
	}
	root := filepath.Join(t.TempDir(), rootName)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			init := exec.Command(bin, "--phvm-dir", root, "init", shell)
			generated, err := init.CombinedOutput()
			if err != nil {
				t.Fatalf("init: %v\n%s", err, generated)
			}
			completion := exec.Command(bin, "completion", shell)
			want, err := completion.CombinedOutput()
			if err != nil {
				t.Fatalf("Cobra completion: %v\n%s", err, want)
			}
			if !strings.HasSuffix(string(generated), string(want)) {
				t.Error("init completion differs from Cobra's current command tree")
			}
			if !strings.Contains(string(generated), "PHVM_DIR") {
				t.Error("script omits managed root")
			}
		})
	}
}
