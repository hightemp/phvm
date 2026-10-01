// Package log provides structured logging for phvm.
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/ui"
)

// Level represents log level.
type Level int

const (
	// LevelQuiet suppresses all output except errors.
	LevelQuiet Level = iota
	// LevelNormal is the default level.
	LevelNormal
	// LevelVerbose shows additional info.
	LevelVerbose
	// LevelDebug shows debug information.
	LevelDebug
)

// Logger provides leveled logging.
type Logger struct {
	mu      sync.Mutex
	level   Level
	out     io.Writer
	errOut  io.Writer
	noColor bool
	mode    ui.Mode
	prefix  string
}

var defaultLogger = New(os.Stderr, LevelNormal)

// New creates a new Logger.
func New(out io.Writer, level Level) *Logger {
	return &Logger{
		level:  level,
		out:    out,
		errOut: out,
		mode:   ui.Auto,
	}
}

// Default returns the default logger.
func Default() *Logger {
	return defaultLogger
}

// SetDefault sets the default logger.
func SetDefault(l *Logger) {
	defaultLogger = l
}

// SetLevel sets the log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetNoColor disables colored output.
func (l *Logger) SetNoColor(noColor bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.noColor = noColor
}

// SetColorMode selects automatic, forced, or disabled colors for this logger.
func (l *Logger) SetColorMode(mode ui.Mode) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mode = mode
}

// Styles returns the logger's palette for the actual destination stream.
func (l *Logger) Styles(out io.Writer) ui.Palette {
	l.mu.Lock()
	defer l.mu.Unlock()
	return ui.New(out, l.mode, !l.noColor)
}

// SetOutput sets the output writer.
func (l *Logger) SetOutput(out io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = out
}

// SetPrefix sets a prefix for all log messages.
func (l *Logger) SetPrefix(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prefix = prefix
}

// Level returns current log level.
func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Logger) output(tone ui.Tone, prefix, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if l.prefix != "" {
		msg = l.prefix + msg
	}
	msg = redact.Text(msg)
	palette := ui.New(l.out, l.mode, !l.noColor)

	if prefix != "" {
		if tone == ui.Success || tone == ui.Warning || tone == ui.Error {
			msg = palette.Text(tone, msg)
		}
		fmt.Fprintf(l.out, "%s %s\n", palette.Text(tone, prefix), msg)
	} else {
		fmt.Fprintln(l.out, palette.Text(tone, msg))
	}
}

// Success prints a success message.
func (l *Logger) Success(format string, args ...interface{}) {
	l.output(ui.Success, "✓", format, args...)
}

// Info prints an info message.
func (l *Logger) Info(format string, args ...interface{}) {
	if l.Level() >= LevelNormal {
		l.output(ui.Info, "→", format, args...)
	}
}

// Warn prints a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.output(ui.Warning, "!", format, args...)
}

// Error prints an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.output(ui.Error, "✗", format, args...)
}

// Debug prints a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.Level() >= LevelDebug {
		l.output(ui.Info, "[DEBUG]", format, args...)
	}
}

// Verbose prints a verbose message.
func (l *Logger) Verbose(format string, args ...interface{}) {
	if l.Level() >= LevelVerbose {
		l.output(ui.Muted, "", format, args...)
	}
}

// Print prints a plain message.
func (l *Logger) Print(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.out, redact.Text(fmt.Sprintf(format, args...)))
}

// Printf prints a plain message without newline.
func (l *Logger) Printf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(l.out, redact.Text(fmt.Sprintf(format, args...)))
}

// Println prints values with newline.
func (l *Logger) Println(args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(l.out, redact.Text(fmt.Sprintln(args...)))
}

// PrintVersions prints a list of versions with markers.
func (l *Logger) PrintVersions(versions []string, current, defaultVer string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	palette := ui.New(l.out, l.mode, !l.noColor)

	for _, v := range versions {
		var markers []string
		if v == current {
			markers = append(markers, "current")
		}
		if v == defaultVer {
			markers = append(markers, "default")
		}

		if len(markers) > 0 {
			tone := ui.Value
			if v == current {
				tone = ui.Success
			}
			fmt.Fprintf(l.out, "%s %s\n", palette.Text(tone, "* "+v), palette.Text(ui.Muted, "("+strings.Join(markers, ", ")+")"))
		} else {
			fmt.Fprintf(l.out, "  %s\n", v)
		}
	}
}

// Package-level convenience functions.

// Success prints a success message.
func Success(format string, args ...interface{}) {
	defaultLogger.Success(format, args...)
}

// Info prints an info message.
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Warn prints a warning message.
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error prints an error message.
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

// Debug prints a debug message.
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Verbose prints a verbose message.
func Verbose(format string, args ...interface{}) {
	defaultLogger.Verbose(format, args...)
}

// Print prints a plain message.
func Print(format string, args ...interface{}) {
	defaultLogger.Print(format, args...)
}

// Printf prints a plain message without newline.
func Printf(format string, args ...interface{}) {
	defaultLogger.Printf(format, args...)
}

// Println prints values with newline.
func Println(args ...interface{}) {
	defaultLogger.Println(args...)
}
