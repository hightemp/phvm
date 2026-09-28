package deps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/toolchain"
)

func TestBuildEnvPreservesCPPFLAGSSeparately(t *testing.T) {
	p := core.NewPaths(t.TempDir())
	include := filepath.Join(p.Root, "deps", "7.4.33", "openssl", "include")
	if err := os.MkdirAll(include, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CFLAGS", "-DC_FLAGS")
	t.Setenv("CPPFLAGS", "-DCPP_FLAGS")
	env := toolchain.Current(NewDepsManager(p, nil, 1).GetBuildEnv("7.4.33")...)
	if !strings.Contains(env.Value("CPPFLAGS", ""), "-DCPP_FLAGS") || strings.Contains(env.Value("CPPFLAGS", ""), "-DC_FLAGS") {
		t.Errorf("CPPFLAGS overwritten with CFLAGS: %s", env.Value("CPPFLAGS", ""))
	}
}

func TestDependencyBuildPreservesEnvironmentFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure fixture")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	dir := t.TempDir()
	depsDir := filepath.Join(dir, "deps")
	for _, part := range []string{"include", "lib"} {
		if err := os.MkdirAll(filepath.Join(depsDir, "openssl", part), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CFLAGS", "-DCUSTOM_C")
	t.Setenv("CPPFLAGS", "-DCUSTOM_CPP")
	t.Setenv("LDFLAGS", "-Wl,--as-needed")
	script := "#!/bin/sh\nprintf '%s\\n' \"$CFLAGS\" \"$CPPFLAGS\" \"$LDFLAGS\" > captured-env\n"
	if err := os.WriteFile(filepath.Join(dir, "configure"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	makefile := "all install:\n\t@printf '%s\\n' \"$$CFLAGS\" \"$$CPPFLAGS\" \"$$LDFLAGS\" > captured-env\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewDepsManager(core.NewPaths(filepath.Join(dir, "phvm")), nil, 1)
	dep := Dependency{Name: "curl", ConfigureCmd: []string{"./configure"}, DependsOn: []string{"openssl"}}
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"configure", func() error { return manager.configure(context.Background(), dep, dir, dir, depsDir) }},
		{"make", func() error { return manager.makeDep(context.Background(), dep, dir, depsDir) }},
		{"install", func() error { return manager.install(context.Background(), dep, dir, depsDir) }},
	} {
		t.Run(step.name, func(t *testing.T) {
			if err := step.run(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "captured-env"))
			if err != nil {
				t.Fatal(err)
			}
			for _, flag := range []string{"-DCUSTOM_C", "-DCUSTOM_CPP", "-Wl,--as-needed"} {
				if !strings.Contains(string(data), flag) {
					t.Errorf("%s lost %s: %s", step.name, flag, data)
				}
			}
			if !strings.Contains(string(data), "-I"+filepath.Join(depsDir, "openssl", "include")) {
				t.Errorf("%s lost private dependency path: %s", step.name, data)
			}
		})
	}
}
