package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256RejectsSymlinkedArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "php.tar.xz")
	outside := filepath.Join(t.TempDir(), "outside.tar.xz")
	data := []byte("outside data")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, archive); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	hash := sha256.Sum256(data)
	verifier := NewVerifier(nil, dir)
	if err := verifier.VerifySHA256(archive, hex.EncodeToString(hash[:])); err == nil {
		t.Error("checksum verification followed an archive symlink outside its directory")
	}
}
