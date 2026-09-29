package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
)

func TestDependencyDefaultsCannotOverrideExplicitFeatureSelection(t *testing.T) {
	p := core.NewPaths(t.TempDir())
	for _, name := range []string{"openssl", "curl"} {
		dir := filepath.Join(p.Root, "deps", "7.4.33", name)
		if err := os.MkdirAll(filepath.Join(dir, "include"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".phvm-installed"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name, profile string
		custom        []string
		wantSSL       string
		wantCurl      bool
	}{
		{"explicit disables", "common", []string{"--without-openssl", "--without-curl"}, "--without-openssl", false},
		{"explicit prefix", "common", []string{"--with-openssl=/custom path", "--without-curl"}, "--with-openssl=/custom path", false},
		{"minimal", "minimal", nil, "", false},
		{"profile with private defaults", "common", nil, "--with-openssl=" + filepath.Join(p.Root, "deps", "7.4.33", "openssl"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBuilder(p, nil)
			b.SetProfile(tt.profile)
			b.SetCustomFlags(tt.custom)
			flags := b.ConfigureFlags("7.4.33")
			var ssl string
			curl := false
			for _, f := range flags {
				if extractOption(f) == "openssl" {
					ssl = f
				}
				if extractOption(f) == "curl" {
					curl = f != "--without-curl"
				}
			}
			if ssl != tt.wantSSL || curl != tt.wantCurl {
				t.Errorf("flags=%v; SSL=%q curl=%v", flags, ssl, curl)
			}
		})
	}
}

func TestConfigureMergingRemovesEveryConflictingDuplicate(t *testing.T) {
	flags := MergeFlags(&Profile{Flags: []string{"--with-openssl=one", "--with-openssl=two", "--with-curl"}}, []string{"--without-openssl", "--without-openssl"})
	if want := []string{"--without-openssl", "--with-curl"}; !reflect.DeepEqual(flags, want) {
		t.Errorf("duplicates/conflicting options remain: %v", flags)
	}
}

func TestUnknownProfileDoesNotFallBackToCommon(t *testing.T) {
	if profile := GetProfile("commmon"); profile != nil {
		t.Errorf("unknown profile selected %s", profile.Name)
	}
}

func TestVersionAwareProfileAndExplicitFlags(t *testing.T) {
	b := NewBuilder(core.NewPaths(t.TempDir()), nil)
	b.SetProfile("full")
	old, err := b.ResolvedConfigureFlags("5.4.45")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range old {
		if f == "--enable-gd" || f == "--with-zip" || extractOption(f) == "opcache" || extractOption(f) == "sodium" {
			t.Errorf("unavailable old profile flag=%s", f)
		}
	}
	for _, tt := range []struct{ version, flag string }{{"7.3.33", "--enable-gd"}, {"8.3.30", "--with-gd"}, {"8.3.30", "--enable-json"}, {"5.4.45", "--enable-opcache"}} {
		b.SetCustomFlags([]string{tt.flag})
		if _, err := b.ResolvedConfigureFlags(tt.version); err == nil {
			t.Errorf("unsupported %s for %s", tt.flag, tt.version)
		}
	}
}

func TestConfigurationCLIAndEnvironmentPrecedence(t *testing.T) {
	p := core.NewPaths(t.TempDir())
	dir := filepath.Join(p.Root, "deps", "7.4.33", "openssl", "include")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(p, nil)
	b.SetProfile("common")
	b.SetConfigFlags([]string{"--without-curl", "--with-openssl=/configured", "CFLAGS=-O2"})
	b.SetCustomFlags([]string{"--without-openssl", "CFLAGS=-O0 -g"})
	flags, err := b.ResolvedConfigureFlags("7.4.33")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(flags, "\n"); strings.Contains(joined, "--with-openssl") || strings.Contains(joined, "--with-curl") {
		t.Errorf("higher-priority disable lost: %s", joined)
	}
	env := b.Environment("7.4.33")
	if env.Value("CFLAGS", "") != "-O0 -g" {
		t.Error("explicit configure environment assignment lost")
	}
	if strings.Contains(env.Value("CPPFLAGS", ""), dir) {
		t.Error("disabled dependencies still affected compiler environment")
	}
}

func TestMetadataMatchesExecutedConfigureArgvAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure capture")
	}
	p := core.NewPaths(t.TempDir())
	source, buildDir := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--with-sdk'; exit 0; fi\nprintf '%s\\0' \"$@\" > captured-argv\nprintf '%s' \"$CFLAGS\" > captured-env\n"
	if err := os.WriteFile(filepath.Join(source, "configure"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(p, nil)
	b.SetProfile("minimal")
	b.SetConfigFlags([]string{"--with-sdk=/configured", "CFLAGS=-O2"})
	b.SetCustomFlags([]string{"--with-sdk=/path with space,a,b", "CFLAGS=-O0 -g"})
	if err := b.configure(context.Background(), "8.3.30", source, buildDir, p.VersionDir("8.3.30")); err != nil {
		t.Fatal(err)
	}
	meta, err := b.candidateMetadata(BuildOptions{Version: "8.3.30"}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(buildDir, "captured-argv"))
	if err != nil {
		t.Fatal(err)
	}
	actual := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if !reflect.DeepEqual(meta.ConfigureFlags, actual) {
		t.Errorf("recorded argv=%q actual=%q", meta.ConfigureFlags, actual)
	}
	env, err := os.ReadFile(filepath.Join(buildDir, "captured-env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(env) != "-O0 -g" || meta.BuildEnvironment["CFLAGS"] != string(env) {
		t.Errorf("recorded environment=%v actual=%s", meta.BuildEnvironment, env)
	}
}
