package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// checkLibraryLink verifies headers and a public symbol without running the probe.
func checkLibraryLink(name string) error {
	probes := map[string]string{
		"openssl":    "#include <openssl/ssl.h>\nint main(void) { return SSL_new(0) == 0; }\n",
		"libcurl":    "#include <curl/curl.h>\nint main(void) { return curl_easy_init() == 0; }\n",
		"zlib":       "#include <zlib.h>\nint main(void) { return zlibVersion() == 0; }\n",
		"libxml-2.0": "#include <libxml/parser.h>\nint main(void) { xmlInitParser(); return 0; }\n",
		"oniguruma":  "#include <oniguruma.h>\nint main(void) { return onig_initialize(0, 0); }\n",
		"readline":   "#include <readline/readline.h>\nint main(void) { return readline(0) == 0; }\n",
		"sqlite3":    "#include <sqlite3.h>\nint main(void) { return sqlite3_libversion() == 0; }\n",
	}
	source, ok := probes[name]
	if !ok {
		return nil
	}

	compiler := os.Getenv("CC")
	if compiler == "" {
		compiler = "cc"
	}
	command, err := splitCompilerFlags(compiler)
	if err != nil || len(command) == 0 {
		return fmt.Errorf("compile/link check: invalid CC %q", compiler)
	}
	args := append([]string{}, command[1:]...)
	for _, key := range []string{"CPPFLAGS", "CFLAGS"} {
		flags, err := splitCompilerFlags(os.Getenv(key))
		if err != nil {
			return fmt.Errorf("compile/link check: parse %s: %w", key, err)
		}
		args = append(args, flags...)
	}

	output, err := exec.Command("pkg-config", "--cflags", "--libs", name).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output = exitErr.Stderr
		}
		return fmt.Errorf("compile/link check: read pkg-config flags: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	pkgFlags, err := splitCompilerFlags(string(output))
	if err != nil {
		return fmt.Errorf("compile/link check: parse pkg-config flags: %w", err)
	}
	ldFlags, err := splitCompilerFlags(os.Getenv("LDFLAGS"))
	if err != nil {
		return fmt.Errorf("compile/link check: parse LDFLAGS: %w", err)
	}

	dir, err := os.MkdirTemp("", "phvm-doctor-*")
	if err != nil {
		return fmt.Errorf("compile/link check: create probe directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	sourcePath := filepath.Join(dir, "check.c")
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		return fmt.Errorf("compile/link check: write probe: %w", err)
	}
	args = append(args, sourcePath, "-o", filepath.Join(dir, "check"))
	args = append(args, ldFlags...)
	args = append(args, pkgFlags...)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile/link check failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// splitCompilerFlags handles shell quoting/escaping without executing shell code.
func splitCompilerFlags(value string) ([]string, error) {
	var flags []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, ch := range value {
		switch {
		case escaped:
			// In double quotes, backslash only escapes these shell characters.
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
