package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

var useCmd = &cobra.Command{
	Use:   "use <version|alias>",
	Short: "Switch to a PHP version",
	Long: `Switch the current PHP version.

The version can be a full version number (8.3.30), a partial version (8.3),
or a local alias (default, prod). Partial versions select the newest matching
installed release. The v and php- prefixes are accepted.

Local aliases take precedence. Without a local alias, latest and stable select
the newest installed release, without network access. Define lts explicitly
as a local alias if needed. Alias cycles and missing versions are errors.

Examples:
  phvm use 8.3.30
  phvm use 8.3
  phvm use default
  phvm use latest`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		versionArg := args[0]
		paths := GetPaths()

		installed := core.NewInstalledManager(paths)
		current := core.NewCurrentManager(paths)

		version, err := installed.Resolve(versionArg)
		if err != nil {
			return redact.Error(err, versionArg)
		}

		// Switch
		if err := current.Set(version); err != nil {
			return fmt.Errorf("switch PHP version: %w", err)
		}

		log.Success("Now using PHP %s", version)
		return nil
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
