// Package ui provides terminal styles for human-readable CLI output.
package ui

import (
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// Mode controls automatic, forced, or disabled terminal colors.
type Mode string

const (
	// Auto enables colors only for a suitable terminal and enabled configuration.
	Auto Mode = "auto"
	// Always explicitly enables colors, including redirected output.
	Always Mode = "always"
	// Never disables all color styles.
	Never Mode = "never"
)

// Tone describes the meaning of a highlighted piece of output.
type Tone int

const (
	Heading Tone = iota
	Name
	Value
	Success
	Warning
	Error
	Info
	Muted
)

// Palette applies styles without changing the underlying text.
type Palette struct{ enabled bool }

// ParseMode validates a CLI color mode.
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case Auto, Always, Never:
		return Mode(value), nil
	default:
		return Auto, fmt.Errorf("--color must be auto, always or never")
	}
}

// New selects colors independently for the actual destination stream.
func New(out io.Writer, mode Mode, configured bool) Palette {
	return paletteFor(mode, configured, isTerminal(out), os.Getenv("NO_COLOR") != "", os.Getenv("TERM") == "dumb")
}

// Plain returns a palette that emits no escape sequences.
func Plain() Palette { return Palette{} }

func paletteFor(mode Mode, configured, terminal, noColor, dumb bool) Palette {
	switch mode {
	case Always:
		return Palette{enabled: true}
	case Never:
		return Plain()
	default:
		return Palette{enabled: configured && terminal && !noColor && !dumb}
	}
}

func isTerminal(out io.Writer) bool {
	for i := 0; i < 8; i++ {
		if wrapped, ok := out.(interface{ UnwrapWriter() io.Writer }); ok {
			out = wrapped.UnwrapWriter()
			continue
		}
		if file, ok := out.(interface{ Fd() uintptr }); ok {
			return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
		}
		return false
	}
	return false
}

// Enabled reports whether terminal styles will be emitted.
func (p Palette) Enabled() bool { return p.enabled }

// Text highlights a semantic value while preserving the plain representation.
func (p Palette) Text(tone Tone, value string) string {
	if !p.enabled || value == "" {
		return value
	}
	var attributes []color.Attribute
	switch tone {
	case Heading:
		attributes = []color.Attribute{color.Bold, color.FgCyan}
	case Name:
		attributes = []color.Attribute{color.Bold}
	case Value, Info:
		attributes = []color.Attribute{color.FgCyan}
	case Success:
		attributes = []color.Attribute{color.FgGreen}
	case Warning:
		attributes = []color.Attribute{color.FgYellow}
	case Error:
		attributes = []color.Attribute{color.Bold, color.FgRed}
	case Muted:
		attributes = []color.Attribute{color.Faint}
	}
	style := color.New(attributes...)
	style.EnableColor()
	return style.Sprint(value)
}
