package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		phpVersion, _ := cmd.Flags().GetString("php")

		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		extensions, err := ext.ListInstalledContext(cmd.Context(), paths, phpVersion)
		if err != nil {
			return fmt.Errorf("failed to list extensions: %w", err)
		}

		if len(extensions) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No extensions installed")
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Extensions for PHP %s:\n\n", phpVersion)
		for _, e := range extensions {
			status := e.State
			version := ""
			if e.Version != "" {
				version = " (" + e.Version + ")"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s%s", status, e.Name, version)
			if e.Module != "" && e.Module != e.Name {
				fmt.Fprintf(cmd.OutOrStdout(), " (module: %s)", e.Module)
			}
			if e.Problem != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " - %s", e.Problem)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return nil
	},
}

var extListRemoteCmd = &cobra.Command{
	Use:   "list-remote",
	Short: "List available PECL extensions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			return fmt.Errorf("failed to list packages: %w", err)
		}

		if limit > 0 && len(packages) > limit {
			packages = packages[:limit]
		}

		for _, pkg := range packages {
			fmt.Fprintln(cmd.OutOrStdout(), pkg)
		}

		if limit > 0 && len(packages) == limit {
			fmt.Fprintf(cmd.OutOrStdout(), "\n(showing first %d results)\n", limit)
		}
		return nil
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
	RunE: func(cmd *cobra.Command, args []string) error {
		extName := args[0]
		paths := GetPaths()

		phpVersion, _ := cmd.Flags().GetString("php")
		extVersion, _ := cmd.Flags().GetString("version")
		jobs := effectiveConfig.General.ParallelJobs
		sha256sum, _ := cmd.Flags().GetString("sha256")

		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		client := getClient(paths)
		installer := ext.NewInstaller(paths, client)

		customFlags := append([]string{}, configureCLIFlags...)

		opts := ext.InstallOptions{
			Name:        extName,
			Version:     extVersion,
			PHPVersion:  phpVersion,
			CustomFlags: customFlags,
			Jobs:        jobs,
			SHA256:      sha256sum,
		}

		if err := installer.Install(cmd.Context(), opts); err != nil {
			return fmt.Errorf("failed to install extension: %w", err)
		}
		return nil
	},
}

var extUninstallCmd = &cobra.Command{
	Use:   "uninstall <extension>",
	Short: "Uninstall an extension",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extName := args[0]
		paths := GetPaths()

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		client := getClient(paths)
		installer := ext.NewInstaller(paths, client)

		if err := installer.Uninstall(cmd.Context(), extName, phpVersion); err != nil {
			return fmt.Errorf("failed to uninstall extension: %w", err)
		}
		return nil
	},
}

var extEnableCmd = &cobra.Command{
	Use:   "enable <extension>",
	Short: "Enable an extension",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extName := args[0]
		paths := GetPaths()

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		if err := ext.EnableContext(cmd.Context(), paths, phpVersion, extName); err != nil {
			return fmt.Errorf("failed to enable extension: %w", err)
		}

		log.Success("Enabled %s", extName)
		return nil
	},
}

var extDisableCmd = &cobra.Command{
	Use:   "disable <extension>",
	Short: "Disable an extension",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extName := args[0]
		paths := GetPaths()

		phpVersion, _ := cmd.Flags().GetString("php")
		var resolveErr error
		phpVersion, resolveErr = resolvePHPVersion(paths, phpVersion)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve installed PHP: %w", resolveErr)
		}

		if err := ext.DisableContext(cmd.Context(), paths, phpVersion, extName); err != nil {
			return fmt.Errorf("failed to disable extension: %w", err)
		}

		log.Success("Disabled %s", extName)
		return nil
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
		c.Flags().String("php", "", "Installed PHP version or alias (default: current)")
	}

	extListRemoteCmd.Flags().String("search", "", "Search for extensions")
	extListRemoteCmd.Flags().Int("limit", 50, "Maximum results to show")

	extInstallCmd.Flags().String("version", "", "Extension version (default: latest)")
	extInstallCmd.Flags().String("configure", "", "Additional configure flags")
	extInstallCmd.Flags().StringArray("configure-flag", nil, "One exact extension configure argument (repeatable; overrides --configure)")
	extInstallCmd.Flags().Int("jobs", 0, "Number of parallel build jobs (0: extension builder default)")
	extInstallCmd.Flags().String("sha256", "", "Pin the PECL archive SHA256; verifies cached bytes without channel metadata")
}
