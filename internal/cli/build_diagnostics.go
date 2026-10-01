package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hightemp/phvm/internal/redact"
)

// buildDiagnostics streams redacted output to the full log while retaining one
// useful error line for the CLI. Child stdout and stderr can write concurrently.
type buildDiagnostics struct {
	mu       sync.Mutex
	dst      io.Writer
	secrets  []string
	pending  []byte
	best     string
	bestRank int
	last     string
	writeErr error
}

func newBuildDiagnostics(dst io.Writer) *buildDiagnostics {
	return &buildDiagnostics{dst: dst, secrets: redact.EnvironmentSecrets(os.Environ())}
}

func (d *buildDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeErr != nil {
		return 0, d.writeErr
	}
	length := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			d.pending = append(d.pending, p...)
			break
		}
		d.pending = append(d.pending, p[:idx]...)
		if err := d.finishLine(true); err != nil {
			d.writeErr = err
			return 0, err
		}
		p = p[idx+1:]
	}
	return length, nil
}

func (d *buildDiagnostics) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeErr != nil {
		return d.writeErr
	}
	if len(d.pending) == 0 {
		return nil
	}
	d.writeErr = d.finishLine(false)
	return d.writeErr
}

func (d *buildDiagnostics) finishLine(newline bool) error {
	line := redact.WithSecrets(string(d.pending), d.secrets)
	d.pending = d.pending[:0]
	plain := strings.TrimSpace(line)
	if plain != "" {
		plain = shortDiagnosticLine(plain)
		d.last = plain
		if rank := diagnosticRank(plain); rank > d.bestRank {
			d.best, d.bestRank = plain, rank
		}
	}
	if d.dst == nil {
		return nil
	}
	if newline {
		line += "\n"
	}
	n, err := io.WriteString(d.dst, line)
	if err == nil && n < len(line) {
		return io.ErrShortWrite
	}
	return err
}

func shortDiagnosticLine(line string) string {
	const limit = 360
	line = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' {
			return -1
		}
		return r
	}, line)
	runes := []rune(line)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return line
}

func diagnosticRank(line string) int {
	text := strings.ToLower(line)
	switch {
	case strings.Contains(text, "fatal error:"):
		return 120
	case strings.Contains(text, "cannot find -l"), strings.Contains(text, "library not found"):
		return 110
	case strings.Contains(text, "undefined reference"), strings.Contains(text, "undefined symbol"):
		return 105
	case strings.Contains(text, "configure: error:"):
		return 95
	case strings.Contains(text, "error:"), strings.Contains(text, "permission denied"):
		return 70
	case strings.Contains(text, "make: ***"):
		return 30
	default:
		return 1
	}
}

func (d *buildDiagnostics) cause() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.best != "" {
		return d.best
	}
	return d.last
}

type installBuildError struct {
	cause, outputErr error
	stage, detail    string
	logPath          string
}

func (e *installBuildError) Unwrap() error { return e.cause }

func (e *installBuildError) Error() string {
	message := "build failed during " + e.stage
	var exit *exec.ExitError
	if errors.As(e.cause, &exit) && exit.ExitCode() >= 0 {
		message += fmt.Sprintf(" (exit %d)", exit.ExitCode())
	}
	detail := e.detail
	if detail == "" {
		detail, _, _ = strings.Cut(e.cause.Error(), "\n")
		detail = shortDiagnosticLine(redact.WithSecrets(detail, redact.EnvironmentSecrets(os.Environ())))
	}
	if detail != "" {
		message += "\nRelevant error: " + detail
		if hint := buildHint(detail); hint != "" {
			message += "\nHint: " + hint
		}
	}
	if e.logPath != "" {
		message += "\nFull build log: " + redact.WithSecrets(e.logPath, redact.EnvironmentSecrets(os.Environ()))
	} else {
		message += "\nFull build log unavailable"
	}
	if e.outputErr != nil {
		message += " (log write failed)"
	}
	return message
}

func buildStage(err error) string {
	s := err.Error()
	switch {
	case strings.HasPrefix(s, "build dependencies:"):
		return "dependency build"
	case strings.HasPrefix(s, "extract source:"):
		return "source extraction"
	case strings.HasPrefix(s, "configure:"):
		return "configure"
	case strings.HasPrefix(s, "make install:"):
		return "make install"
	case strings.HasPrefix(s, "make:"):
		return "make"
	case strings.HasPrefix(s, "validate staged PHP:"):
		return "PHP validation"
	default:
		return "PHP installation"
	}
}

func buildHint(detail string) string {
	s := strings.ToLower(detail)
	switch {
	case strings.Contains(s, "fatal error:") && strings.Contains(s, "no such file or directory"):
		return "a header or source file is missing; check development packages and include paths"
	case strings.Contains(s, "cannot find -l"), strings.Contains(s, "library not found"):
		return "the linker cannot find a library; check development packages and library paths"
	case strings.Contains(s, "undefined reference"), strings.Contains(s, "undefined symbol"):
		return "the linker cannot resolve a symbol; check library versions and linker flags"
	case strings.Contains(s, "configure: error:"):
		return "check the named dependency or flag with phvm doctor --php <version>"
	default:
		return "inspect the full log for the first failing command"
	}
}
