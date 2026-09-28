package cli

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/build"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/redact"
)

var fileConfig, effectiveConfig *core.Config

var configCmd = &cobra.Command{Use: "config", Short: "Inspect and validate phvm configuration"}
var configShowCmd = &cobra.Command{
	Use: "show", Short: "Show validated TOML settings; --effective includes env and CLI overrides",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		effective, _ := cmd.Flags().GetBool("effective")
		cfg := fileConfig
		if effective {
			cfg = effectiveConfig
		}
		public := cfg.Clone()
		public.Remote.Mirror = redact.URL(public.Remote.Mirror)
		data, err := toml.Marshal(public)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), redact.Text(string(data)))
		return err
	},
}
var configValidateCmd = &cobra.Command{
	Use: "validate", Short: "Validate TOML, environment and explicit CLI settings",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Configuration is valid: %s\n", redact.Text(GetPaths().ConfigFile()))
		return err
	},
}

func init() {
	configCmd.AddCommand(configShowCmd, configValidateCmd)
	configShowCmd.Flags().Bool("effective", false, "Include environment and CLI overrides")
	for _, cmd := range []*cobra.Command{configShowCmd, configValidateCmd} {
		cmd.Flags().String("profile", "common", "Default PHP build profile")
		cmd.Flags().Int("jobs", 0, "Parallel build jobs (0: automatic)")
		cmd.Flags().String("configure", "", "Additional PHP configure arguments")
	}
}

func loadCommandConfig(cmd *cobra.Command) error {
	var err error
	fileConfig, err = core.NewConfigManager(GetPaths()).Load()
	if err != nil {
		return fmt.Errorf("load config: %w", redact.Error(err, ""))
	}
	effectiveConfig = fileConfig.Clone()
	showEffective, _ := cmd.Flags().GetBool("effective")
	if cmd == configShowCmd && !showEffective {
		return nil
	}
	if err := effectiveConfig.ApplyEnvironment(); err != nil {
		return err
	}
	return applyConfigFlags(cmd, effectiveConfig)
}

func applyConfigFlags(cmd *cobra.Command, cfg *core.Config) error {
	for name, target := range map[string]*string{"profile": &cfg.General.DefaultProfile, "mirror": &cfg.Remote.Mirror, "user-agent": &cfg.Remote.UserAgent} {
		if cmd.Flags().Changed(name) {
			value, err := cmd.Flags().GetString(name)
			if err != nil {
				return err
			}
			*target = value
		}
	}
	for name, target := range map[string]*int{"jobs": &cfg.General.ParallelJobs, "timeout": &cfg.Remote.Timeout, "retries": &cfg.Remote.Retries} {
		if cmd.Flags().Changed(name) {
			value, err := cmd.Flags().GetInt(name)
			if err != nil {
				return err
			}
			*target = value
		}
	}
	for name, target := range map[string]*bool{"gpg": &cfg.Verify.GPG, "gpg-fallback-sha256": &cfg.Verify.GPGFallbackSHA256} {
		if cmd.Flags().Changed(name) {
			value, err := cmd.Flags().GetBool(name)
			if err != nil {
				return err
			}
			*target = value
		}
	}
	if cmd.Flags().Changed("no-color") {
		value, err := cmd.Flags().GetBool("no-color")
		if err != nil {
			return err
		}
		cfg.General.Color = !value
	}
	if cmd.Flags().Changed("skip-gpg") || cmd.Flags().Changed("skip-verify") {
		if cmd.Flags().Changed("gpg") {
			return fmt.Errorf("use either --gpg or --skip-gpg")
		}
		if cmd.Flags().Changed("skip-gpg") && cmd.Flags().Changed("skip-verify") {
			return fmt.Errorf("use either --skip-gpg or its deprecated alias --skip-verify")
		}
		name := "skip-gpg"
		if cmd.Flags().Changed("skip-verify") {
			name = "skip-verify"
		}
		value, err := cmd.Flags().GetBool(name)
		if err != nil {
			return err
		}
		cfg.Verify.GPG = !value
	}
	if cmd.Flags().Changed("configure") && (cmd == installCmd || cmd == configShowCmd || cmd == configValidateCmd) {
		value, err := cmd.Flags().GetString("configure")
		if err != nil {
			return err
		}
		cfg.Build.DefaultFlags = build.MergeFlags(&build.Profile{Flags: cfg.Build.DefaultFlags}, strings.Fields(value))
	}
	return cfg.Validate()
}
