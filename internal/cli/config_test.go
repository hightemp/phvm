package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/hightemp/phvm/internal/core"
)

func withoutConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PHVM_PROFILE", "PHVM_JOBS", "PHVM_COLOR", "PHVM_MIRROR", "PHVM_USER_AGENT", "PHVM_TIMEOUT", "PHVM_RETRIES", "PHVM_GPG", "PHVM_GPG_FALLBACK_SHA256", "PHVM_CONFIGURE_FLAGS"} {
		t.Setenv(key, "")
	}
}

func TestCLIRejectsMalformedConfigBeforeCommand(t *testing.T) {
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile(), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "--phvm-dir", p.Root, "ls").CombinedOutput(); err == nil || !strings.Contains(string(out), "config") {
		t.Errorf("malformed config was ignored: %v %s", err, out)
	}
}

func TestConfigShowPrecedence(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile(), []byte("[general]\ndefault_profile='minimal'\nparallel_jobs=3\ncolor=false\n[verify]\ngpg=false\n[build]\ndefault_flags=['--with-curl','--with-zlib']"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHVM_PROFILE", "full")
	t.Setenv("PHVM_JOBS", "7")
	t.Setenv("PHVM_COLOR", "false")
	t.Setenv("PHVM_GPG", "false")
	t.Setenv("PHVM_CONFIGURE_FLAGS", `["--with-curl"]`)
	t.Setenv("PHVM_MIRROR", "https://env.example.invalid")
	t.Setenv("PHVM_USER_AGENT", "env-agent")
	t.Setenv("PHVM_TIMEOUT", "42")
	t.Setenv("PHVM_RETRIES", "5")
	t.Setenv("PHVM_GPG_FALLBACK_SHA256", "false")
	cmd := exec.Command(bin, "--phvm-dir", p.Root, "config", "show", "--effective", "--profile", "common", "--jobs", "0", "--no-color=false", "--gpg=true", "--configure", "--without-curl", "--mirror", "https://cli.example.invalid", "--user-agent", "cli-agent", "--timeout", "3", "--retries", "0", "--gpg-fallback-sha256=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show effective: %v %s", err, out)
	}
	var cfg core.Config
	if err := toml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("output not TOML: %v\n%s", err, out)
	}
	if cfg.General.DefaultProfile != "common" || cfg.General.ParallelJobs != 0 || !cfg.General.Color || !cfg.Verify.GPG || len(cfg.Build.DefaultFlags) != 1 || cfg.Build.DefaultFlags[0] != "--without-curl" {
		t.Errorf("CLI precedence incorrect: %+v", cfg)
	}
	if cfg.Remote.Mirror != "https://cli.example.invalid" || cfg.Remote.UserAgent != "cli-agent" || cfg.Remote.Timeout != 3 || cfg.Remote.Retries != 0 || !cfg.Verify.GPGFallbackSHA256 {
		t.Errorf("remote/verification CLI precedence incorrect: %+v", cfg)
	}
	cmd = exec.Command(bin, "--phvm-dir", p.Root, "config", "show", "--effective")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show env: %v %s", err, out)
	}
	if err := toml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.General.DefaultProfile != "full" || cfg.General.ParallelJobs != 7 || cfg.Verify.GPG {
		t.Errorf("env precedence incorrect: %+v", cfg)
	}
	if cfg.Remote.Mirror != "https://env.example.invalid" || cfg.Remote.UserAgent != "env-agent" || cfg.Remote.Timeout != 42 || cfg.Remote.Retries != 5 || cfg.Verify.GPGFallbackSHA256 {
		t.Errorf("remote/verification env precedence incorrect: %+v", cfg)
	}
	cmd = exec.Command(bin, "--phvm-dir", p.Root, "config", "show")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("show file: %v %s", err, out)
	}
	if err := toml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.General.DefaultProfile != "minimal" || cfg.General.ParallelJobs != 3 || len(cfg.Build.DefaultFlags) != 2 {
		t.Errorf("raw file settings lost: %+v", cfg)
	}
	if cfg.Remote != core.DefaultConfig().Remote || !cfg.Verify.GPGFallbackSHA256 {
		t.Errorf("defaults or raw settings lost: %+v", cfg)
	}
}

func TestConfigEnvironmentValidation(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, tt := range []struct{ key, value string }{
		{"PHVM_JOBS", "many"}, {"PHVM_JOBS", "-1"}, {"PHVM_PROFILE", "unknown"}, {"PHVM_GPG", "maybe"}, {"PHVM_CONFIGURE_FLAGS", "not JSON"}, {"PHVM_TIMEOUT", "0"},
	} {
		t.Run(tt.key+tt.value, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			p := core.NewPaths(t.TempDir())
			if out, err := exec.Command(bin, "--phvm-dir", p.Root, "config", "validate").CombinedOutput(); err == nil {
				t.Errorf("invalid env accepted: %s", out)
			}
		})
	}
}

func TestInstallRejectsConflictingVerificationFlags(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{
		{"--gpg=true", "--skip-gpg"},
		{"--gpg=false", "--skip-verify"},
		{"--skip-gpg", "--skip-verify=false"},
	} {
		p := core.NewPaths(t.TempDir())
		// No network or installation work may start before flag validation.
		cmdArgs := append([]string{"--phvm-dir", p.Root, "install", "8.5.11", "--mirror", "http://127.0.0.1:1", "--retries", "0"}, args...)
		out, err := exec.Command(bin, cmdArgs...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "use either") || strings.Contains(string(out), "Resolving version") {
			t.Errorf("conflicting flags were not rejected before install: %v %s", err, out)
		}
	}
}

func TestConfigShowRedactsMirrorCredentials(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	cfg := core.DefaultConfig()
	cfg.Remote.Mirror = "https://config-user:config-password@example.invalid"
	if err := cfg.Save(p.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "config", "show", "--effective").CombinedOutput()
	if err != nil {
		t.Fatalf("show: %v %s", err, out)
	}
	if strings.Contains(string(out), "config-user") || strings.Contains(string(out), "config-password") || !strings.Contains(string(out), "example.invalid") {
		t.Errorf("unsafe configuration output: %s", out)
	}
}
