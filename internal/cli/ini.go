package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/ini"
	"github.com/hightemp/phvm/internal/log"
)

var iniCmd = &cobra.Command{
	Use:   "ini",
	Short: "Manage PHP configuration",
	Long: `Manage php.ini and conf.d configuration files.

Subcommands:
  path     - Show configuration paths
  open     - Open php.ini in editor
  list     - List conf.d files
  enable   - Enable a conf.d file
  disable  - Disable a conf.d file
  profile  - Manage ini profiles`,
}

var iniPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show PHP configuration paths",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		pathInfo := ini.GetPaths(paths, version)
		fmt.Printf("php.ini:  %s\n", pathInfo.PHPIniPath)
		fmt.Printf("conf.d:   %s\n", pathInfo.ScanDir)
	},
}

var iniOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Open php.ini in editor",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		pathInfo := ini.GetPaths(paths, version)

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			// Try common editors
			for _, e := range []string{"nano", "vim", "vi", "code", "notepad"} {
				if _, err := exec.LookPath(e); err == nil {
					editor = e
					break
				}
			}
		}

		if editor == "" {
			fmt.Printf("php.ini path: %s\n", pathInfo.PHPIniPath)
			log.Warn("No editor found. Set EDITOR environment variable.")
			return
		}

		editorCmd := exec.Command(editor, pathInfo.PHPIniPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			log.Error("Failed to open editor: %v", err)
		}
	},
}

var iniListCmd = &cobra.Command{
	Use:   "list",
	Short: "List conf.d files",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		confD := paths.VersionConfD(version)
		mgr := ini.NewConfDManager(confD)

		files, err := mgr.List()
		if err != nil {
			log.Error("Failed to list files: %v", err)
			os.Exit(1)
		}

		if len(files) == 0 {
			log.Print("No conf.d files")
			return
		}

		for _, f := range files {
			status := "enabled"
			if !f.Enabled {
				status = "disabled"
			}
			fmt.Printf("[%s] %s\n", status, f.Name)
		}
	},
}

var iniEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a conf.d file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		confD := paths.VersionConfD(version)
		mgr := ini.NewConfDManager(confD)

		if err := mgr.Enable(name); err != nil {
			log.Error("Failed to enable: %v", err)
			os.Exit(1)
		}

		log.Success("Enabled %s", name)
	},
}

var iniDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a conf.d file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			log.Error("No current version set")
			os.Exit(1)
		}

		confD := paths.VersionConfD(version)
		mgr := ini.NewConfDManager(confD)

		if err := mgr.Disable(name); err != nil {
			log.Error("Failed to disable: %v", err)
			os.Exit(1)
		}

		log.Success("Disabled %s", name)
	},
}

var iniProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage ini profiles",
}

var iniProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles",
	Run: func(cmd *cobra.Command, args []string) {
		paths := GetPaths()
		mgr := ini.NewProfileManager(paths)

		profiles, err := mgr.List()
		if err != nil {
			log.Error("Failed to list profiles: %v", err)
			os.Exit(1)
		}

		if len(profiles) == 0 {
			log.Print("No profiles defined")
			log.Print("Run 'phvm ini profile save <name>' to save current config as a profile")
			return
		}

		for _, p := range profiles {
			fmt.Printf("%s\n", p.Name)
		}
	},
}

var iniProfileUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Apply a profile to current version",
	Long: `Apply a profile to current PHP. Saved profiles replace php.ini and all conf.d
files with their snapshot; absent files are removed. Development/production
defaults change only php.ini. Candidate PHP startup is checked before and after
publication. Errors restore the previous configuration. Backups use unique names;
--backup-keep limits retained managed backup history (default 5).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			return fmt.Errorf("resolve current PHP: %w", err)
		}

		mgr := ini.NewProfileManager(paths)
		backup, _ := cmd.Flags().GetBool("backup")
		keep, _ := cmd.Flags().GetInt("backup-keep")
		if err := mgr.SetBackupLimit(keep); err != nil {
			return err
		}
		if name == "development" || name == "production" {
			if err := mgr.CreateDefaultProfilesContext(cmd.Context()); err != nil {
				return err
			}
		}

		if err := mgr.ApplyContext(cmd.Context(), name, version, backup); err != nil {
			return fmt.Errorf("apply ini profile: %w", err)
		}

		log.Success("Applied profile '%s' to PHP %s", name, version)
		return nil
	},
}

var iniProfileSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save current config as a profile",
	Long: `Save an exact snapshot of current PHP's php.ini and conf.d. Saving the same
name replaces the complete saved snapshot, removing stale files.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		paths := GetPaths()
		current := core.NewCurrentManager(paths)

		version, err := current.Get()
		if err != nil {
			return fmt.Errorf("resolve current PHP: %w", err)
		}

		mgr := ini.NewProfileManager(paths)

		if err := mgr.SaveContext(cmd.Context(), name, version); err != nil {
			return fmt.Errorf("save ini profile: %w", err)
		}

		log.Success("Saved profile '%s'", name)
		return nil
	},
}

func init() {
	iniCmd.AddCommand(iniPathCmd)
	iniCmd.AddCommand(iniOpenCmd)
	iniCmd.AddCommand(iniListCmd)
	iniCmd.AddCommand(iniEnableCmd)
	iniCmd.AddCommand(iniDisableCmd)
	iniCmd.AddCommand(iniProfileCmd)

	iniProfileCmd.AddCommand(iniProfileListCmd)
	iniProfileCmd.AddCommand(iniProfileUseCmd)
	iniProfileCmd.AddCommand(iniProfileSaveCmd)

	iniProfileUseCmd.Flags().Bool("backup", true, "Backup existing config before applying")
	iniProfileUseCmd.Flags().Int("backup-keep", 5, "Keep the most recent managed ini backups (1-100)")
}
