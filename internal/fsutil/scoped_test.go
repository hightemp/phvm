package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenScopedDirRejectsFileAncestorAsCorruption(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "versions")
	if err := os.WriteFile(file, []byte("broken directory"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenScopedDir(base, filepath.Join(file, "php"), false)
	if root != nil {
		_ = root.Close()
	}
	if err == nil || os.IsNotExist(err) {
		t.Fatalf("file ancestor looked like absent directory: %v", err)
	}
}
