package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/composer"
)

var composerCmd = &cobra.Command{
	Use:   "composer",
	Short: "Manage Composer",
	Long: `Manage Composer installation.

Subcommands:
  install  - Install Composer
  update   - Update Composer
  enable   - Enable Composer for a PHP version
  disable  - Disable Composer for a PHP version`,
}

var composerInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Composer",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		global, _ := cmd.Flags().GetBool("global")
		phpVersion, _ := cmd.Flags().GetString("php")

		if global {
			if err := mgr.InstallGlobal(cmd.Context()); err != nil {
				return fmt.Errorf("failed to install Composer: %w", err)
			}
		} else {
			var resolveErr error
			phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
			if resolveErr != nil {
				return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
			}

			if err := mgr.Install(cmd.Context(), phpVersion); err != nil {
				return fmt.Errorf("failed to install Composer: %w", err)
			}
		}
		return nil
	},
}

var composerUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Composer",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		if err := mgr.Update(cmd.Context(), phpVersion); err != nil {
			return fmt.Errorf("failed to update Composer: %w", err)
		}
		return nil
	},
}

var composerEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable Composer for current PHP version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		if err := mgr.EnableContext(cmd.Context(), phpVersion); err != nil {
			return fmt.Errorf("failed to enable Composer: %w", err)
		}
		return nil
	},
}

var composerDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable Composer for current PHP version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		if err := mgr.DisableContext(cmd.Context(), phpVersion); err != nil {
			return fmt.Errorf("failed to disable Composer: %w", err)
		}
		return nil
	},
}

func init() {
	composerCmd.AddCommand(composerInstallCmd)
	composerCmd.AddCommand(composerUpdateCmd)
	composerCmd.AddCommand(composerEnableCmd)
	composerCmd.AddCommand(composerDisableCmd)

	composerInstallCmd.Flags().Bool("global", false, "Install globally (shared by all versions)")
	composerInstallCmd.Flags().String("php", "", "Installed PHP version or alias (default: current)")

	composerUpdateCmd.Flags().String("php", "", "Installed PHP version or alias (default: current)")
	composerEnableCmd.Flags().String("php", "", "Installed PHP version or alias (default: current)")
	composerDisableCmd.Flags().String("php", "", "Installed PHP version or alias (default: current)")
}
