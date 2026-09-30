package deps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
)

func TestDependencyRejectsUnsafePkgConfigBeforeMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX dependency build fixture")
	}
	paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	depsDir := filepath.Join(paths.Root, "deps", "8.0.30")
	prefix := filepath.Join(depsDir, "openssl")
	pkgDir := filepath.Join(prefix, "lib", "pkgconfig")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external.pc")
	const original = "Libs: -lssl\n"
	if err := os.WriteFile(outside, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pkgDir, "libssl.pc")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	sentinel := filepath.Join(paths.Root, "configure-executed")
	archive := dependencyArchiveNamed(t, "openssl", "1", sentinel)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()
	dep := withDependencyHash(t, Dependency{Name: "openssl", Version: "1", URL: server.URL + "/openssl-1.tar.gz", ConfigureCmd: []string{"./configure", "--prefix=%PREFIX%"}}, hex.EncodeToString(sum[:]))
	manager := NewDepsManager(paths, remote.NewClient(remote.ClientOptions{Retries: 0}), 1)
	if err := manager.ensureDep(context.Background(), dep, depsDir); err == nil {
		t.Error("published dependency despite unsafe pkg-config path")
	}
	if _, err := os.Stat(filepath.Join(prefix, ".phvm-installed")); !os.IsNotExist(err) {
		t.Errorf("ready marker published after failure: %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != original {
		t.Errorf("external file changed: %q, %v", got, err)
	}
}

func TestFixPkgConfigRejectsSymlinkPaths(t *testing.T) {
	for _, tc := range []struct {
		name, packageName, fileName string
		fix                         func(*DepsManager, string) error
	}{
		{"libssl", "openssl", "libssl.pc", (*DepsManager).fixOpenSSLPkgConfig},
		{"libcrypto", "openssl", "libcrypto.pc", (*DepsManager).fixOpenSSLPkgConfig},
		{"libcurl", "curl", "libcurl.pc", (*DepsManager).fixCurlPkgConfig},
	} {
		for _, pathKind := range []string{"file", "pkgconfig-directory"} {
			t.Run(tc.name+"/"+pathKind, func(t *testing.T) {
				paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
				if err := paths.EnsureDirectories(); err != nil {
					t.Fatal(err)
				}
				depsDir := filepath.Join(paths.Root, "deps", "7.4.33")
				pkgDir := filepath.Join(depsDir, tc.packageName, "lib", "pkgconfig")
				if err := os.MkdirAll(filepath.Dir(pkgDir), 0755); err != nil {
					t.Fatal(err)
				}
				outsideDir := t.TempDir()
				external := filepath.Join(outsideDir, tc.fileName)
				const original = "Libs: -lssl -lcrypto\n"
				if err := os.WriteFile(external, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				link := pkgDir
				target := outsideDir
				if pathKind == "file" {
					if err := os.Mkdir(pkgDir, 0755); err != nil {
						t.Fatal(err)
					}
					link = filepath.Join(pkgDir, tc.fileName)
					target = external
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if err := tc.fix(NewDepsManager(paths, nil, 1), depsDir); err == nil {
					t.Error("accepted a symlink-controlled pkg-config path")
				}
				got, err := os.ReadFile(external)
				if err != nil || string(got) != original {
					t.Errorf("outside pkg-config file changed: %q, %v", got, err)
				}
			})
		}
	}
}

func TestFixPkgConfigPreservesRegularFiles(t *testing.T) {
	for _, tc := range []struct {
		name, packageName, fileName string
		fix                         func(*DepsManager, string) error
	}{
		{"libssl", "openssl", "libssl.pc", (*DepsManager).fixOpenSSLPkgConfig},
		{"libcrypto", "openssl", "libcrypto.pc", (*DepsManager).fixOpenSSLPkgConfig},
		{"libcurl", "curl", "libcurl.pc", (*DepsManager).fixCurlPkgConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := core.NewPaths(filepath.Join(t.TempDir(), "managed"))
			if err := paths.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			depsDir := filepath.Join(paths.Root, "deps", "7.4.33")
			pkgDir := filepath.Join(depsDir, tc.packageName, "lib", "pkgconfig")
			if err := os.MkdirAll(pkgDir, 0755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(pkgDir, tc.fileName)
			if err := os.WriteFile(file, []byte("Libs: -lssl -lcrypto\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.fix(NewDepsManager(paths, nil, 1), depsDir); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) == "Libs: -lssl -lcrypto\n" {
				t.Errorf("pkg-config file was not updated: %q, %v", got, err)
			}
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm() != before.Mode().Perm() {
				t.Errorf("pkg-config permissions changed: %v, %v", info, err)
			}
		})
	}
}
