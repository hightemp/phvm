package configure

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/toolchain"
)

var helpOption = regexp.MustCompile(`--[a-zA-Z][a-zA-Z0-9_-]*`)

// CheckSupported checks explicit options against the exact source's configure help.
func CheckSupported(ctx context.Context, script, dir string, env toolchain.Environment, args []string) error {
	if len(args) == 0 {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := process.CommandContext(probeCtx, script, "--help")
	cmd.Dir = dir
	cmd.Env = []string(env)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if probeCtx.Err() != nil {
		return probeCtx.Err()
	}
	if err != nil {
		return fmt.Errorf("read configure --help: %w: %s", redact.Error(err, ""), redact.Text(string(output)))
	}
	available := make(map[string]bool)
	for _, option := range helpOption.FindAllString(string(output), -1) {
		if option == "--enable-FEATURE" || option == "--with-PACKAGE" || option == "--without-PACKAGE" {
			continue
		}
		available[Key(option)] = true
	}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		if !available[Key(arg)] {
			return fmt.Errorf("configure option %q is not supported by this source/version", arg)
		}
	}
	return nil
}

// RecordEnvironment selects build-related values and redacts URL credentials.
func RecordEnvironment(env toolchain.Environment) map[string]string {
	result := make(map[string]string)
	for _, key := range []string{"CC", "CXX", "PKG_CONFIG", "LD", "PATH", "CFLAGS", "CPPFLAGS", "CXXFLAGS", "LDFLAGS", "LIBS", "PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR", "PKG_CONFIG_SYSROOT_DIR", "OPENSSL_CFLAGS", "OPENSSL_LIBS", "CURL_CFLAGS", "CURL_LIBS"} {
		fallback := ""
		if key == "CC" {
			fallback = "cc"
		}
		if key == "CXX" {
			fallback = "c++"
		}
		if key == "PKG_CONFIG" {
			fallback = "pkg-config"
		}
		result[key] = redact.Text(env.Value(key, fallback))
	}
	return result
}
