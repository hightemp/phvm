package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/spf13/cobra"
)

var useCmd = &cobra.Command{
	Use:   "use <version|alias>",
	Short: "Switch to a PHP version",
	Long: `Switch the current PHP version.

The version can be a full version number (8.3.30), a partial version (8.3),
or an alias (default, stable).

Examples:
  phvm use 8.3.30
  phvm use 8.3
  phvm use default`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		versionArg := args[0]
		paths := GetPaths()

		installed := core.NewInstalledManager(paths)
		aliases := core.NewAliasManager(paths)
		current := core.NewCurrentManager(paths)

		// Try to resolve as alias
		version, _, err := aliases.ResolveVersionOrAlias(versionArg)
		if err != nil {
			// Try as a version pattern
			version, err = installed.GetLatestInstalled(versionArg)
			if err != nil {
				log.Error("Version or alias not found: %s", versionArg)
				os.Exit(1)
			}
		}

		// Check if installed
		if !installed.IsInstalled(version) {
			log.Error("PHP %s is not installed", version)
			log.Print("Run 'phvm install %s' to install it", version)
			os.Exit(1)
		}

		// Switch
		if err := current.Set(version); err != nil {
			log.Error("Failed to switch version: %v", err)
			os.Exit(1)
		}

		log.Success("Now using PHP %s", version)
	},
}

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current PHP version",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		showPath, _ := cmd.Flags().GetBool("path")

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			log.Print("Run 'phvm use <version>' to set a version")
			os.Exit(1)
		}

		if showPath {
			path, _ := current.GetPath()
			fmt.Println(path)
		} else {
			fmt.Println(version)
		}
	},
}

func init() {
	currentCmd.Flags().Bool("path", false, "Show the path instead of version")
}

var whichCmd = &cobra.Command{
	Use:   "which [binary]",
	Short: "Show path to a PHP binary",
	Long: `Show the full path to a PHP binary in the current version.

Examples:
  phvm which           # Shows path to php
  phvm which php
  phvm which phpize
  phvm which php-config`,
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		binary := "php"
		if len(args) > 0 {
			binary = args[0]
		}

		binPath, err := current.GetBinPath()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		fullPath := filepath.Join(binPath, binary)
		fmt.Println(fullPath)
	},
}
