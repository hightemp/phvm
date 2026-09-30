package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
)

func dependencyArchive(t *testing.T, version, sentinel string) []byte {
	return dependencyArchiveNamed(t, "fixture", version, sentinel)
}

func dependencyArchiveNamed(t *testing.T, name, version, sentinel string) []byte {
	t.Helper()
	script := "#!/bin/sh\nprefix=${1#--prefix=}\nprintf executed >> '" + sentinel + "'\ncat > Makefile <<EOF\nall:\n\t@true\ninstall:\n\tmkdir -p '$prefix'\n\techo library > '$prefix/library'\nEOF\n"
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name + "-" + version + "/configure", Mode: 0755, Size: int64(len(script))}); err != nil {
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
	return out.Bytes()
}

func withDependencyHash(t *testing.T, dep Dependency, hash string) Dependency {
	t.Helper()
	// The pre-fix type ignores this field, exposing the missing integrity check.
	data, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	values["SHA256"] = hash
	data, _ = json.Marshal(values)
	if err := json.Unmarshal(data, &dep); err != nil {
		t.Fatal(err)
	}
	return dep
}

func TestDependencyRejectsUntrustedArchiveBeforeConfigure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX dependency build fixture")
	}
	for _, scenario := range []string{"tampered", "missing hash", "bad hash", "legacy marker", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			depsDir := filepath.Join(p.Root, "deps", "8.0.30")
			sentinel := filepath.Join(p.Root, "executed")
			payload := dependencyArchive(t, "1", sentinel)
			hash := strings.Repeat("a", 64)
			if scenario == "missing hash" {
				hash = ""
			}
			if scenario == "bad hash" {
				hash = "wrong"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "truncated" {
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)+10))
				}
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			if scenario == "legacy marker" {
				if err := os.MkdirAll(filepath.Join(depsDir, "fixture"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(depsDir, "fixture", ".phvm-installed"), []byte("1"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			dep := withDependencyHash(t, Dependency{Name: "fixture", Version: "1", URL: server.URL + "/fixture.tar.gz", ConfigureCmd: []string{"./configure", "--prefix=%PREFIX%"}}, hash)
			client := remote.NewClient(remote.ClientOptions{Retries: 0})
			if err := NewDepsManager(p, client, 1).ensureDep(context.Background(), dep, depsDir); err == nil {
				t.Error("untrusted dependency accepted")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Error("unverified configure executed")
			}
		})
	}
}

func TestDependencyMarkerBindsVersionSourceAndArchiveHash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX dependency build fixture")
	}
	for _, change := range []string{"version", "source", "hash", "corrupt marker", "bad cache"} {
		t.Run(change, func(t *testing.T) {
			p := core.NewPaths(t.TempDir())
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			depsDir, sentinel := filepath.Join(p.Root, "deps", "8.0.30"), filepath.Join(p.Root, "executed")
			payload := dependencyArchive(t, "1", sentinel)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = w.Write(payload) }))
			defer server.Close()
			sum := sha256.Sum256(payload)
			dep := withDependencyHash(t, Dependency{Name: "fixture", Version: "1", URL: server.URL + "/fixture.tar.gz", ConfigureCmd: []string{"./configure", "--prefix=%PREFIX%"}}, hex.EncodeToString(sum[:]))
			m := NewDepsManager(p, remote.NewClient(remote.ClientOptions{Retries: 0}), 1)
			if err := m.ensureDep(context.Background(), dep, depsDir); err != nil {
				t.Fatal(err)
			}
			if err := m.ensureDep(context.Background(), dep, depsDir); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Error("valid marker did not avoid rebuild")
			}
			marker := filepath.Join(depsDir, "fixture", ".phvm-installed")
			data, _ := os.ReadFile(marker)
			var record struct{ Version, URL, SHA256 string }
			if err := json.Unmarshal(data, &record); err != nil || record.Version != dep.Version || record.URL != dep.URL || record.SHA256 != hex.EncodeToString(sum[:]) {
				t.Errorf("marker lacks verified identity: %s", data)
			}
			switch change {
			case "version":
				dep.Version = "2"
				payload = dependencyArchive(t, "2", sentinel)
				sum = sha256.Sum256(payload)
				dep = withDependencyHash(t, dep, hex.EncodeToString(sum[:]))
			case "source":
				dep.URL = server.URL + "/other/fixture.tar.gz"
			case "hash":
				payload = append(payload, 0)
				sum = sha256.Sum256(payload)
				dep = withDependencyHash(t, dep, hex.EncodeToString(sum[:]))
			case "corrupt marker":
				if err := os.WriteFile(marker, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "bad cache":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(depsDir, "src", "fixture.tar.gz"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.ensureDep(context.Background(), dep, depsDir); err != nil {
				t.Fatalf("rebuild: %v", err)
			}
			data, _ = os.ReadFile(sentinel)
			if string(data) != "executedexecuted" {
				t.Errorf("stale identity reused: %s", data)
			}
		})
	}
}

func TestDependencyMarkerTracksItsBuildDependencies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX dependency build fixture")
	}
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	depsDir, sentinel := filepath.Join(p.Root, "deps", "8.0.30"), filepath.Join(p.Root, "executed")
	parent := filepath.Join(depsDir, "parent")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	parentMarker := filepath.Join(parent, ".phvm-installed")
	if err := os.WriteFile(parentMarker, []byte(`{"Version":"1","SHA256":"parent1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	payload := dependencyArchive(t, "1", sentinel)
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	dep := withDependencyHash(t, Dependency{Name: "fixture", Version: "1", URL: server.URL + "/fixture.tar.gz", ConfigureCmd: []string{"./configure", "--prefix=%PREFIX%"}, DependsOn: []string{"parent"}}, hex.EncodeToString(sum[:]))
	m := NewDepsManager(p, remote.NewClient(remote.ClientOptions{Retries: 0}), 1)
	if err := m.ensureDep(context.Background(), dep, depsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentMarker, []byte(`{"Version":"2","SHA256":"parent2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureDep(context.Background(), dep, depsDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "executedexecuted" {
		t.Errorf("parent change did not rebuild dependent library: %q %v", data, err)
	}
}
