package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestCLIForcePHPInstallIsTransactional(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX build fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	for _, failure := range []string{"", "install", "metadata"} {
		t.Run(map[string]string{"": "success", "install": "partial install", "metadata": "metadata failure"}[failure], func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{p.VersionBin("8.5.11"), p.VersionConfD("8.5.11")} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			oldFiles := map[string]string{"bin/php": "#!/bin/sh\necho 'old PHP'\n", "bin/composer": "old launcher", "etc/php.ini": "memory_limit=321M\n", "etc/conf.d/90-user.ini.disabled": "; user file\n"}
			for name, data := range oldFiles {
				if err := os.WriteFile(filepath.Join(p.VersionDir("8.5.11"), name), []byte(data), 0755); err != nil {
					t.Fatal(err)
				}
			}
			meta := core.NewMetadata("8.5.11")
			meta.AddExtension("redis", "6.0.0", false)
			if err := meta.Save(p.VersionMetadata("8.5.11")); err != nil {
				t.Fatal(err)
			}
			if err := core.NewCurrentManager(p).Set("8.5.11"); err != nil {
				t.Fatal(err)
			}
			if err := core.NewAliasManager(p).SetDefault("8.5.11"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(p.VersionMetadata("8.5.11"))
			if err != nil {
				t.Fatal(err)
			}
			script := phpConfigureFixture(p.VersionDir("8.5.11"), "8.5.11", "")
			if failure != "" {
				line := "\tfalse\n"
				if failure == "metadata" {
					line = "\tmkdir '$(INSTALL_ROOT)" + p.VersionDir("8.5.11") + "/.phvm-metadata.json'\n"
				}
				script = strings.Replace(script, "\nEOF\n", "\n"+line+"EOF\n", 1)
			}
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{Name: "php-8.5.11/configure", Mode: 0755, Size: int64(len(script))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(script)); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(archive.Bytes())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/releases/" {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(hash[:])}}})
					return
				}
				if r.URL.Path == "/distributions/php-8.5.11.tar.gz" {
					_, _ = w.Write(archive.Bytes())
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			if err := os.WriteFile(p.ConfigFile(), []byte(fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "--phvm-dir", p.Root, "install", "8.5.11", "--profile", "minimal", "--jobs", "1", "--force").CombinedOutput()
			if (err != nil) != (failure != "") {
				t.Fatalf("force install error=%v expected failure=%s\n%s", err, failure, out)
			}
			if failure != "" {
				if strings.Contains(string(out), "installed successfully") {
					t.Error("failed force install reported success")
				}
				for name, want := range oldFiles {
					data, err := os.ReadFile(filepath.Join(p.VersionDir("8.5.11"), name))
					if err != nil || string(data) != want {
						t.Errorf("old file changed: %s", name)
					}
				}
				if data, err := os.ReadFile(p.VersionMetadata("8.5.11")); err != nil || !bytes.Equal(data, before) {
					t.Error("old metadata changed")
				}
			} else {
				meta, err := core.LoadMetadata(p.VersionMetadata("8.5.11"))
				if err != nil {
					t.Fatal(err)
				}
				if meta.InstallationState != "ready" || !meta.HasExtension("redis") || !meta.SHA256Verified {
					t.Errorf("bad published metadata: %+v", meta)
				}
				for _, name := range []string{"bin/composer", "etc/php.ini", "etc/conf.d/90-user.ini.disabled"} {
					data, err := os.ReadFile(filepath.Join(p.VersionDir("8.5.11"), name))
					if err != nil || string(data) != oldFiles[name] {
						t.Errorf("user file lost: %s", name)
					}
				}
			}
			if current, err := core.NewCurrentManager(p).Get(); err != nil || current != "8.5.11" {
				t.Error("force install changed current")
			}
			if alias, err := os.ReadFile(p.AliasFile("default")); err != nil || strings.TrimSpace(string(alias)) != "8.5.11" {
				t.Error("force install changed default")
			}
			versions, err := core.NewInstalledManager(p).List()
			if err != nil || !reflect.DeepEqual(versions, []string{"8.5.11"}) {
				t.Errorf("wrong installed versions: %v %v", versions, err)
			}
			if tx, _ := filepath.Glob(filepath.Join(p.Versions, ".php-install-*")); len(tx) != 0 {
				t.Errorf("transaction remnants: %v", tx)
			}
		})
	}
}
