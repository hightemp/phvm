package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/composer"
	"github.com/hightemp/phvm/internal/core"
)

func TestComposerUpdateDoesNotReportFalseSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PHP fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.VersionBin("8.3.30"), 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate PHP's zero exit when given a shell wrapper, and a genuine PHAR error.
	php := `#!/bin/sh
case "$1" in
*/composer) printf '#!/bin/sh\nexec php composer.phar\n'; exit 0;;
*) echo 'invalid Composer PHAR' >&2; exit 7;;
esac
`
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(php), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Composer, "composer.phar"), []byte("preserve-working-file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := composer.NewManager(p).Enable("8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := core.NewCurrentManager(p).Set("8.3.30"); err != nil {
		t.Fatal(err)
	}
	if err := core.NewAliasManager(p).Set("prod", "8.3"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--php", "prod"}, {"--php", "8.3"}} {
		command := append([]string{"--phvm-dir", p.Root, "composer", "update"}, args...)
		out, err := exec.Command(bin, command...).CombinedOutput()
		if err == nil || strings.Contains(string(out), "Composer updated") || !strings.Contains(string(out), "invalid Composer PHAR") {
			t.Errorf("false update success: %v %s", err, out)
		}
		data, err := os.ReadFile(filepath.Join(p.Composer, "composer.phar"))
		if err != nil || string(data) != "preserve-working-file" {
			t.Error("failed CLI update changed active PHAR")
		}
		current, err := core.NewCurrentManager(p).Get()
		if err != nil || current != "8.3.30" {
			t.Error("Composer update changed current PHP")
		}
	}
}
