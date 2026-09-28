package composer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestComposerRejectsUnsafeVersion(t *testing.T) {
	dir := t.TempDir()
	p := core.NewPaths(filepath.Join(dir, "phvm"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside", "bin")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "composer")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewManager(p).Disable("../../../outside"); err == nil {
		t.Error("Disable accepted unsafe version")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("outside launcher removed: %v", err)
	}
}
