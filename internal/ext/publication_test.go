package ext

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
)

func shellLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func installationFixture(t *testing.T, artifact, probe string) *core.Paths {
	t.Helper()
	p := extensionFixture(t)
	extDir := filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions")
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(p.VersionBin("8.3.30"), "phpize"), "#!/bin/sh\nexit 0\n")
	write(filepath.Join(p.VersionBin("8.3.30"), "php-config"), "#!/bin/sh\nprintf '%s' "+shellLiteral(extDir)+"\n")
	write(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), "#!/bin/sh\ncase \"$*\" in\n*extension=*|*zend_extension=*)\n"+probe+"\n;;\n*) printf '[PHP Modules]\\nCore\\nstandard\\n[Zend Modules]\\n';;\nesac\n")
	tools := t.TempDir()
	// The old make-install path writes directly to the live installation.
	write(filepath.Join(tools, "make"), "#!/bin/sh\nif [ \"$1\" = install ]; then\n touch "+shellLiteral(filepath.Join(extDir, "make-install-ran"))+"\n for f in modules/*; do [ ! -f \"$f\" ] || cp \"$f\" "+shellLiteral(extDir)+"; done\nelse\n mkdir -p modules\n "+artifact+"\nfi\n")
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeHTTPArchive(t, p, "http")
	return p
}

func writeHTTPArchive(t *testing.T, p *core.Paths, module string) {
	t.Helper()
	file, err := os.Create(p.ExtensionCachePath("pecl_http", "4.3.0"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{
		"package.xml":               `<package><name>pecl_http</name><channel>pecl.php.net</channel><version><release>4.3.0</release></version><providesextension>` + module + `</providesextension></package>`,
		"pecl_http-4.3.0/config.m4": "fixture",
		"pecl_http-4.3.0/configure": "#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--with-greeting --with-php-config'; fi\nexit 0\n",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{tw, gz, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func installHTTP(ctx context.Context, t *testing.T, p *core.Paths) error {
	t.Helper()
	archive, err := os.ReadFile(p.ExtensionCachePath("pecl_http", "4.3.0"))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(archive)
	return NewInstaller(p, nil).Install(ctx, InstallOptions{Name: "pecl_http", Version: "4.3.0", PHPVersion: "8.3.30", SHA256: hex.EncodeToString(hash[:])})
}

func TestReinstallDoesNotOverwriteForeignIniWhenOwnedLoaderIsMissing(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	const foreign = "extension=rediscluster.so\n"
	writeExtensionFile(t, p, "20-pecl_http.ini", foreign)
	if err := installHTTP(context.Background(), t, p); err == nil {
		t.Error("unowned loading ini overwritten")
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini")); err != nil || string(data) != foreign {
		t.Error("foreign extension loader changed")
	}
}

func TestInstallRejectsUnusableArtifactBeforePublication(t *testing.T) {
	for _, tt := range []struct{ name, artifact, probe string }{
		{"missing", ":", "printf '[PHP Modules]\\nhttp\\n'"},
		{"unexpected name", "printf fixture > modules/http_extra.so", "printf '[PHP Modules]\\nhttp\\n'"},
		{"ABI failure despite zero exit", "printf invalid > modules/http.so", "printf 'PHP Warning: module API mismatch\\n' >&2; printf '[PHP Modules]\\nCore\\n'"},
		{"different loaded module", "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp_extra\\n'"},
		{"symlink artifact", "printf foreign > other.so; ln -s ../other.so modules/http.so", "printf '[PHP Modules]\\nhttp\\n'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := installationFixture(t, tt.artifact, tt.probe)
			if err := installHTTP(context.Background(), t, p); err == nil {
				t.Error("unusable artifact reported a successful installation")
			}
			for _, path := range []string{p.VersionMetadata("8.3.30"), filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/make-install-ran")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Errorf("failed build published live file: %s (%v)", path, err)
				}
			}
		})
	}
}

func TestInstallPublishesVerifiedArtifactAndUninstallRemovesIt(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nCore\\nhttp\\n[Zend Modules]\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	entry := meta.Extensions["pecl_http"]
	data, err := os.ReadFile(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Extensions map[string]struct {
			Hash string `json:"binary_sha256"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("fixture"))
	if raw.Extensions["pecl_http"].Hash != hex.EncodeToString(hash[:]) || entry.Module != "http" || filepath.Base(entry.Binary) != "http.so" {
		t.Errorf("publication lacks exact ownership: %s", data)
	}
	if err := Disable(p, "8.3.30", "http"); err != nil {
		t.Fatal(err)
	}
	if err := NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")); !os.IsNotExist(err) {
		t.Error("owned extension binary survived uninstall")
	}
	if _, err := os.Lstat(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini.disabled")); !os.IsNotExist(err) {
		t.Error("disabled ini survived uninstall")
	}
}

func TestUninstallRejectsChangedOwnedBinaryAndBuiltin(t *testing.T) {
	for _, module := range []string{"http", "Core"} {
		t.Run(module, func(t *testing.T) {
			p := extensionFixture(t)
			binary := "lib/extensions/" + strings.ToLower(module) + ".so"
			if err := os.WriteFile(filepath.Join(p.VersionDir("8.3.30"), binary), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			writeExtensionFile(t, p, "20-fixture.ini", "extension="+strings.ToLower(module)+".so\n")
			hash := sha256.Sum256([]byte("original"))
			metadata := fmt.Sprintf(`{"extensions":{"fixture":{"module":%q,"binary":%q,"ini_file":"20-fixture.ini","binary_sha256":%q}}}`, module, binary, hex.EncodeToString(hash[:]))
			if err := os.WriteFile(p.VersionMetadata("8.3.30"), []byte(metadata), 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewInstaller(p, nil).Uninstall(context.Background(), "fixture", "8.3.30"); err == nil {
				t.Error("unsafe uninstall succeeded")
			}
			if data, err := os.ReadFile(p.VersionMetadata("8.3.30")); err != nil || string(data) != metadata {
				t.Error("failed uninstall changed metadata")
			}
			if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-fixture.ini")); err != nil {
				t.Error("failed uninstall removed ini")
			}
			if data, err := os.ReadFile(filepath.Join(p.VersionDir("8.3.30"), binary)); err != nil || string(data) != "changed" {
				t.Error("foreign binary changed")
			}
		})
	}
}

func TestInstallDoesNotReplacePreviousWorkingExtensionOnProbeFailure(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	paths := []string{p.VersionMetadata("8.3.30"), filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")}
	saved := make(map[string]string)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		saved[path] = string(data)
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte("#!/bin/sh\nprintf '[PHP Modules]\\nCore\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installHTTP(context.Background(), t, p); err == nil {
		t.Error("failed upgrade reported success")
	}
	for path, want := range saved {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Errorf("previous working file changed: %s", path)
		}
	}
}

func TestUninstallRefusesForeignLoaderAndProtectedBinaryPath(t *testing.T) {
	for _, kind := range []string{"foreign loader", "outside path", "symlink", "shared binary"} {
		t.Run(kind, func(t *testing.T) {
			p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
			if err := installHTTP(context.Background(), t, p); err != nil {
				t.Fatal(err)
			}
			metadata, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
			if err != nil {
				t.Fatal(err)
			}
			entry := metadata.Extensions["pecl_http"]
			binary := filepath.Join(p.VersionDir("8.3.30"), entry.Binary)
			foreign := filepath.Join(t.TempDir(), "http.so")
			if err := os.WriteFile(foreign, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign loader":
				writeExtensionFile(t, p, entry.IniFile, "extension=\""+foreign+"\"\n")
			case "outside path":
				entry.Binary = foreign
				metadata.Extensions["pecl_http"] = entry
			case "symlink":
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, binary); err != nil {
					t.Fatal(err)
				}
			case "shared binary":
				metadata.Extensions["another_package"] = core.ExtMetadata{Module: "another_module", Binary: entry.Binary}
			}
			if err := metadata.Save(p.VersionMetadata("8.3.30")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(p.VersionMetadata("8.3.30"))
			if err != nil {
				t.Fatal(err)
			}
			if err := NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30"); err == nil {
				t.Error("unverified ownership accepted")
			}
			if data, err := os.ReadFile(p.VersionMetadata("8.3.30")); err != nil || string(data) != string(before) {
				t.Error("failed deletion changed metadata")
			}
			if data, err := os.ReadFile(foreign); err != nil || string(data) != "fixture" {
				t.Error("foreign library changed")
			}
			if _, err := os.Lstat(binary); err != nil {
				t.Error("owned library/link changed despite refusal")
			}
		})
	}
}

func TestInstallRejectsPackageChangingModuleOwnership(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	meta := core.NewMetadata("8.3.30")
	meta.Extensions["pecl_http"] = core.ExtMetadata{Module: "old_module", Binary: "lib/extensions/old_module.so"}
	if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	if err := installHTTP(context.Background(), t, p); err == nil {
		t.Error("package changed module and lost previous ownership")
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil || meta.Extensions["pecl_http"].Module != "old_module" {
		t.Error("old package ownership was replaced")
	}
}

func TestInstallInitializesNullExtensionMetadata(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := os.WriteFile(p.VersionMetadata("8.3.30"), []byte(`{"version":"8.3.30","extensions":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionMutationRollsBackOnLateWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires non-root user")
	}
	for _, operation := range []string{"install", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
			if err := installHTTP(context.Background(), t, p); err != nil {
				t.Fatal(err)
			}
			paths := []string{p.VersionMetadata("8.3.30"), filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")}
			before := make(map[string]string)
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(data)
			}
			// Binary/ini directories remain writable. Only the last metadata
			// publication fails after the earlier mutations have succeeded.
			if err := os.Chmod(p.VersionDir("8.3.30"), 0500); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Chmod(p.VersionDir("8.3.30"), 0755) }()
			var err error
			if operation == "install" {
				err = installHTTP(context.Background(), t, p)
			} else {
				err = NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30")
			}
			if err == nil {
				t.Error("late metadata failure reported success")
			}
			for path, want := range before {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Errorf("rollback did not restore %s: %v", path, err)
				}
			}
		})
	}
}

func TestCancelledExtensionProbePublishesNothing(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := installHTTP(ctx, t, p); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled probe error=%v", err)
	}
	for _, path := range []string{p.VersionMetadata("8.3.30"), filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini"), filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("cancelled install published %s", path)
		}
	}
}

func TestReinstallReplacesVerifiedBinaryAndDisabledIni(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	if err := Disable(p, "8.3.30", "http"); err != nil {
		t.Fatal(err)
	}
	makeBin := filepath.Join(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0], "make")
	if err := os.WriteFile(makeBin, []byte("#!/bin/sh\nmkdir -p modules\nprintf replacement > modules/http.so\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionDir("8.3.30"), "lib/extensions/http.so")); err != nil || string(data) != "replacement" {
		t.Error("verified replacement not published")
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Extensions["pecl_http"].BinarySHA256 != binaryHash([]byte("replacement")) || !meta.Extensions["pecl_http"].Enabled {
		t.Error("replacement ownership/state not published")
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini.disabled")); !os.IsNotExist(err) {
		t.Error("disabled counterpart survived reinstall")
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini")); err != nil {
		t.Error("active ini not published")
	}
}

func TestLegacyUninstallRetainsUnownedLibrary(t *testing.T) {
	p := extensionFixture(t)
	binary := "lib/extensions/http.so"
	file := filepath.Join(p.VersionDir("8.3.30"), binary)
	if err := os.WriteFile(file, []byte("unowned"), 0600); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, p, "20-pecl_http.ini", "extension=http.so\n")
	meta := core.NewMetadata("8.3.30")
	meta.Extensions["pecl_http"] = core.ExtMetadata{Module: "http", Binary: binary, IniFile: "20-pecl_http.ini"}
	if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	if err := NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "unowned" {
		t.Error("legacy ownership guessed and library removed")
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil || meta.HasExtension("pecl_http") {
		t.Error("legacy metadata not cleaned")
	}
}
