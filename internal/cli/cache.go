package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/composer"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the phvm cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the cache",
	Long: `Clear downloaded files and build artifacts from the cache.

By default, clears all cache. Use flags to clear specific parts:
  --downloads  Clear downloaded tarballs
  --sources    Clear extracted source files
  --build      Clear build directories
  --all        Clear everything (default)

Installed Composer is kept outside cache. Legacy Composer is migrated before
download cleanup; migration errors stop cleanup before files are removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()

		downloads, _ := cmd.Flags().GetBool("downloads")
		sources, _ := cmd.Flags().GetBool("sources")
		build, _ := cmd.Flags().GetBool("build")
		all, _ := cmd.Flags().GetBool("all")

		// If no specific flag, clear all
		if !downloads && !sources && !build {
			all = true
		}

		if all || downloads {
			if err := composer.NewManager(paths).MigrateLegacy(cmd.Context()); err != nil {
				return fmt.Errorf("migrate composer before clearing cache: %w", redact.Error(err, ""))
			}
		}
		root, err := paths.OpenDataDir(paths.Cache, true)
		if err != nil {
			return redact.Error(err, "")
		}
		defer root.Close()
		for _, part := range []struct {
			name  string
			clear bool
		}{
			{"downloads", all || downloads}, {"sources", all || sources}, {"build", all || build},
		} {
			if !part.clear {
				continue
			}
			log.Info("Clearing %s...", part.name)
			if err := root.RemoveAll(part.name); err != nil {
				return fmt.Errorf("clear %s cache: %w", part.name, redact.Error(err, ""))
			}
			if err := root.Mkdir(part.name, 0755); err != nil {
				return fmt.Errorf("recreate %s cache: %w", part.name, redact.Error(err, ""))
			}
		}

		log.Success("Cache cleared")
		return nil
	},
}

var cacheShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show cache location",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		log.Print("Cache directory: %s", paths.Cache)
		log.Print("  Downloads:     %s", paths.Downloads)
		log.Print("  Sources:       %s", paths.Sources)
		log.Print("  Build:         %s", paths.Build)
		log.Print("  Extensions:    %s", paths.Extensions)
	},
}

func init() {
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheShowCmd)

	cacheClearCmd.Flags().Bool("downloads", false, "Clear only downloads")
	cacheClearCmd.Flags().Bool("sources", false, "Clear only sources")
	cacheClearCmd.Flags().Bool("build", false, "Clear only build files")
	cacheClearCmd.Flags().Bool("all", false, "Clear everything")
}
