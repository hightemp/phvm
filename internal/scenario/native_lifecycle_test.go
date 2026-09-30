// Package scenario contains full lifecycle integration checks.
package scenario

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/composer"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/ext"
	"github.com/hightemp/phvm/internal/ini"
	"github.com/hightemp/phvm/internal/remote"
)

func literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func nativeOutput(ctx context.Context, t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %q: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestScenarioNativeComposerPECLAndIniLifecycle(t *testing.T) {
	if os.Getenv("PHVM_SCENARIO_NATIVE") != "1" {
		t.Skip("opt-in upstream Composer/PECL lifecycle; requires network and real PHP SDK")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("this native source scenario requires Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tools := make(map[string]string)
	for _, name := range []string{"php", "php-config", "phpize", "make", "cc", "autoconf"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		absolute, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		tools[name] = absolute
	}
	version := nativeOutput(ctx, t, tools["php"], "-n", "-r", "echo PHP_VERSION;")
	if configVersion := nativeOutput(ctx, t, tools["php-config"], "--version"); configVersion != version {
		t.Fatalf("PHP/SDK version mismatch: %s / %s", version, configVersion)
	}
	for _, name := range []string{"CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS", "LIBS", "PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR", "PHPRC", "PHP_INI_SCAN_DIR", "PHP_AUTOCONF", "PHP_AUTOHEADER"} {
		t.Setenv(name, "")
	}
	t.Setenv("CC", tools["cc"])
	p := core.NewPaths(filepath.Join(t.TempDir(), "isolated-native-root"))
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	extDir := filepath.Join(p.VersionDir(version), "lib", "extensions")
	for _, dir := range []string{p.VersionBin(version), p.VersionConfD(version), extDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, text string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	// Only regular launchers and owned metadata are published in the temporary root.
	// SDK executables/headers are read-only; phpize/configure/make run in temp source.
	php := fmt.Sprintf("#!/bin/sh\nexport PHPRC=${PHPRC:-%s}\nexport PHP_INI_SCAN_DIR=${PHP_INI_SCAN_DIR:-%s}\nexec %s \"$@\"\n", literal(p.VersionPhpIni(version)), literal(p.VersionConfD(version)), literal(tools["php"]))
	write(filepath.Join(p.VersionBin(version), core.PHPBinary()), php, 0755)
	write(filepath.Join(p.VersionBin(version), "phpize"), "#!/bin/sh\nexec "+literal(tools["phpize"])+" \"$@\"\n", 0755)
	config := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n--prefix) echo %s;;\n--extension-dir) echo %s;;\n*) exec %s \"$@\";;\nesac\n", literal(p.VersionDir(version)), literal(extDir), literal(tools["php-config"]))
	write(filepath.Join(p.VersionBin(version), "php-config"), config, 0755)
	write(p.VersionPhpIni(version), "memory_limit=256M\ndate.timezone=UTC\n", 0600)
	if err := core.NewCurrentManager(p).Set(version); err != nil {
		t.Fatal(err)
	}
	if err := core.NewAliasManager(p).SetDefault(version); err != nil {
		t.Fatal(err)
	}

	m := composer.NewManager(p)
	if err := m.Install(ctx, version); err != nil {
		t.Fatal(err)
	}
	composerBin := filepath.Join(p.VersionBin(version), "composer")
	before := nativeOutput(ctx, t, composerBin, "--version", "--no-ansi")
	if !strings.HasPrefix(before, "Composer version ") {
		t.Fatal(before)
	}
	if err := m.Update(ctx, version); err != nil {
		t.Fatal(err)
	}
	if after := nativeOutput(ctx, t, composerBin, "--version", "--no-ansi"); !strings.HasPrefix(after, "Composer version ") {
		t.Fatal(after)
	}

	client := remote.NewClient(remote.ClientOptions{Timeout: 60 * time.Second, Retries: 0})
	installer := ext.NewInstaller(p, client)
	buildLog, err := os.Create(filepath.Join(p.Logs, "native-redis.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer buildLog.Close()
	installer.SetLogWriter(buildLog)
	if err := installer.Install(ctx, ext.InstallOptions{Name: "redis", Version: "6.0.0", PHPVersion: version, Jobs: 2}); err != nil {
		data, _ := os.ReadFile(buildLog.Name())
		t.Fatalf("real PECL build: %v\n%s", err, data)
	}
	phpBin := filepath.Join(p.VersionBin(version), core.PHPBinary())
	checkRedis := func(want string) {
		t.Helper()
		if out := nativeOutput(ctx, t, phpBin, "-r", `echo extension_loaded('redis') ? 'yes' : 'no';`); out != want {
			t.Fatalf("redis loaded=%s want=%s", out, want)
		}
	}
	checkRedis("yes")
	profile := ini.NewProfileManager(p)
	if err := profile.SaveContext(ctx, "with-redis", version); err != nil {
		t.Fatal(err)
	}
	if err := ext.DisableContext(ctx, p, version, "redis"); err != nil {
		t.Fatal(err)
	}
	checkRedis("no")
	if err := profile.ApplyContext(ctx, "with-redis", version, true); err != nil {
		t.Fatal(err)
	}
	checkRedis("yes")
	metadata, err := core.LoadMetadata(p.VersionMetadata(version))
	if err != nil {
		t.Fatal(err)
	}
	entry := metadata.Extensions["redis"]
	if entry.SourceVerification != "pecl-https-manifest" || entry.BinarySHA256 == "" || entry.SourceSHA256 == "" || !entry.Enabled {
		t.Fatalf("native ownership/trust not recorded: %+v", entry)
	}
	if err := remote.NewDownloader(client, p.Extensions).ClearCacheContext(ctx); err != nil {
		t.Fatal(err)
	}
	checkRedis("yes")
	if out := nativeOutput(ctx, t, composerBin, "--version", "--no-ansi"); !strings.HasPrefix(out, "Composer version ") {
		t.Fatal(out)
	}
	if err := installer.Uninstall(ctx, "redis", version); err != nil {
		t.Fatal(err)
	}
	checkRedis("no")
	if _, err := os.Stat(filepath.Join(p.VersionDir(version), entry.Binary)); !os.IsNotExist(err) {
		t.Fatalf("owned library retained: %v", err)
	}
	if err := m.DisableContext(ctx, version); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(nativeOutput(ctx, t, phpBin, filepath.Join(p.Composer, "composer.phar"), "--version"), "Composer version ") {
		t.Fatal("shared Composer removed by disable")
	}
	if err := core.NewCurrentManager(p).Clear(); err != nil {
		t.Fatal(err)
	}
	if err := core.NewInstalledManager(p).RemoveContext(ctx, version); err != nil {
		t.Fatal(err)
	}
	if core.NewInstalledManager(p).IsInstalled(version) {
		t.Fatal("native scenario PHP registration survived uninstall")
	}
	t.Logf("Real PHP %s, %s, PECL redis 6.0.0: verified download/build/publication, disable/profile restore/cache clear/uninstall", version, before)
}
