package cli

import (
	"fmt"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List installed PHP versions",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		installed := core.NewInstalledManager(paths)
		current := core.NewCurrentManager(paths)
		aliases := core.NewAliasManager(paths)

		versions, err := installed.List()
		if err != nil {
			log.Error("Failed to list versions: %v", err)
			return
		}

		if len(versions) == 0 {
			log.Print("No PHP versions installed")
			log.Print("Run 'phvm install <version>' to install a version")
			return
		}

		currentVer, _ := current.Get()
		defaultVer, _ := aliases.GetDefault()

		log.Default().PrintVersions(versions, currentVer, defaultVer)
	},
}

var lsRemoteCmd = &cobra.Command{
	Use:   "ls-remote",
	Short: "List available PHP versions",
	Long: `List PHP versions available for installation from php.net.

By default, shows the latest version for each supported branch.
Use --all to attempt to fetch more versions (may be slow).`,
	Run: func(cmd *cobra.Command, args []string) {
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")

		paths := GetPaths()
		client := getClient(paths)
		api := getAPI(client)

		if all {
			log.Info("Fetching all available versions...")
			versions, err := api.GetAvailableVersions(cmd.Context(), limit)
			if err != nil {
				log.Error("Failed to fetch versions: %v", err)
				return
			}

			for _, v := range versions {
				fmt.Println(v)
			}
		} else {
			log.Info("Fetching latest versions...")
			releases, err := api.GetLatestReleases(cmd.Context())
			if err != nil {
				log.Error("Failed to fetch releases: %v", err)
				return
			}

			// Sort by major version
			var majors []string
			for m := range releases {
				majors = append(majors, m)
			}
			core.SortVersions(majors)

			for _, major := range majors {
				release := releases[major]
				fmt.Printf("PHP %s: %s\n", major, release.Version)
			}

			// Show supported versions
			supported, _ := api.GetSupportedVersions(cmd.Context())
			if len(supported) > 0 {
				fmt.Println("\nSupported branches:", supported)
			}
		}
	},
}

func init() {
	lsRemoteCmd.Flags().Bool("all", false, "Fetch all available versions")
	lsRemoteCmd.Flags().Int("limit", 10, "Maximum versions per major version")
}
