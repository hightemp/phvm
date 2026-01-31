package cli

import (
	"fmt"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/shell"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <shell>",
	Short: "Print shell initialization script",
	Long: `Print the shell initialization script to be added to your profile.

Supported shells: bash, zsh, fish, powershell

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
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Run: func(cmd *cobra.Command, args []string) {
		shellName := args[0]
		phvmDir := core.DefaultRoot()

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
			fmt.Printf("Unknown shell: %s\n", shellName)
			fmt.Println("Supported shells: bash, zsh, fish, powershell")
			return
		}

		script := shell.GetInitScript(s, phvmDir)
		fmt.Print(script)
	},
}
