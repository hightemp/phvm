package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
)

var aliasCmd = &cobra.Command{
	Use:   "alias <name> <version>",
	Short: "Create a version alias",
	Long: `Create an alias for a PHP version.

Aliases can be used with 'phvm use' to quickly switch versions.

Examples:
  phvm alias default 8.3.30
  phvm alias prod 8.2.28
  phvm alias lts 8.1.27`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		version := args[1]
		paths := GetPaths()

		aliases := core.NewAliasManager(paths)

		if err := aliases.Set(name, version); err != nil {
			log.Error("Failed to create alias: %v", err)
			os.Exit(1)
		}

		log.Success("Alias '%s' -> '%s'", name, version)
	},
}

var unaliasCmd = &cobra.Command{
	Use:   "unalias <name>",
	Short: "Remove a version alias",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		paths := GetPaths()

		aliases := core.NewAliasManager(paths)

		if err := aliases.Delete(name); err != nil {
			log.Error("Failed to remove alias: %v", err)
			os.Exit(1)
		}

		log.Success("Removed alias '%s'", name)
	},
}

func init() {
	// Add a subcommand to list aliases
	aliasListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all aliases",
		Run: func(cmd *cobra.Command, args []string) {
			paths := GetPaths()
			aliases := core.NewAliasManager(paths)

			list, err := aliases.List()
			if err != nil {
				log.Error("Failed to list aliases: %v", err)
				os.Exit(1)
			}

			if len(list) == 0 {
				log.Print("No aliases defined")
				return
			}

			for name, version := range list {
				fmt.Printf("%s -> %s\n", name, version)
			}
		},
	}

	// Make 'alias' accept no args to list aliases
	aliasCmd.Run = func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			aliasListCmd.Run(cmd, args)
			return
		}
		if len(args) == 1 {
			// Show single alias
			name := args[0]
			paths := GetPaths()
			aliases := core.NewAliasManager(paths)

			version, err := aliases.Get(name)
			if err != nil {
				log.Error("Alias not found: %s", name)
				os.Exit(1)
			}
			fmt.Printf("%s -> %s\n", name, version)
			return
		}

		// Create alias
		name := args[0]
		version := args[1]
		paths := GetPaths()
		aliases := core.NewAliasManager(paths)

		if err := aliases.Set(name, version); err != nil {
			log.Error("Failed to create alias: %v", err)
			os.Exit(1)
		}

		log.Success("Alias '%s' -> '%s'", name, version)
	}

	aliasCmd.Args = cobra.RangeArgs(0, 2)
}
