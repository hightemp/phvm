package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
)

func TestNativePHPTransactionalBuild(t *testing.T) {
	archive := os.Getenv("PHVM_NATIVE_PHP_ARCHIVE")
	if archive == "" {
		t.Skip("set PHVM_NATIVE_PHP_ARCHIVE to run the real PHP source build")
	}
	version := os.Getenv("PHVM_NATIVE_PHP_VERSION")
	if version == "" {
		version = "8.3.30"
	}
	expected := os.Getenv("PHVM_NATIVE_PHP_SHA256")
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if expected == "" || hex.EncodeToString(hash.Sum(nil)) != expected {
		t.Fatal("native PHP archive SHA256 does not match supplied digest")
	}
	for _, tool := range []string{"tar", "make", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	for _, name := range []string{"CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS", "LIBS", "PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR"} {
		t.Setenv(name, "")
	}
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	b := NewBuilder(p, nil)
	logPath := os.Getenv("PHVM_NATIVE_PHP_LOG")
	if logPath == "" {
		logPath = filepath.Join(t.TempDir(), "native-build.log")
	}
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	b.SetLogWriter(log)
	opts := BuildOptions{Version: version, TarballPath: archive, Profile: "minimal", Jobs: 2, SHA256: expected, Verification: &remote.VerifyResult{SHA256Verified: true, GPGSkipped: true, GPGSkipReason: "local native smoke with supplied SHA256"}}
	if err := b.Build(ctx, opts); err != nil {
		t.Fatalf("real PHP build: %v; log=%s", err, logPath)
	}
	metadata, err := core.LoadMetadata(p.VersionMetadata(version))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.InstallationState != "ready" || metadata.PHPAPI == "" || !metadata.SHA256Verified {
		t.Fatalf("native publication metadata=%+v", metadata)
	}
	if !core.NewInstalledManager(p).IsInstalled(version) {
		t.Fatal("native ready publication not installed")
	}
	output, err := exec.CommandContext(ctx, filepath.Join(p.VersionBin(version), core.PHPBinary()), "-n", "-r", "echo PHP_VERSION;").CombinedOutput()
	if err != nil || string(output) != version {
		t.Fatalf("published native PHP runtime: %v %s", err, output)
	}
	if transactions, _ := filepath.Glob(filepath.Join(p.Versions, ".php-install-*")); len(transactions) != 0 {
		t.Fatal("native transaction not cleaned")
	}
	t.Logf("Native PHP %s: API %s, isolated source/configure/make/INSTALL_ROOT/runtime/publication verified", version, metadata.PHPAPI)
}
