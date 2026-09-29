package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/composer"
	"github.com/hightemp/phvm/internal/fsutil"
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
			{"downloads", all || downloads}, {"sources", all || sources}, {"build", all || build}, {"extensions", all},
		} {
			if !part.clear {
				continue
			}
			// Validate the managed path before creating any sibling lock files.
			partRoot, err := paths.OpenDataDir(filepath.Join(paths.Cache, part.name), false)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("open %s cache: %w", part.name, err)
			}
			if partRoot != nil {
				_ = partRoot.Close()
			}
			log.Info("Clearing %s...", part.name)
			if err := fsutil.WithDirectoryLock(cmd.Context(), filepath.Join(paths.Cache, part.name), func() error {
				if err := root.RemoveAll(part.name); err != nil {
					return err
				}
				return root.Mkdir(part.name, 0755)
			}); err != nil {
				return fmt.Errorf("clear %s cache: %w", part.name, redact.Error(err, ""))
			}
		}

		log.Success("Cache cleared")
		return nil
	},
}

var cacheShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show cache location",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		fmt.Fprintf(cmd.OutOrStdout(), "Cache directory: %s\n", paths.Cache)
		fmt.Fprintf(cmd.OutOrStdout(), "  Downloads:     %s\n", paths.Downloads)
		fmt.Fprintf(cmd.OutOrStdout(), "  Sources:       %s\n", paths.Sources)
		fmt.Fprintf(cmd.OutOrStdout(), "  Build:         %s\n", paths.Build)
		fmt.Fprintf(cmd.OutOrStdout(), "  Extensions:    %s\n", paths.Extensions)
		return nil
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
