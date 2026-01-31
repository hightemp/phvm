package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/log"
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
  --all        Clear everything (default)`,
	Run: func(cmd *cobra.Command, args []string) {
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
			log.Info("Clearing downloads...")
			if err := os.RemoveAll(paths.Downloads); err != nil {
				log.Warn("Failed to clear downloads: %v", err)
			}
			_ = os.MkdirAll(paths.Downloads, 0755)
		}

		if all || sources {
			log.Info("Clearing sources...")
			if err := os.RemoveAll(paths.Sources); err != nil {
				log.Warn("Failed to clear sources: %v", err)
			}
			_ = os.MkdirAll(paths.Sources, 0755)
		}

		if all || build {
			log.Info("Clearing build files...")
			if err := os.RemoveAll(paths.Build); err != nil {
				log.Warn("Failed to clear build files: %v", err)
			}
			_ = os.MkdirAll(paths.Build, 0755)
		}

		log.Success("Cache cleared")
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
