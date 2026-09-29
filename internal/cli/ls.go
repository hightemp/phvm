package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List installed PHP versions",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		installed := core.NewInstalledManager(paths)
		current := core.NewCurrentManager(paths)
		aliases := core.NewAliasManager(paths)

		versions, err := installed.List()
		if err != nil {
			return fmt.Errorf("list installed PHP versions: %w", err)
		}

		if len(versions) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No PHP versions installed")
			fmt.Fprintln(cmd.OutOrStdout(), "Run 'phvm install <version>' to install a version")
			return nil
		}

		currentVer, err := current.Get()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read current PHP: %w", err)
		}
		defaultVer, err := aliases.GetDefault()
		if err != nil && !errors.Is(err, core.ErrAliasNotFound) {
			return fmt.Errorf("read default alias: %w", err)
		}

		logger := log.New(cmd.OutOrStdout(), log.LevelNormal)
		logger.SetNoColor(!effectiveConfig.General.Color)
		logger.PrintVersions(versions, currentVer, defaultVer)
		return nil
	},
}

var lsRemoteCmd = &cobra.Command{
	Use:   "ls-remote",
	Short: "List available PHP versions",
	Long: `List PHP versions available for installation from php.net.

By default, shows the latest version for each supported branch.
Use --all to attempt to fetch more versions (may be slow).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")

		paths := GetPaths()
		client := getClient(paths)
		api := getAPI(client)

		if all {
			log.Info("Fetching all available versions...")
			versions, err := api.GetAvailableVersions(cmd.Context(), limit)
			if err != nil {
				return fmt.Errorf("fetch PHP versions: %w", err)
			}

			for _, v := range versions {
				fmt.Fprintln(cmd.OutOrStdout(), v)
			}
		} else {
			log.Info("Fetching latest versions...")
			releases, err := api.GetLatestReleases(cmd.Context())
			if err != nil {
				return fmt.Errorf("fetch PHP releases: %w", err)
			}

			// Sort by major version
			var majors []string
			for m := range releases {
				majors = append(majors, m)
			}
			core.SortVersions(majors)

			for _, major := range majors {
				release := releases[major]
				fmt.Fprintf(cmd.OutOrStdout(), "PHP %s: %s\n", major, release.Version)
			}

			// Show supported versions
			supported, err := api.GetSupportedVersions(cmd.Context())
			if err != nil {
				return fmt.Errorf("fetch supported PHP branches: %w", err)
			}
			if len(supported) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nSupported branches:", supported)
			}
		}
		return nil
	},
}

func init() {
	lsRemoteCmd.Flags().Bool("all", false, "Fetch all available versions")
	lsRemoteCmd.Flags().Int("limit", 10, "Maximum versions per major version")
}
