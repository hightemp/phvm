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

func extensionFixture(t *testing.T) *core.Paths {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP fixture")
	}
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.VersionBin("8.3.30"), p.VersionConfD("8.3.30"), filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n-n) printf '[PHP Modules]\\nCore\\nstandard\\n[Zend Modules]\\n';;\n-r) printf '%%s' '%s';;\n*) printf '[PHP Modules]\\nCore\\nstandard\\nrediscluster\\n[Zend Modules]\\n';;\nesac\n", filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions"))
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeExtensionFile(t *testing.T, p *core.Paths, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.VersionConfD("8.3.30"), name), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionMutationsUseExactName(t *testing.T) {
	for _, operation := range []string{"enable", "disable", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			p := extensionFixture(t)
			name := "20-rediscluster.ini"
			if operation == "enable" {
				name += ".disabled"
			}
			const content = "; redis must not match rediscluster\nextension=rediscluster.so\n"
			writeExtensionFile(t, p, name, content)
			var err error
			switch operation {
			case "enable":
				err = Enable(p, "8.3.30", "redis")
			case "disable":
				err = Disable(p, "8.3.30", "redis")
			case "uninstall":
				err = NewInstaller(p, nil).Uninstall(context.Background(), "redis", "8.3.30")
			}
			if err == nil {
				t.Error("missing redis reported success or matched rediscluster")
			}
			data, readErr := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), name))
			if readErr != nil || string(data) != content {
				t.Errorf("unrelated rediscluster was modified: %s %v", data, readErr)
			}
		})
	}
}

func TestExtensionMatchesLoadingDirectiveInsteadOfFilename(t *testing.T) {
	p := extensionFixture(t)
	writeExtensionFile(t, p, "01-rediscluster.ini", "extension=rediscluster.so\n")
	writeExtensionFile(t, p, "99-custom.ini.disabled", "; extension=rediscluster.so\n  EXTENSION = \"redis.so\" ; an exact module\n")
	if err := Enable(p, "8.3.30", "ReDiS"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "99-custom.ini")); err != nil {
		t.Error("redis directive with custom ini name was not enabled")
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "01-rediscluster.ini")); err != nil {
		t.Error("rediscluster changed")
	}
}

func TestListIncludesDisabledAndMetadataOnlyExtensions(t *testing.T) {
	p := extensionFixture(t)
	writeExtensionFile(t, p, "20-redis.ini.disabled", "extension=redis.so\n")
	writeExtensionFile(t, p, "20-rediscluster.ini", "extension=rediscluster.so\n")
	metadata := core.NewMetadata("8.3.30")
	metadata.AddExtension("redis", "6.0.0", false)
	metadata.AddExtension("missing", "1.0.0", true)
	if err := metadata.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"redis.so", "rediscluster.so", "orphan.so"} {
		if err := os.WriteFile(filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	extensions, err := ListInstalled(p, "8.3.30")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Extension{}
	for _, extension := range extensions {
		found[strings.ToLower(extension.Name)] = extension
	}
	if extension, ok := found["redis"]; !ok || extension.Enabled || extension.IniFile != "20-redis.ini" || extension.Version != "6.0.0" {
		t.Errorf("disabled redis missing or wrong: %+v", extension)
	}
	if _, ok := found["missing"]; !ok {
		t.Error("metadata-only extension missing from inventory")
	}
	if extension, ok := found["orphan"]; !ok || extension.Enabled {
		t.Errorf("unloaded library missing or reported enabled: %+v", extension)
	}
	if extension := found["rediscluster"]; !extension.Enabled || extension.IniFile != "20-rediscluster.ini" {
		t.Errorf("rediscluster associated with redis ini: %+v", extension)
	}
}

func TestMixedExtensionIniIsNotMutated(t *testing.T) {
	p := extensionFixture(t)
	const content = "extension=redis.so\nextension=rediscluster.so\n"
	writeExtensionFile(t, p, "20-redis.ini", content)
	if err := Disable(p, "8.3.30", "redis"); err == nil {
		t.Error("disabling one extension changed a shared loading file")
	}
	if data, err := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini")); err != nil || string(data) != content {
		t.Error("mixed ini changed")
	}
}

func TestPackageModuleIdentityAndInventoryStates(t *testing.T) {
	p := extensionFixture(t)
	writeExtensionFile(t, p, "42-custom-loader.ini.disabled", "extension=http.so\n")
	metadata := core.NewMetadata("8.3.30")
	metadata.AddExtension("pecl_http", "4.3.0", false)
	entry := metadata.Extensions["pecl_http"]
	entry.Module = "http"
	entry.Binary = "lib/extensions/http.so"
	entry.IniFile = "42-custom-loader.ini"
	metadata.Extensions["pecl_http"] = entry
	metadata.AddExtension("missing", "1.0.0", true)
	if err := metadata.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions", "http.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pecl_http", "HTTP"} {
		if err := Enable(p, "8.3.30", name); err != nil {
			t.Fatal(err)
		}
		if err := Disable(p, "8.3.30", name); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ListInstalled(p, "8.3.30")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, e := range list {
		states[e.Name] = e.State
		if e.Name == "pecl_http" && (e.Module != "http" || e.Version != "4.3.0" || e.IniFile != "42-custom-loader.ini") {
			t.Errorf("package mapping lost: %+v", e)
		}
	}
	if states["pecl_http"] != "disabled" || states["missing"] != "missing" || states["core"] != "builtin" {
		t.Errorf("wrong inventory states: %v", states)
	}
	if err := NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.HasExtension("pecl_http") {
		t.Error("module-name uninstall did not remove package metadata")
	}
}

func TestAmbiguousAndMisleadingIniNames(t *testing.T) {
	for _, kind := range []string{"misleading name", "duplicate files", "enabled and disabled copies"} {
		t.Run(kind, func(t *testing.T) {
			p := extensionFixture(t)
			switch kind {
			case "misleading name":
				writeExtensionFile(t, p, "20-redis.ini", "; redis is only a label\nextension=rediscluster.so\n")
			case "duplicate files":
				writeExtensionFile(t, p, "20-redis.ini", "extension=redis.so\n")
				writeExtensionFile(t, p, "90-other.ini", "extension=redis.so\n")
			case "enabled and disabled copies":
				writeExtensionFile(t, p, "20-redis.ini", "extension=redis.so\n")
				writeExtensionFile(t, p, "20-redis.ini.disabled", "extension=redis.so\n")
			}
			if err := Disable(p, "8.3.30", "redis"); err == nil {
				t.Error("ambiguous/misleading identity accepted")
			}
		})
	}
}

func TestLegacyMetadataDoesNotInferOwnershipFromIniFilename(t *testing.T) {
	p := extensionFixture(t)
	const data = "extension=rediscluster.so\n"
	writeExtensionFile(t, p, "20-redis.ini", data)
	meta := core.NewMetadata("8.3.30")
	meta.AddExtension("redis", "6.0.0", true)
	if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	if err := Disable(p, "8.3.30", "redis"); err == nil {
		t.Error("legacy filename guessed ownership of a different module")
	}
	if content, err := os.ReadFile(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini")); err != nil || string(content) != data {
		t.Error("foreign module configuration changed")
	}
}

func TestMetadataErrorsAreNotHiddenByExtensionMutation(t *testing.T) {
	p := extensionFixture(t)
	writeExtensionFile(t, p, "20-redis.ini", "extension=redis.so\n")
	if err := os.WriteFile(p.VersionMetadata("8.3.30"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Disable(p, "8.3.30", "redis"); err == nil {
		t.Error("corrupt metadata ignored")
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-redis.ini")); err != nil {
		t.Error("ini changed before metadata validation")
	}
}

func TestListReadsConfiguredExternalExtensionDirectory(t *testing.T) {
	p := extensionFixture(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "redis.so")
	if err := os.WriteFile(binary, []byte("read-only fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n-r) printf '%%s' '%s';;\n*) printf '[PHP Modules]\\nCore\\n[Zend Modules]\\n';;\nesac\n", dir)
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, p, "70-custom.ini.disabled", "extension=\""+binary+"\"\n")
	list, err := ListInstalled(p, "8.3.30")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range list {
		if e.Name == "redis" {
			found = true
			if e.State != "disabled" {
				t.Errorf("existing external library not recognized: %+v", e)
			}
		}
	}
	if !found {
		t.Error("external extension omitted")
	}
	data, err := os.ReadFile(binary)
	if err != nil || string(data) != "read-only fixture" {
		t.Error("external library changed by inventory")
	}
}

func TestInventoryWithRealPHPBuiltinAndDisabledModule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP launcher")
	}
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP unavailable")
	}
	p := extensionFixture(t)
	extDir := filepath.Join(p.VersionDir("8.3.30"), "lib", "extensions")
	if err := os.WriteFile(p.VersionPhpIni("8.3.30"), []byte("extension_dir=\""+extDir+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	shellQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nexec " + shellQuote(php) + " -c " + shellQuote(p.VersionPhpIni("8.3.30")) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHP_INI_SCAN_DIR", p.VersionConfD("8.3.30"))
	writeExtensionFile(t, p, "20-redis.ini.disabled", "extension=redis.so\n")
	if err := os.WriteFile(filepath.Join(extDir, "redis.so"), []byte("not loaded because disabled"), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstalled(p, "8.3.30")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, e := range list {
		states[e.Name] = e.State
	}
	if states["core"] != "builtin" || states["redis"] != "disabled" {
		t.Errorf("real PHP inventory incorrect: %v", states)
	}
}
