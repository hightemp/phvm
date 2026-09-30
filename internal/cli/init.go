package cli

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/shell"
)

var initCmd = &cobra.Command{
	Use:   "init <shell>",
	Short: "Print shell initialization script",
	Long: `Print the shell initialization script to be added to your profile.

Supported shells: bash, zsh, fish, powershell (pwsh)

The script uses the effective PHVM_DIR. An explicit --phvm-dir overrides an
existing PHVM_DIR when the script is loaded; otherwise it is preserved.
Loading the script repeatedly does not duplicate PATH entries.

Usage:
  # For bash, add to ~/.bashrc:
  eval "$(phvm init bash)"

  # For zsh, add to ~/.zshrc:
  eval "$(phvm init zsh)"

  # For fish, add to ~/.config/fish/config.fish:
  phvm init fish | source

  # For PowerShell, add to $PROFILE:
  Invoke-Expression (phvm init powershell | Out-String)`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell", "pwsh"},
	RunE: func(cmd *cobra.Command, args []string) error {
		shellName := args[0]
		phvmDir, err := filepath.Abs(GetPaths().Root)
		if err != nil {
			return fmt.Errorf("resolve PHVM_DIR: %w", err)
		}

		var s shell.Shell
		switch shellName {
		case "bash":
			s = shell.Bash
		case "zsh":
			s = shell.Zsh
		case "fish":
			s = shell.Fish
		case "powershell", "pwsh":
			s = shell.PowerShell
		default:
			return fmt.Errorf("unknown shell %q; supported: bash, zsh, fish, powershell", shellName)
		}

		var output bytes.Buffer
		output.WriteString(shell.GetInitScriptWithOverride(s, phvmDir, cmd.Root().PersistentFlags().Changed("phvm-dir")))
		output.WriteByte('\n')
		switch s {
		case shell.Bash:
			err = cmd.Root().GenBashCompletionV2(&output, true)
		case shell.Zsh:
			err = cmd.Root().GenZshCompletion(&output)
		case shell.Fish:
			err = cmd.Root().GenFishCompletion(&output, true)
		case shell.PowerShell:
			err = cmd.Root().GenPowerShellCompletionWithDesc(&output)
		}
		if err != nil {
			return fmt.Errorf("generate %s completion: %w", shellName, err)
		}
		_, err = io.Copy(cmd.OutOrStdout(), &output)
		return err
	},
}
