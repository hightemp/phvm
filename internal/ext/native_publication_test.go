package ext

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func nativeModule(t *testing.T, module string, badABI, dependency bool) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("native Linux PHP extension fixture")
	}
	config, err := exec.LookPath("php-config")
	if err != nil {
		t.Skip("PHP development headers unavailable")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler unavailable")
	}
	includes, err := exec.Command(config, "--includes").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src, binary := filepath.Join(dir, "fixture.c"), filepath.Join(dir, module+".so")
	deps := "ZEND_MOD_END"
	if dependency {
		deps = `ZEND_MOD_REQUIRED("phvm_dep") ZEND_MOD_END`
	}
	changeAPI := ""
	if badABI {
		changeAPI = "entry.zend_api = ZEND_MODULE_API_NO + 1;"
	}
	code := fmt.Sprintf(`#include "php.h"
static const zend_module_dep deps[] = { %s };
static zend_module_entry entry = {
    STANDARD_MODULE_HEADER_EX, NULL, deps, "%s", NULL,
    NULL, NULL, NULL, NULL, NULL, "1.0.0", STANDARD_MODULE_PROPERTIES
};
ZEND_DLEXPORT zend_module_entry *get_module(void) { %s return &entry; }
`, deps, module, changeAPI)
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	args := append(strings.Fields(string(includes)), "-shared", "-fPIC", "-o", binary, src)
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile native fixture: %v\n%s", err, output)
	}
	return binary
}

func TestInstallWithRealPHPArtifact(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP unavailable")
	}
	for _, tt := range []struct {
		name, module             string
		badABI, dependency, fail bool
	}{
		{name: "valid", module: "http"},
		{name: "wrong ABI", module: "http", badABI: true, fail: true},
		{name: "different module in correctly named file", module: "http_extra", fail: true},
		{name: "enabled dependency", module: "http", dependency: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			artifact := nativeModule(t, tt.module, tt.badABI, tt.dependency)
			p := installationFixture(t, "cp "+shellLiteral(artifact)+" modules/http.so", "exit 99")
			launcher := "#!/bin/sh\nexec " + shellLiteral(php) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(launcher), 0755); err != nil {
				t.Fatal(err)
			}
			if tt.dependency {
				dep := nativeModule(t, "phvm_dep", false, false)
				data, err := os.ReadFile(dep)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/phvm_dep.so"), data, 0600); err != nil {
					t.Fatal(err)
				}
				writeExtensionFile(t, p, "01-dependency.ini", "extension=phvm_dep.so\n")
			}
			err := installHTTP(context.Background(), t, p)
			if (err != nil) != tt.fail {
				t.Fatalf("install error=%v, fail=%v", err, tt.fail)
			}
			if tt.fail {
				for _, path := range []string{p.VersionMetadata("8.3.30"), filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("bad native artifact published: %s", path)
					}
				}
				return
			}
			args := []string{"-n"}
			if tt.dependency {
				args = append(args, "-d", "extension="+filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/phvm_dep.so"))
			}
			args = append(args, "-d", "extension="+filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so"), "-m")
			output, err := exec.Command(php, args...).CombinedOutput()
			if err != nil || !loadedModules(output)["http"] {
				t.Fatalf("published native module did not load: %v\n%s", err, output)
			}
			if err := NewInstaller(p, nil).Uninstall(context.Background(), "pecl_http", "8.3.30"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")); !os.IsNotExist(err) {
				t.Error("native binary survived uninstall")
			}
			if tt.dependency {
				if _, err := os.Stat(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/phvm_dep.so")); err != nil {
					t.Error("dependency removed")
				}
			}
		})
	}
}

func TestPublicationWithRealZendExtension(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native Linux Zend extension fixture")
	}
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP unavailable")
	}
	config, err := exec.LookPath("php-config")
	if err != nil {
		t.Skip("php-config unavailable")
	}
	dir, err := exec.Command(config, "--extension-dir").Output()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(strings.TrimSpace(string(dir)), "opcache.so")
	if _, err := os.Stat(source); os.IsNotExist(err) {
		t.Skip("shared OPcache unavailable")
	} else if err != nil {
		t.Fatal(err)
	}
	p := installationFixture(t, "cp "+shellLiteral(source)+" modules/opcache.so", "exit 99")
	writeHTTPArchive(t, p, "opcache")
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\nexec "+shellLiteral(php)+" \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"))
	if err != nil || !strings.Contains(string(data), "zend_extension=") {
		t.Fatalf("wrong Zend directive: %v %s", err, data)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil || !meta.Extensions["pecl_http"].Zend {
		t.Fatalf("Zend identity not persisted: %v", err)
	}
	if err := NewInstaller(p, nil).Uninstall(context.Background(), "opcache", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/opcache.so")); !os.IsNotExist(err) {
		t.Error("owned Zend binary survived uninstall")
	}
	if _, err := os.Stat(source); err != nil {
		t.Error("read-only source Zend library changed")
	}
}
