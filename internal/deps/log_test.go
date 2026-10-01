package deps

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestPrivateDependencyBuildStreamsFullOutputWithoutExpandingError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX make fixture")
	}
	tools := t.TempDir()
	makeScript := "#!/bin/sh\ni=0\nwhile [ $i -lt 300 ]; do echo 'ld: undefined reference to private_symbol'; i=$((i+1)); done\nexit 2\n"
	if err := os.WriteFile(filepath.Join(tools, "make"), []byte(makeScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := NewDepsManager(core.NewPaths(t.TempDir()), nil, 1)
	var full bytes.Buffer
	m.SetLogWriter(&full)
	if err := m.makeDep(context.Background(), Dependency{Name: "openssl"}, t.TempDir(), t.TempDir()); err == nil || len(err.Error()) > 150 || strings.Contains(err.Error(), "undefined reference") {
		t.Errorf("dependency build returned unbounded output: %v", err)
	}
	if strings.Count(full.String(), "undefined reference to private_symbol") != 300 {
		t.Errorf("private dependency log lost output: %d lines", strings.Count(full.String(), "undefined reference"))
	}
}
