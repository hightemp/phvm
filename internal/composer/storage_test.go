package composer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposerInstallationSurvivesRemovedCache(t *testing.T) {
	php := realPHP(t)
	old := testComposerPHAR(t, php, "2.8.1", "")
	m, p := seedComposerUpdate(t, php, old)
	sum := sha256.Sum256(old)
	m.client = &http.Client{Transport: updateTransport(old, hex.EncodeToString(sum[:]), 200)}
	if err := m.InstallGlobal(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"8.3.30", "8.2.30"} {
		if err := m.Enable(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(p.Cache); err != nil {
		t.Fatal(err)
	}
	if !m.IsInstalledGlobally() {
		t.Error("working Composer was stored only in cache")
	}
	for _, v := range []string{"8.3.30", "8.2.30"} {
		out, err := exec.Command(filepath.Join(p.VersionBin(v), "composer"), "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), "2.8.1") {
			t.Errorf("Composer %s failed without cache: %v %s", v, err, out)
		}
	}
	// A failed update after cleanup must retain the persistent working version.
	newPHAR := testComposerPHAR(t, php, "2.9.0", "")
	m.client = &http.Client{Transport: updateTransport(newPHAR, strings.Repeat("0", 64), 200)}
	if err := m.Update(context.Background(), "8.3.30"); err == nil {
		t.Error("bad checksum accepted")
	}
	out, err := exec.Command(filepath.Join(p.VersionBin("8.3.30"), "composer"), "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "2.8.1") {
		t.Errorf("failed update after cache cleanup destroyed working Composer: %v %s", err, out)
	}
	// Recovery publishes to the same permanent location, not to recreated cache.
	sum = sha256.Sum256(newPHAR)
	m.client = &http.Client{Transport: updateTransport(newPHAR, hex.EncodeToString(sum[:]), 200)}
	if err := m.Update(context.Background(), "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.Cache); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(filepath.Join(p.VersionBin("8.2.30"), "composer"), "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "2.8.1") {
		t.Errorf("other PHP Composer changed after update: %v %s", err, out)
	}
	out, err = exec.Command(filepath.Join(p.VersionBin("8.3.30"), "composer"), "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "2.9.0") {
		t.Errorf("updated PHP Composer failed after second cleanup: %v %s", err, out)
	}
}
