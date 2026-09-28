package ext

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestExtensionOperationsRejectUnsafeVersion(t *testing.T) {
	dir := t.TempDir()
	p := core.NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside", "etc", "conf.d")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "20-redis.ini")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Disable(p, "../../../outside", "redis"); err == nil {
		t.Error("Disable accepted unsafe version")
	}
	if err := Enable(p, "../../../outside", "redis"); err == nil {
		t.Error("Enable accepted unsafe version")
	}
	if err := NewInstaller(p, nil).Uninstall(context.Background(), "redis", "../../../outside"); err == nil {
		t.Error("Uninstall accepted unsafe version")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "outside" {
		t.Errorf("outside configuration changed: %q %v", data, err)
	}
}
