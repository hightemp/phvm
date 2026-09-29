package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestCLIExtensionConfigureUsesExactArgumentsAndOwnSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	for _, repeated := range []bool{false, true} {
		t.Run(fmt.Sprint(repeated), func(t *testing.T) {
			p := core.NewPaths(filepath.Join(t.TempDir(), "root with space"))
			if err := p.EnsureDirectories(); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(p.VersionBin("8.3.30"), 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{core.PHPBinary(), "phpize", "php-config"} {
				if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := core.NewCurrentManager(p).Set("8.3.30"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p.ConfigFile(), []byte("[build]\ndefault_flags=['--with-pear']\n"), 0600); err != nil {
				t.Fatal(err)
			}
			capture := filepath.Join(p.Root, "argv")
			marker := filepath.Join(p.Root, "must-not-execute")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--with-message --enable-redis'; exit 0; fi\nprintf '%s\\0' \"$@\" > " + quote(capture) + "\nexit 1\n"
			file, err := os.Create(p.ExtensionCachePath("redis", "6.0.0"))
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			for name, data := range map[string]string{"package.xml": "<package><name>redis</name><channel>pecl.php.net</channel><version><release>6.0.0</release></version><providesextension>redis</providesextension></package>", "redis-6.0.0/config.m4": "fixture", "redis-6.0.0/configure": script} {
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
			literal := "--with-message=$(touch " + marker + ") a,b"
			archive, err := os.ReadFile(p.ExtensionCachePath("redis", "6.0.0"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(archive)
			args := []string{"--phvm-dir", p.Root, "ext", "install", "redis", "--version", "6.0.0", "--sha256", hex.EncodeToString(hash[:])}
			if repeated {
				args = append(args, "--configure-flag="+literal, "--configure-flag=--enable-redis", "--configure-flag=--disable-redis")
			} else {
				args = append(args, "--configure", quote(literal)+" --enable-redis --disable-redis")
			}
			out, err := exec.Command(bin, args...).CombinedOutput()
			if err == nil {
				t.Error("fixture configure failure ignored")
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatalf("configure did not receive flags: %v %s", err, out)
			}
			actual := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
			want := []string{literal, "--disable-redis", "--with-php-config=" + filepath.Join(p.VersionBin("8.3.30"), "php-config")}
			if !reflect.DeepEqual(actual, want) {
				t.Errorf("PECL argv=%q want=%q", actual, want)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Error("configure argument executed as shell code")
			}
		})
	}
}
