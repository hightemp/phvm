package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/ui"
)

func validateColorFlags(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("color") {
		return nil
	}
	if cmd.Flags().Changed("no-color") {
		return fmt.Errorf("use either --color or --no-color")
	}
	_, err := ui.ParseMode(colorMode)
	return err
}

func commandColorMode(cmd *cobra.Command) ui.Mode {
	if cmd.Flags().Changed("no-color") && noColor {
		return ui.Never
	}
	mode, _ := ui.ParseMode(colorMode)
	return mode
}

func commandPalette(cmd *cobra.Command, out io.Writer) ui.Palette {
	configured := true
	if effectiveConfig != nil {
		configured = effectiveConfig.General.Color
	}
	return ui.New(out, commandColorMode(cmd), configured)
}

// FormatError highlights diagnostics for stderr while preserving redacted text.
func FormatError(err error, out io.Writer) string {
	palette := commandPalette(rootCmd, out)
	lines := strings.Split(redact.Text(err.Error()), "\n")
	lines[0] = palette.Text(ui.Error, "Error: "+lines[0])
	for i := 1; i < len(lines); i++ {
		for label, tone := range map[string]ui.Tone{"Relevant error:": ui.Error, "Hint:": ui.Warning, "Full build log:": ui.Info, "Full build log unavailable": ui.Warning} {
			if rest, ok := strings.CutPrefix(lines[i], label); ok {
				lines[i] = palette.Text(tone, label) + rest
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
