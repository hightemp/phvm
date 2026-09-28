package deps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestDependenciesRejectUnsafeVersion(t *testing.T) {
	dir := t.TempDir()
	p := core.NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	version := "../../outside"
	m := NewDepsManager(p, nil, 1)
	for _, name := range []string{"openssl", "curl"} {
		prefix := filepath.Join(dir, "outside", name)
		if err := os.MkdirAll(filepath.Join(prefix, "include"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(prefix, ".phvm-installed"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnsureDeps(context.Background(), version); err == nil {
		t.Error("EnsureDeps accepted unsafe version")
	}
	if flags := m.GetConfigureFlags(version); len(flags) != 0 {
		t.Errorf("unsafe dependency flags: %v", flags)
	}
	if env := m.GetBuildEnv(version); len(env) != 0 {
		t.Errorf("unsafe dependency environment: %v", env)
	}
}
