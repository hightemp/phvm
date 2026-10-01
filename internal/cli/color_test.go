package cli

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/ext"
	"github.com/hightemp/phvm/internal/ui"
)

var ansiColors = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestDoctorColorModeAddsAccentsWithoutChangingReport(t *testing.T) {
	withoutConfigEnv(t)
	t.Setenv("NO_COLOR", "")
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	p := core.NewPaths(t.TempDir())
	run := func(mode string) string {
		t.Helper()
		cmd := exec.Command(bin, "--phvm-dir", p.Root, "--color="+mode, "doctor", "--php", "8.3.30", "--profile", "minimal")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("doctor --color=%s: %v %s", mode, err, &stderr)
		}
		return stdout.String()
	}
	plain := run("never")
	styled := run("always")
	if strings.Contains(plain, "\x1b[") || !strings.Contains(styled, "\x1b[") {
		t.Errorf("color modes ignored: plain=%q styled=%q", plain, styled)
	}
	if ansiColors.ReplaceAllString(styled, "") != plain {
		t.Error("color changed the report's data or wording")
	}
	for _, part := range []string{"System Requirements Check", "8.3.30", "✓", "All required build checks passed!"} {
		if !strings.Contains(styled, part) {
			t.Errorf("styled report omits %q", part)
		}
	}
}

func TestHumanListsGetColorsButMachineResultsStayPlain(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	if err := core.NewAliasManager(p).SetDefault("7.4.33"); err != nil {
		t.Fatal(err)
	}
	run := func(mode string, args ...string) string {
		t.Helper()
		command := append([]string{"--phvm-dir", p.Root, "--color=" + mode}, args...)
		cmd := exec.Command(bin, command...)
		var out, diagnostics bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostics
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v %s", args, err, &diagnostics)
		}
		return out.String()
	}
	plain, styled := run("never", "ls"), run("always", "ls")
	if !strings.Contains(styled, "\x1b[") || ansiColors.ReplaceAllString(styled, "") != plain {
		t.Errorf("version list accents changed data: %q", styled)
	}
	for _, args := range [][]string{{"current"}, {"which", "php"}, {"version"}, {"init", "bash"}, {"__complete", "use", "8.3"}} {
		if out := run("always", args...); strings.Contains(out, "\x1b[") {
			t.Errorf("machine result %v contains styling: %q", args, out)
		}
	}
	var config core.Config
	if err := toml.Unmarshal([]byte(run("always", "config", "show", "--effective")), &config); err != nil {
		t.Fatalf("colored mode broke TOML output: %v", err)
	}
}

func TestColorErrorsAndLegacyOptOutPreserveExitAndRedaction(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	for _, tc := range []struct {
		flags   []string
		colored bool
		message string
	}{
		{[]string{"--color=always"}, true, "missing"},
		{[]string{"--no-color"}, false, "missing"},
		{[]string{"--color=rainbow"}, false, "--color must be auto, always or never"},
		{[]string{"--color=always", "--no-color"}, false, "use either --color or --no-color"},
	} {
		cmd := exec.Command(bin, append(append([]string{"--phvm-dir", p.Root}, tc.flags...), "which", "missing")...)
		var out, diagnostics bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostics
		err := cmd.Run()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || out.Len() != 0 || !strings.Contains(diagnostics.String(), tc.message) {
			t.Errorf("bad error contract for %v: error=%v stdout=%q stderr=%q", tc.flags, err, out.String(), diagnostics.String())
		}
		if strings.Contains(diagnostics.String(), "\x1b[") != tc.colored {
			t.Errorf("wrong stderr color for %v: %q", tc.flags, diagnostics.String())
		}
	}
}

func TestExtensionListStylesReflectActualLifecycleStates(t *testing.T) {
	entries := []ext.Extension{
		{Name: "redis", State: "enabled", Version: "6.0.0"},
		{Name: "xdebug", State: "disabled"},
		{Name: "Core", State: "builtin"},
		{Name: "pecl_http", Module: "http", State: "broken", Problem: "module unavailable"},
	}
	plain := formatExtensionList(entries, "8.3.30", ui.Plain())
	styled := formatExtensionList(entries, "8.3.30", ui.New(&bytes.Buffer{}, ui.Always, true))
	if ansiColors.ReplaceAllString(styled, "") != plain {
		t.Error("extension colors changed names, versions or states")
	}
	for _, state := range []string{"\x1b[32menabled", "\x1b[33mdisabled", "\x1b[36mbuiltin", "\x1b[1;31mbroken"} {
		if !strings.Contains(styled, state) {
			t.Errorf("extension state lacks correct accent: %q", state)
		}
	}
}

func TestInvalidColorModeIsRejectedByMachineCommands(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, args := range [][]string{{"version"}, {"config", "show"}} {
		cmd := exec.Command(bin, append([]string{"--phvm-dir", t.TempDir(), "--color=rainbow"}, args...)...)
		var out, diagnostics bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostics
		if err := cmd.Run(); err == nil || out.Len() != 0 || !strings.Contains(diagnostics.String(), "--color must be auto, always or never") {
			t.Errorf("invalid color mode was ignored by %v: %v %q %q", args, err, out.String(), diagnostics.String())
		}
	}
}
