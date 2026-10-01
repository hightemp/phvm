package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestBuildDiagnosticsRedactsAcrossWritesAndKeepsCauseBounded(t *testing.T) {
	t.Setenv("PHVM_BUILD_SECRET", "environment-secret-value")
	var full bytes.Buffer
	diagnostics := newBuildDiagnostics(&full)
	for _, fragment := range []string{"TOKEN=", "split-secret-value\n", "ld: undefined reference to first_symbol\n", "fatal error: curl/curl.h: No such file or directory\n", "environment-secret-value"} {
		if _, err := diagnostics.Write([]byte(fragment)); err != nil {
			t.Fatal(err)
		}
	}
	if err := diagnostics.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(full.String(), "split-secret-value") || strings.Contains(full.String(), "environment-secret-value") {
		t.Errorf("split or unlabelled secret leaked: %s", &full)
	}
	if !strings.Contains(diagnostics.cause(), "curl/curl.h") || len(diagnostics.cause()) > 400 {
		t.Errorf("wrong bounded cause: %q", diagnostics.cause())
	}
}

func TestInstallBuildFailureShowsBoundedCauseAndRedactedFullLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure/make fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHVM_BUILD_SECRET", "environment-secret-value")
	script := phpConfigureFixture(p.VersionDir("8.5.11"), "8.5.11", "")
	recipe := `@i=0; while [ $$i -lt 500 ]; do echo 'ld: undefined reference to repeated_symbol'; i=$$((i+1)); done; echo 'fatal error: curl/curl.h: No such file or directory'; printf 'TOKEN='; printf 'split-secret-value\n'; echo 'Authorization: Bearer bearer-secret-value'; echo 'https://user:pass@example.invalid/file?token=query-secret-value'; echo "$$PHVM_BUILD_SECRET"; exit 2`
	script = strings.Replace(script, "@true", recipe, 1)
	archive := scenarioPHPArchive(t, script)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(sum[:])}}})
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	config := fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)
	if err := os.WriteFile(p.ConfigFile(), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostics := commandStreams(t, bin, p, "install", "8.5.11", "--profile", "minimal", "--jobs", "1")
	if code != 1 || out != "" {
		t.Fatalf("failed build: exit=%d stdout=%q stderr=%q", code, out, diagnostics)
	}
	logPath := p.LogFile("install-8.5.11")
	if len(diagnostics) > 3000 || !strings.Contains(diagnostics, "curl/curl.h") || !strings.Contains(diagnostics, logPath) || strings.Count(diagnostics, "undefined reference") > 2 {
		t.Errorf("diagnostic is not a bounded cause with full-log path: %q", diagnostics)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(logData), "undefined reference to repeated_symbol") < 400 {
		t.Error("full build log lost repeated linker output")
	}
	for _, secret := range []string{"split-secret-value", "bearer-secret-value", "user:pass", "query-secret-value", "environment-secret-value"} {
		if strings.Contains(diagnostics, secret) || strings.Contains(string(logData), secret) {
			t.Errorf("build diagnostic or log disclosed %q", secret)
		}
	}
	if !strings.Contains(string(logData), "REDACTED") {
		t.Error("full log lacks redaction markers")
	}
	if info, err := os.Stat(filepath.Clean(logPath)); err != nil || info.Mode().Perm()&0077 != 0 {
		t.Errorf("build log permissions=%v, error=%v", info, err)
	}
}

func TestInstallConfigureFailureUsesCompilerCauseFromConfigLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX configure fixture")
	}
	withoutConfigEnv(t)
	bin := buildTestCLI(t)
	fakeBuildTools(t)
	p := core.NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo '--with-pear'; exit 0; fi\n" +
		"i=0; while [ $i -lt 200 ]; do echo 'checking another flag'; i=$((i+1)); done\n" +
		"printf 'ld: cannot find -lcurl\\nTOKEN=configlog-secret\\n' > config.log\necho 'configure: error: libcurl check failed' >&2\nexit 1\n"
	archive := scenarioPHPArchive(t, script)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "8.5.11", "source": []map[string]string{{"filename": "php-8.5.11.tar.gz", "sha256": hex.EncodeToString(sum[:])}}})
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	config := fmt.Sprintf("[remote]\nmirror=%q\nretries=0\n[verify]\ngpg=false\n", server.URL)
	if err := os.WriteFile(p.ConfigFile(), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostics := commandStreams(t, bin, p, "install", "8.5.11", "--profile", "minimal", "--jobs", "1")
	if code != 1 || out != "" || len(diagnostics) > 3000 || !strings.Contains(diagnostics, "cannot find -lcurl") || !strings.Contains(diagnostics, "linker") {
		t.Errorf("configure cause not summarized: exit=%d out=%q stderr=%q", code, out, diagnostics)
	}
	data, err := os.ReadFile(p.LogFile("install-8.5.11"))
	if err != nil || !strings.Contains(string(data), "cannot find -lcurl") || strings.Count(string(data), "checking another flag") < 150 || strings.Contains(string(data), "configlog-secret") {
		t.Errorf("full configure/config.log output missing: %v %q", err, data)
	}
}
