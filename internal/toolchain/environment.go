// Package toolchain shares build environment and command parsing with diagnostics.
package toolchain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/redact"
)

// Environment holds a snapshot of the environment passed to build tools.
type Environment []string

// Current snapshots the process environment, replacing keys with explicit overrides.
func Current(overrides ...string) Environment {
	values := make(map[string]string)
	for _, item := range append(os.Environ(), overrides...) {
		if key, value, ok := strings.Cut(item, "="); ok {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var env Environment
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

// Value returns a snapshot's variable, using fallback for an empty/unset value.
func (e Environment) Value(key, fallback string) string {
	for _, item := range e {
		if value, ok := strings.CutPrefix(item, key+"="); ok && value != "" {
			return value
		}
	}
	return fallback
}

// Command parses an explicit tool command without invoking a shell.
func (e Environment) Command(ctx context.Context, key, fallback string, args ...string) (*exec.Cmd, error) {
	return e.command(ctx, key, fallback, process.CommandContext, args...)
}

// InteractiveCommand parses an editor command and preserves foreground terminal access.
func (e Environment) InteractiveCommand(ctx context.Context, key, fallback string, args ...string) (*exec.Cmd, error) {
	return e.command(ctx, key, fallback, process.InteractiveCommandContext, args...)
}

func (e Environment) command(ctx context.Context, key, fallback string, newCommand func(context.Context, string, ...string) *exec.Cmd, args ...string) (*exec.Cmd, error) {
	command, err := SplitArguments(e.Value(key, fallback))
	if err != nil || len(command) == 0 || command[0] == "" {
		return nil, fmt.Errorf("invalid %s command", key)
	}
	cmd := newCommand(ctx, command[0], append(command[1:], args...)...)
	cmd.Env = []string(e)
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// Describe reports only build-related variables and selected tool paths.
func (e Environment) Describe(ctx context.Context) string {
	var out strings.Builder
	out.WriteString("Build environment:\n")
	for _, tool := range []struct{ key, fallback string }{{"CC", "cc"}, {"CXX", "c++"}, {"PKG_CONFIG", "pkg-config"}} {
		_, _ = fmt.Fprintf(&out, "  %s=%s", tool.key, e.Value(tool.key, tool.fallback))
		if cmd, err := e.Command(ctx, tool.key, tool.fallback); err == nil {
			_, _ = fmt.Fprintf(&out, " (resolved: %s)", cmd.Path)
			if target, err := filepath.EvalSymlinks(cmd.Path); err == nil && target != cmd.Path {
				fmt.Fprintf(&out, " (target: %s)", target)
			}
		}
		out.WriteByte('\n')
	}
	for _, key := range []string{"LD", "PATH", "CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS", "LIBS", "PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR", "PKG_CONFIG_SYSROOT_DIR", "CURL_CFLAGS", "CURL_LIBS", "OPENSSL_CFLAGS", "OPENSSL_LIBS"} {
		_, _ = fmt.Fprintf(&out, "  %s=%s\n", key, e.Value(key, ""))
	}
	for _, prefix := range []string{"ICU", "PNG", "JPEG", "FREETYPE2", "ONIG", "LIBXML", "SQLITE", "PGSQL", "LIBSODIUM", "LIBZIP", "XSL"} {
		for _, suffix := range []string{"_CFLAGS", "_LIBS"} {
			key := prefix + suffix
			if value := e.Value(key, ""); value != "" {
				fmt.Fprintf(&out, "  %s=%s\n", key, value)
			}
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ld := "ld"
	var compilerFlags []string
	for _, key := range []string{"CPPFLAGS", "CFLAGS", "LDFLAGS"} {
		flags, _ := SplitArguments(e.Value(key, ""))
		compilerFlags = append(compilerFlags, flags...)
	}
	ccArgs, _ := SplitArguments(e.Value("CC", "cc"))
	for _, flag := range append(ccArgs, compilerFlags...) {
		if value, ok := strings.CutPrefix(flag, "-fuse-ld="); ok {
			ld = "ld." + value
		}
	}
	if cmd, err := e.Command(probeCtx, "CC", "cc", append(compilerFlags, "-print-prog-name="+ld)...); err == nil {
		if data, err := cmd.Output(); err == nil && strings.TrimSpace(string(data)) != "" {
			path := strings.TrimSpace(string(data))
			if resolved, err := exec.LookPath(path); err == nil {
				path = resolved
			}
			_, _ = fmt.Fprintf(&out, "  linker selected by CC: %s\n", path)
		} else {
			out.WriteString("  linker selected by CC: could not determine\n")
		}
	}
	return redact.Text(out.String())
}

// SplitArguments handles shell quoting and escaping without shell expansion.
func SplitArguments(value string) ([]string, error) {
	var flags []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, ch := range value {
		switch {
		case escaped:
			if quote == '"' && !strings.ContainsRune("$`\"\\\n", ch) {
				word.WriteRune('\\')
			}
			if ch != '\n' {
				word.WriteRune(ch)
				started = true
			}
			escaped = false
		case ch == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
		case ch == '\'' || ch == '"':
			quote, started = ch, true
		case unicode.IsSpace(ch):
			if started {
				flags = append(flags, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(ch)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if started {
		flags = append(flags, word.String())
	}
	return flags, nil
}
