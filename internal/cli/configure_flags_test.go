package cli

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/hightemp/phvm/internal/core"
)

func TestConfigureCLIHandlesQuotedAndRepeatedArguments(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tt := range []struct {
		name       string
		args, want []string
	}{
		{"quoted legacy", []string{"--configure", `--with-sdk="/path with space" --without-curl`}, []string{"--with-sdk=/path with space", "--without-curl"}},
		{"repeated exact", []string{"--configure-flag=--with-sdk=/path with space", "--configure-flag=--with-options=a,b", "--configure-flag=--without-curl"}, []string{"--with-sdk=/path with space", "--with-options=a,b", "--without-curl"}},
		{"repeat precedence", []string{"--configure", "--with-curl --with-sdk=/old", "--configure-flag=--without-curl", "--configure-flag=--with-sdk=/new"}, []string{"--without-curl", "--with-sdk=/new"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			args := append([]string{"--phvm-dir", p.Root, "config", "show", "--effective"}, tt.args...)
			out, err := exec.Command(bin, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("configure flags: %v %s", err, out)
			}
			var cfg core.Config
			if err := toml.Unmarshal(out, &cfg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Build.DefaultFlags, tt.want) {
				t.Errorf("actual argv=%q want=%q", cfg.Build.DefaultFlags, tt.want)
			}
		})
	}
}

func TestInvalidConfigureInputStopsBeforeNetwork(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{{"--configure", `--with-sdk="unfinished`}, {"--configure", "--with-sdk=/ok dangling"}, {"--configure-flag="}, {"--profile", "commmon"}} {
		p := core.NewPaths(t.TempDir())
		argv := append([]string{"--phvm-dir", p.Root, "install", "8.3.30", "--mirror", "http://127.0.0.1:1", "--retries", "0"}, args...)
		out, err := exec.Command(bin, argv...).CombinedOutput()
		if err == nil || strings.Contains(string(out), "Resolving version") {
			t.Errorf("invalid configure input %q reached installation: %v %s", args, err, out)
		}
	}
}
