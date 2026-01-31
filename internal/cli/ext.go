package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/ext"
	"github.com/hightemp/phvm/internal/log"
)

var extCmd = &cobra.Command{
	Use:   "ext",
	Short: "Manage PHP extensions",
	Long: `Manage PHP extensions from PECL.

Subcommands:
  list        - List installed extensions
  list-remote - List available PECL extensions
  install     - Install an extension
  uninstall   - Uninstall an extension
  enable      - Enable an extension
  disable     - Disable an extension`,
}

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed extensions",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
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

		extensions, err := ext.ListInstalled(paths, phpVersion)
		if err != nil {
			log.Error("Failed to list extensions: %v", err)
			os.Exit(1)
		}

		if len(extensions) == 0 {
			log.Print("No extensions installed")
			return
		}

		fmt.Printf("Extensions for PHP %s:\n\n", phpVersion)
		for _, e := range extensions {
			status := "enabled"
			if !e.Enabled {
				status = "disabled"
			}
			version := ""
			if e.Version != "" {
				version = " (" + e.Version + ")"
			}
			fmt.Printf("[%s] %s%s\n", status, e.Name, version)
		}
	},
}

var extListRemoteCmd = &cobra.Command{
	Use:   "list-remote",
	Short: "List available PECL extensions",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		client := getClient(paths)
		api := getPECLAPI(client)

		search, _ := cmd.Flags().GetString("search")
		limit, _ := cmd.Flags().GetInt("limit")

		var packages []string
		var err error

		if search != "" {
			log.Info("Searching for '%s'...", search)
			packages, err = api.SearchPackages(cmd.Context(), search)
		} else {
			log.Info("Fetching package list...")
			packages, err = api.ListPackages(cmd.Context())
		}

		if err != nil {
			log.Error("Failed to list packages: %v", err)
			os.Exit(1)
		}

		if limit > 0 && len(packages) > limit {
			packages = packages[:limit]
		}

		for _, pkg := range packages {
			fmt.Println(pkg)
		}

		if limit > 0 && len(packages) == limit {
			fmt.Printf("\n(showing first %d results)\n", limit)
		}
	},
}

var extInstallCmd = &cobra.Command{
	Use:   "install <extension>",
	Short: "Install a PECL extension",
	Long: `Install a PHP extension from PECL.

Examples:
  phvm ext install xdebug
  phvm ext install redis --version 5.3.7
  phvm ext install mongodb --php 8.3.30`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		extName := args[0]
		paths := GetPaths()

		phpVersion, _ := cmd.Flags().GetString("php")
		extVersion, _ := cmd.Flags().GetString("version")
		configure, _ := cmd.Flags().GetString("configure")
		jobs, _ := cmd.Flags().GetInt("jobs")

		if phpVersion == "" {
			current := core.NewCurrentManager(paths)
			version, err := current.Get()
			if err != nil {
				log.Error("No current version set")
				os.Exit(1)
			}
			phpVersion = version
		}

		client := getClient(paths)
		installer := ext.NewInstaller(paths, client)

		var customFlags []string
		if configure != "" {
			customFlags = []string{configure}
		}

		opts := ext.InstallOptions{
			Name:        extName,
			Version:     extVersion,
			PHPVersion:  phpVersion,
			CustomFlags: customFlags,
			Jobs:        jobs,
		}

		if err := installer.Install(cmd.Context(), opts); err != nil {
			log.Error("Failed to install extension: %v", err)
			os.Exit(1)
		}
	},
}

var extUninstallCmd = &cobra.Command{
	Use:   "uninstall <extension>",
	Short: "Uninstall an extension",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		extName := args[0]
		paths := GetPaths()

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

		client := getClient(paths)
		installer := ext.NewInstaller(paths, client)

		if err := installer.Uninstall(cmd.Context(), extName, phpVersion); err != nil {
			log.Error("Failed to uninstall extension: %v", err)
			os.Exit(1)
		}
	},
}

var extEnableCmd = &cobra.Command{
	Use:   "enable <extension>",
	Short: "Enable an extension",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		extName := args[0]
		paths := GetPaths()

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

		if err := ext.Enable(paths, phpVersion, extName); err != nil {
			log.Error("Failed to enable extension: %v", err)
			os.Exit(1)
		}

		log.Success("Enabled %s", extName)
	},
}

var extDisableCmd = &cobra.Command{
	Use:   "disable <extension>",
	Short: "Disable an extension",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		extName := args[0]
		paths := GetPaths()

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

		if err := ext.Disable(paths, phpVersion, extName); err != nil {
			log.Error("Failed to disable extension: %v", err)
			os.Exit(1)
		}

		log.Success("Disabled %s", extName)
	},
}

func init() {
	extCmd.AddCommand(extListCmd)
	extCmd.AddCommand(extListRemoteCmd)
	extCmd.AddCommand(extInstallCmd)
	extCmd.AddCommand(extUninstallCmd)
	extCmd.AddCommand(extEnableCmd)
	extCmd.AddCommand(extDisableCmd)

	// Common flags
	for _, c := range []*cobra.Command{extListCmd, extInstallCmd, extUninstallCmd, extEnableCmd, extDisableCmd} {
		c.Flags().String("php", "", "PHP version (default: current)")
	}

	extListRemoteCmd.Flags().String("search", "", "Search for extensions")
	extListRemoteCmd.Flags().Int("limit", 50, "Maximum results to show")

	extInstallCmd.Flags().String("version", "", "Extension version (default: latest)")
	extInstallCmd.Flags().String("configure", "", "Additional configure flags")
	extInstallCmd.Flags().Int("jobs", 2, "Number of parallel build jobs")
}
