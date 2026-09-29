package ini

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/toolchain"
)

func TestProfileSnapshotAndInvalidStartupWithRealPHP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP launcher")
	}
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP unavailable")
	}
	p := profileFixture(t)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	launcher := "#!/bin/sh\nexec " + quote(php) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	m := NewProfileManager(p)
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "90-user.ini"), "date.timezone=UTC\n")
	if err := m.Save("saved", "8.3.30"); err != nil {
		t.Fatal(err)
	}
	writeProfileFile(t, p.VersionPhpIni("8.3.30"), "memory_limit=256M\n")
	writeProfileFile(t, filepath.Join(p.VersionConfD("8.3.30"), "99-extra.ini"), "precision=12\n")
	if err := m.Apply("saved", "8.3.30", true); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(php, "-c", p.VersionPhpIni("8.3.30"), "-r", `echo ini_get("memory_limit"), "|", ini_get("date.timezone");`)
	cmd.Env = []string(toolchain.Current("PHP_INI_SCAN_DIR=" + p.VersionConfD("8.3.30")))
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "128M|UTC" {
		t.Fatalf("real PHP configuration=%s error=%v", out, err)
	}
	for _, broken := range []string{"syntax", "extension"} {
		t.Run(broken, func(t *testing.T) {
			root := p.ProfileDir(broken)
			writeProfileFile(t, filepath.Join(root, "php.ini"), "memory_limit=128M\n")
			if broken == "syntax" {
				writeProfileFile(t, filepath.Join(root, "php.ini"), "memory_limit=\"unfinished\n")
			} else {
				writeProfileFile(t, filepath.Join(root, "conf.d", "20-missing.ini"), "extension=phvm_nonexistent_module.so\n")
			}
			before := configurationBytes(t, p.VersionEtc("8.3.30"))
			if err := m.ApplyContext(context.Background(), broken, "8.3.30", true); err == nil {
				t.Error("actual PHP startup failure accepted")
			}
			if after := configurationBytes(t, p.VersionEtc("8.3.30")); !reflect.DeepEqual(before, after) {
				t.Error("invalid real-PHP profile changed working configuration")
			}
		})
	}
}
