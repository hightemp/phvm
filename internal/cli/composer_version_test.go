package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposerInstallVersionFlagRejectsUnsafeReleaseBeforeNetwork(t *testing.T) {
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	p := seedInstalledVersions(t)
	out, err := exec.Command(bin, "--phvm-dir", p.Root, "composer", "install", "--php", "8.3", "--version", "../2.2.30").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "exact X.Y.Z") {
		t.Fatalf("unsafe Composer release was accepted: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(p.Composer, "8.3.30")); !os.IsNotExist(err) {
		t.Errorf("invalid release changed Composer storage: %v", err)
	}
}
