package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func quoteShell(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func transactionFixture(t *testing.T, failure string, previous bool) (*Builder, BuildOptions) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP build fixture")
	}
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	final := p.VersionDir("8.3.30")
	write := func(path, data string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	if previous {
		write(filepath.Join(final, "bin/php"), "#!/bin/sh\necho 'old working PHP'\n", 0755)
		write(filepath.Join(final, "bin/composer"), "old composer launcher", 0755)
		write(filepath.Join(final, "etc/php.ini"), "; user configuration\nmemory_limit=321M\n", 0640)
		write(filepath.Join(final, "etc/conf.d/90-custom.ini.disabled"), "; custom disabled config\n", 0600)
		write(filepath.Join(final, "user-file"), "user data", 0600)
		meta := core.NewMetadata("8.3.30")
		meta.AddExtension("redis", "6.0.0", false)
		if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
			t.Fatal(err)
		}
		if err := core.NewCurrentManager(p).Set("8.3.30"); err != nil {
			t.Fatal(err)
		}
		if err := core.NewAliasManager(p).SetDefault("8.3.30"); err != nil {
			t.Fatal(err)
		}
	}
	tools := t.TempDir()
	version := "8.3.30"
	if failure == "wrong version" {
		version = "8.3.29"
	}
	api := "20230831"
	if failure == "ABI" {
		api = "20230832"
	}
	php := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in\n-n*-r*) printf '%%s\\n' %s %s %s 0 0 cli;;\n*-r*) echo '%s';;\n*-i*) printf 'PHP API => 20230831\\n';;\n*) echo 'PHP %s';;\nesac\n", quoteShell(version), quoteShell(final), quoteShell(filepath.Join(final, "lib/php/extensions/no-debug-non-zts-20230831")), version, version)
	if failure == "post install" {
		php = "#!/bin/sh\necho 'runtime failure' >&2\nexit 1\n"
	}
	if failure == "startup warning" {
		php = strings.Replace(php, "case ", "echo 'PHP Warning: wrong module API' >&2\ncase ", 1)
	}
	write(filepath.Join(tools, "php-template"), php, 0755)
	config := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n--prefix) echo %s;;\n--version) echo '8.3.30';;\n--extension-dir) echo %s;;\n*) exit 1;;\nesac\n", quoteShell(final), quoteShell(filepath.Join(final, "lib/php/extensions/no-debug-non-zts-20230831")))
	write(filepath.Join(tools, "config-template"), config, 0755)
	installScript := ""
	if failure == "metadata" {
		installScript = "mkdir \"$target/.phvm-metadata.json\"\n"
	}
	if failure == "ini" {
		installScript = "mkdir -p \"$target/etc/php.ini\"\n"
	}
	makeScript := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" != install ]; then\n [ %s != make ]\n exit $?\nfi\nroot=\nfor arg in \"$@\"; do case \"$arg\" in INSTALL_ROOT=*) root=${arg#INSTALL_ROOT=};; esac; done\nprefix=$(cat prefix)\ntarget=\"$root$prefix\"\nmkdir -p \"$target/bin\" \"$target/include/php/Zend\" \"$target/lib/php/extensions/no-debug-non-zts-20230831\"\ncp %s \"$target/bin/php\"\ncp %s \"$target/bin/php-config\"\nprintf '#!/bin/sh\\necho PHP Api Version: 20230831\\n' > \"$target/bin/phpize\"\nchmod 755 \"$target/bin/php\" \"$target/bin/phpize\" \"$target/bin/php-config\"\nprintf '#define ZEND_MODULE_API_NO %s\\n' > \"$target/include/php/Zend/zend_modules.h\"\n%s\n[ %s != install ]\n", quoteShell(failure), quoteShell(filepath.Join(tools, "php-template")), quoteShell(filepath.Join(tools, "config-template")), api, installScript, quoteShell(failure))
	write(filepath.Join(tools, "make"), makeScript, 0755)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	configure := fmt.Sprintf("#!/bin/sh\nfor arg in \"$@\"; do case \"$arg\" in --prefix=*) printf '%%s' \"${arg#--prefix=}\" > prefix;; esac; done\nprintf '%%s\\n' \"$@\" > %s\n[ %s != configure ]\n", quoteShell(filepath.Join(p.Root, "configure-args")), quoteShell(failure))
	archive := filepath.Join(t.TempDir(), "php.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{"php-8.3.30/configure": configure, "php-8.3.30/php.ini-production": "; production ini\nmemory_limit=128M\n"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return NewBuilder(p, nil), BuildOptions{Version: "8.3.30", TarballPath: archive, Profile: "minimal", Jobs: 1}
}

func snapshotInstallation(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%o:%s", info.Mode().Perm(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFailedPHPBuildPreservesPreviousInstallation(t *testing.T) {
	for _, failure := range []string{"configure", "make", "install", "post install", "wrong version", "ABI", "startup warning", "metadata", "ini"} {
		t.Run(failure, func(t *testing.T) {
			b, opts := transactionFixture(t, failure, true)
			before := snapshotInstallation(t, b.paths.VersionDir(opts.Version))
			if err := b.Build(context.Background(), opts); err == nil {
				t.Error("invalid installation reported success")
			}
			after := snapshotInstallation(t, b.paths.VersionDir(opts.Version))
			if fmt.Sprint(before) != fmt.Sprint(after) {
				t.Errorf("previous installation changed\nbefore=%v\nafter=%v", before, after)
			}
			if current, err := core.NewCurrentManager(b.paths).Get(); err != nil || current != opts.Version {
				t.Errorf("current changed: %s %v", current, err)
			}
		})
	}
}

func TestFailedNewPHPBuildNeverAppearsInstalled(t *testing.T) {
	for _, failure := range []string{"install", "post install", "wrong version", "metadata"} {
		t.Run(failure, func(t *testing.T) {
			b, opts := transactionFixture(t, failure, false)
			if err := b.Build(context.Background(), opts); err == nil {
				t.Error("invalid build accepted")
			}
			if core.NewInstalledManager(b.paths).IsInstalled(opts.Version) {
				t.Error("partial installation reported installed")
			}
			if versions, err := core.NewInstalledManager(b.paths).List(); err != nil || len(versions) != 0 {
				t.Errorf("partial installation listed: %v %v", versions, err)
			}
			if _, err := os.Stat(b.paths.VersionDir(opts.Version)); !os.IsNotExist(err) {
				t.Errorf("partial final directory remains: %v", err)
			}
		})
	}
}

func TestSuccessfulPHPBuildPublishesMetadataAndPreservesUserFiles(t *testing.T) {
	b, opts := transactionFixture(t, "", true)
	if err := b.Build(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(b.paths.VersionMetadata(opts.Version))
	if err != nil {
		t.Fatal(err)
	}
	if !meta.HasExtension("redis") {
		t.Error("reinstall lost extension metadata")
	}
	data, err := os.ReadFile(b.paths.VersionMetadata(opts.Version))
	if err != nil || !strings.Contains(string(data), `"installation_state": "ready"`) {
		t.Errorf("published metadata lacks ready state: %s %v", data, err)
	}
	if data, err := os.ReadFile(b.paths.VersionPhpIni(opts.Version)); err != nil || !strings.Contains(string(data), "321M") {
		t.Error("user php.ini replaced")
	}
	if data, err := os.ReadFile(filepath.Join(b.paths.VersionBin(opts.Version), "composer")); err != nil || string(data) != "old composer launcher" {
		t.Error("Composer launcher lost")
	}
	if data, err := os.ReadFile(filepath.Join(b.paths.VersionDir(opts.Version), "user-file")); err != nil || string(data) != "user data" {
		t.Error("user file lost")
	}
	args, err := os.ReadFile(filepath.Join(b.paths.Root, "configure-args"))
	if err != nil || !strings.Contains(string(args), "--prefix="+b.paths.VersionDir(opts.Version)) {
		t.Error("configure does not keep final prefix")
	}
}

func TestPHPDirectoryPublicationRollsBackLateValidationAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			b, opts := transactionFixture(t, "", true)
			before := snapshotInstallation(t, b.paths.VersionDir(opts.Version))
			tx, err := newPHPTransaction(b.paths, opts.Version, b.paths.VersionDir(opts.Version))
			if err != nil {
				t.Fatal(err)
			}
			defer tx.close()
			if err := os.MkdirAll(tx.candidate, 0755); err != nil {
				t.Fatal(err)
			}
			if err := tx.writeMetadata(core.NewMetadata(opts.Version)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("published runtime failure")
			err = tx.publish(ctx, func() error {
				if cancelled {
					cancel()
					return nil
				}
				return failure
			})
			if cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if after := snapshotInstallation(t, b.paths.VersionDir(opts.Version)); fmt.Sprint(before) != fmt.Sprint(after) {
				t.Error("late publication error did not restore previous bytes and permissions")
			}
		})
	}
}

func TestReinstallPreservesOwnedExtensionOverNewBundledFile(t *testing.T) {
	b, opts := transactionFixture(t, "", true)
	const binary = "lib/php/extensions/no-debug-non-zts-20230831/redis.so"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(b.paths.VersionDir(opts.Version), binary)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.paths.VersionDir(opts.Version), binary), []byte("owned module"), 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(b.paths.VersionMetadata(opts.Version))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("owned module"))
	entry := meta.Extensions["redis"]
	entry.Module = "redis"
	entry.Binary = binary
	entry.BinarySHA256 = hex.EncodeToString(hash[:])
	meta.Extensions["redis"] = entry
	if err := meta.Save(b.paths.VersionMetadata(opts.Version)); err != nil {
		t.Fatal(err)
	}
	makePath := filepath.Join(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0], "make")
	script, err := os.ReadFile(makePath)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "[ '' != install ]", "printf 'new bundled module' > \"$target/"+binary+"\"\n[ '' != install ]", 1))
	if err := os.WriteFile(makePath, script, 0755); err != nil {
		t.Fatal(err)
	}
	if err := b.Build(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(b.paths.VersionDir(opts.Version), binary)); err != nil || string(data) != "owned module" {
		t.Errorf("owned binary was overwritten, invalidating persisted ownership: %q %v", data, err)
	}
}
