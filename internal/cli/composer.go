package cli

import (
	"os"

	"github.com/hightemp/phvm/internal/composer"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/spf13/cobra"
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
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		global, _ := cmd.Flags().GetBool("global")
		phpVersion, _ := cmd.Flags().GetString("php")

		if global {
			if err := mgr.InstallGlobal(cmd.Context()); err != nil {
				log.Error("Failed to install Composer: %v", err)
				os.Exit(1)
			}
		} else {
			if phpVersion == "" {
				current := core.NewCurrentManager(paths)
				version, err := current.Get()
				if err != nil {
					log.Error("No current version set")
					os.Exit(1)
				}
				phpVersion = version
			}

			if err := mgr.Install(cmd.Context(), phpVersion); err != nil {
				log.Error("Failed to install Composer: %v", err)
				os.Exit(1)
			}
		}
	},
}

var composerUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Composer",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		if phpVersion == "" {
			current := core.NewCurrentManager(paths)
			version, err := current.Get()
			if err != nil {
				log.Error("No current version set")
				os.Exit(1)
			}
			phpVersion = version
		}

		if err := mgr.Update(cmd.Context(), phpVersion); err != nil {
			log.Error("Failed to update Composer: %v", err)
			os.Exit(1)
		}
	},
}

var composerEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable Composer for current PHP version",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		if phpVersion == "" {
			current := core.NewCurrentManager(paths)
			version, err := current.Get()
			if err != nil {
				log.Error("No current version set")
				os.Exit(1)
			}
			phpVersion = version
		}

		if err := mgr.Enable(phpVersion); err != nil {
			log.Error("Failed to enable Composer: %v", err)
			os.Exit(1)
		}
	},
}

var composerDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable Composer for current PHP version",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		mgr := composer.NewManager(paths)

		phpVersion, _ := cmd.Flags().GetString("php")
		if phpVersion == "" {
			current := core.NewCurrentManager(paths)
			version, err := current.Get()
			if err != nil {
				log.Error("No current version set")
				os.Exit(1)
			}
			phpVersion = version
		}

		if err := mgr.Disable(phpVersion); err != nil {
			log.Error("Failed to disable Composer: %v", err)
			os.Exit(1)
		}
	},
}

func init() {
	composerCmd.AddCommand(composerInstallCmd)
	composerCmd.AddCommand(composerUpdateCmd)
	composerCmd.AddCommand(composerEnableCmd)
	composerCmd.AddCommand(composerDisableCmd)

	composerInstallCmd.Flags().Bool("global", false, "Install globally (shared by all versions)")
	composerInstallCmd.Flags().String("php", "", "PHP version (default: current)")

	composerUpdateCmd.Flags().String("php", "", "PHP version (default: current)")
	composerEnableCmd.Flags().String("php", "", "PHP version (default: current)")
	composerDisableCmd.Flags().String("php", "", "PHP version (default: current)")
}
