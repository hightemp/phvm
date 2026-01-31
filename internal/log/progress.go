package log

import (
	"io"
	"os"

	"github.com/schollz/progressbar/v3"
)

// ProgressBar wraps schollz/progressbar for download progress.
type ProgressBar struct {
	bar    *progressbar.ProgressBar
	writer io.Writer
}

// ProgressOptions configures the progress bar.
type ProgressOptions struct {
	Description string
	Total       int64
	Writer      io.Writer
	ShowBytes   bool
	ShowSpeed   bool
}

// NewProgressBar creates a new progress bar.
func NewProgressBar(opts ProgressOptions) *ProgressBar {
	if opts.Writer == nil {
		opts.Writer = os.Stderr
	}

	options := []progressbar.Option{
		progressbar.OptionSetDescription(opts.Description),
		progressbar.OptionSetWriter(opts.Writer),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	}

	if opts.ShowBytes {
		options = append(options, progressbar.OptionShowBytes(true))
	}

	if opts.ShowSpeed {
		options = append(options, progressbar.OptionShowIts())
	}

	bar := progressbar.NewOptions64(opts.Total, options...)

	return &ProgressBar{
		bar:    bar,
		writer: opts.Writer,
	}
}

// Write implements io.Writer for use with io.TeeReader.
func (p *ProgressBar) Write(b []byte) (int, error) {
	return p.bar.Write(b)
}

// Add adds to the progress.
func (p *ProgressBar) Add(n int) error {
	return p.bar.Add(n)
}

// Add64 adds to the progress (int64).
func (p *ProgressBar) Add64(n int64) error {
	return p.bar.Add64(n)
}

// Finish completes the progress bar.
func (p *ProgressBar) Finish() error {
	return p.bar.Finish()
}

// Clear clears the progress bar.
func (p *ProgressBar) Clear() error {
	return p.bar.Clear()
}

// ChangeMax changes the maximum value.
func (p *ProgressBar) ChangeMax64(max int64) {
	p.bar.ChangeMax64(max)
}

// Describe changes the description.
func (p *ProgressBar) Describe(desc string) {
	p.bar.Describe(desc)
}

// Spinner creates a simple spinner for indeterminate progress.
type Spinner struct {
	bar *progressbar.ProgressBar
}

// NewSpinner creates a new spinner.
func NewSpinner(description string) *Spinner {
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Spinner{bar: bar}
}

// Tick advances the spinner.
func (s *Spinner) Tick() {
	s.bar.Add(1)
}

// Finish completes the spinner.
func (s *Spinner) Finish() {
	s.bar.Finish()
}
