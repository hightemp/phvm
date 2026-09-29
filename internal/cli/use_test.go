package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func buildTestCLI(t *testing.T) string {
	t.Helper()
	name := "phvm-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/phvm")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return bin
}

func seedInstalledVersions(t *testing.T) *core.Paths {
	t.Helper()
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"7.4.33", "8.2.30", "8.3.9", "8.3.30", "8.4.5"} {
		if err := os.MkdirAll(p.VersionBin(version), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.VersionBin(version), core.PHPBinary()), []byte("fixture"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// The newest directory is incomplete and must not be selected.
	if err := os.Mkdir(p.VersionDir("8.9.99"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := core.NewCurrentManager(p).Set("8.2.30"); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	return p
}

func TestUseResolvesInstalledVersions(t *testing.T) {
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name, input, want string
		aliases           map[string]string
		wantErr           bool
	}{
		{name: "full", input: "8.3.9", want: "8.3.9"},
		{name: "minor newest numeric patch", input: "8.3", want: "8.3.30"},
		{name: "major", input: "8", want: "8.4.5"},
		{name: "v prefix", input: "v8.3.30", want: "8.3.30"},
		{name: "php prefix", input: "php-8.3.30", want: "8.3.30"},
		{name: "partial v prefix", input: "v8.3", want: "8.3.30"},
		{name: "partial php prefix", input: "php-8", want: "8.4.5"},
		{name: "whitespace prefix", input: " v8.3 ", want: "8.3.30"},
		{name: "latest", input: "latest", want: "8.4.5"},
		{name: "stable", input: "stable", want: "8.4.5"},
		{name: "special case insensitive", input: "LATEST", want: "8.4.5"},
		{name: "alias full", input: "prod", want: "8.3.9", aliases: map[string]string{"prod": "v8.3.9"}},
		{name: "alias chain minor", input: "prod", want: "8.3.30", aliases: map[string]string{"prod": "branch", "branch": "8.3"}},
		{name: "alias chain major", input: "prod", want: "8.4.5", aliases: map[string]string{"prod": "default", "default": "8"}},
		{name: "alias to latest", input: "prod", want: "8.4.5", aliases: map[string]string{"prod": "latest"}},
		{name: "local stable override", input: "stable", want: "8.2.30", aliases: map[string]string{"stable": "8.2"}},
		{name: "explicit lts alias", input: "lts", want: "7.4.33", aliases: map[string]string{"lts": "7.4"}},
		{name: "undefined lts", input: "lts", wantErr: true},
		{name: "missing branch", input: "9.1", wantErr: true},
		{name: "missing exact never falls back", input: "8.3.31", wantErr: true},
		{name: "unknown alias", input: "missing", wantErr: true},
		{name: "alias missing target", input: "prod", aliases: map[string]string{"prod": "missing"}, wantErr: true},
		{name: "alias cycle", input: "prod", aliases: map[string]string{"prod": "branch", "branch": "prod"}, wantErr: true},
		{name: "numeric alias cycle", input: "8.3", aliases: map[string]string{"8.3": "loop", "loop": "8.3"}, wantErr: true},
		{name: "traversal", input: "../../outside", wantErr: true},
		{name: "invalid URL secrets", input: "https://phvm-user:phvm-password@example.invalid/?token=phvm-token", wantErr: true},
		{name: "overflow", input: "999999999999999999999999999.3", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := seedInstalledVersions(t)
			aliases := core.NewAliasManager(p)
			for name, target := range tt.aliases {
				if err := aliases.Set(name, target); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(bin, "--phvm-dir", p.Root, "use", tt.input)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Errorf("use error=%v wantErr=%v\n%s", err, tt.wantErr, out)
			}
			if strings.Contains(tt.name, "cycle") && !strings.Contains(string(out), "alias cycle") {
				t.Errorf("cycle cause was hidden: %s", out)
			}
			if tt.name == "invalid URL secrets" {
				for _, secret := range []string{"phvm-user", "phvm-password", "phvm-token"} {
					if strings.Contains(string(out), secret) {
						t.Errorf("invalid input exposed %s: %s", secret, out)
					}
				}
			}
			want := tt.want
			if tt.wantErr {
				want = "8.2.30"
			}
			current, readErr := core.NewCurrentManager(p).Get()
			if readErr != nil || current != want {
				t.Errorf("current=%s want=%s error=%v\n%s", current, want, readErr, out)
			}
			if !tt.wantErr && !strings.Contains(string(out), "Now using PHP "+want) {
				t.Errorf("output lacks canonical version: %s", out)
			}
		})
	}
}

func TestPHPFlagsUseInstalledResolver(t *testing.T) {
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name      string
		args      []string
		extension bool
	}{
		{"composer minor", []string{"composer", "disable", "--php", "8.3"}, false},
		{"composer alias", []string{"composer", "disable", "--php", "prod"}, false},
		{"extension alias", []string{"ext", "disable", "redis", "--php", "prod"}, true},
		{"extension partial prefix", []string{"ext", "disable", "redis", "--php", "v8.3"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := seedInstalledVersions(t)
			if err := core.NewAliasManager(p).Set("prod", "8.3"); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(p.VersionBin("8.3.30"), "composer")
			if tt.extension {
				marker = filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini")
				if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
					t.Fatal(err)
				}
			}
			content := "fixture"
			if tt.extension {
				content = "extension=redis.so\n"
			}
			if err := os.WriteFile(marker, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--phvm-dir", p.Root}, tt.args...)
			if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
				t.Errorf("command failed: %v\n%s", err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Errorf("selected version was not changed: %v", err)
			}
			if tt.extension {
				if _, err := os.Stat(marker + ".disabled"); err != nil {
					t.Errorf("selected extension was not disabled: %v", err)
				}
			}
			if current, err := core.NewCurrentManager(p).Get(); err != nil || current != "8.2.30" {
				t.Errorf("--php changed current: %s %v", current, err)
			}
		})
	}
}
