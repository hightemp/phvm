package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/doctor"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system dependencies for building PHP",
	Long: `Check tools and linkable libraries for the selected PHP build.

By default, doctor checks the current PHP selected in PHVM_DIR.
--php accepts current, an uninstalled X.Y branch or X.Y.Z release without a download.
If current is missing or unsafe, select a version with phvm use or pass --php.
The profile and configure flags follow CLI/env/TOML settings.
Required failures return a nonzero exit code. Private libraries that phvm will
build for older PHP are reported as DEFERRED and checked by PHP configure later.
The report shows the selected compiler, linker, pkg-config and build flags.
Compiled probe programs are never run.`,
	Example: "  phvm doctor\n  phvm doctor --php current --profile common\n  phvm doctor --php 8.5.11 --profile common\n  phvm doctor --php 8.5 --profile minimal",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		version, _ := cmd.Flags().GetString("php")
		result, err := doctor.CheckFor(cmd.Context(), doctor.Options{PHPVersion: version, Profile: effectiveConfig.General.DefaultProfile, ConfigFlags: configureBaseFlags, CustomFlags: configureCLIFlags, Paths: GetPaths(), GPGRequired: effectiveConfig.Verify.GPG && !effectiveConfig.Verify.GPGFallbackSHA256})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), doctor.FormatResults(result)); err != nil {
			return err
		}

		if !result.AllOK {
			return fmt.Errorf("build requirements check failed")
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().String("php", "current", "Target PHP version: current, X.Y or X.Y.Z (no download)")
	doctorCmd.Flags().String("profile", "common", "Build profile (minimal, common, full)")
	doctorCmd.Flags().String("configure", "", "Additional PHP configure flags")
	doctorCmd.Flags().StringArray("configure-flag", nil, "One exact configure argument (repeatable; overrides --configure)")
}
