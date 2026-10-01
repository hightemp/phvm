package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestOpenInstallLogTightensExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	paths := core.NewPaths(t.TempDir())
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	path := paths.LogFile("install-8.5.11")
	if err := os.WriteFile(path, []byte("old log"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	file, err := openInstallLog(paths, "8.5.11")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("reused build log mode=%v, error=%v", info, err)
	}
}

func TestInstallLogSymlinkCannotOverwriteOutsideRoot(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	paths := core.NewPaths(t.TempDir())
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-log")
	const original = "keep external data\n"
	if err := os.WriteFile(outside, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.LogFile("install-8.5.11")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	script := phpConfigureFixture(paths.VersionDir("8.5.11"), "8.5.11", "")
	archive := scenarioPHPArchive(t, script)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(sum[:])}}})
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	config := fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)
	if err := os.WriteFile(paths.ConfigFile(), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostics := commandStreams(t, bin, paths, "install", "8.5.11", "--profile", "minimal", "--jobs", "1")
	if code != 0 {
		t.Fatalf("install failed: %s", diagnostics)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != original {
		t.Errorf("install log changed outside file: %q, %v", got, err)
	}
	if !core.NewInstalledManager(paths).IsInstalled("8.5.11") {
		t.Error("unavailable log prevented verified installation")
	}
}
