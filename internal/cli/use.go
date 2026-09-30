package cli

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

var useCmd = &cobra.Command{
	Use:   "use [version|alias]",
	Short: "Switch to a PHP version",
	Long: `Switch the current PHP version.

Without an argument, read the nearest .php-version from the current directory
or one of its parents. The file contains one version or local alias using the
same syntax as an explicit argument. An explicit argument ignores the file.

The version can be a full version number (8.3.30), a partial version (8.3),
or a local alias (default, prod). Partial versions select the newest matching
installed release. The v and php- prefixes are accepted.

Local aliases take precedence. Without a local alias, latest and stable select
the newest installed release, without network access. Define lts explicitly
as a local alias if needed. Alias cycles and missing versions are errors.

Examples:
  phvm use
  phvm use 8.3.30
  phvm use 8.3
  phvm use default
  phvm use latest`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeInstalledPHP,
	RunE: func(cmd *cobra.Command, args []string) error {
		var versionArg, projectFile string
		if len(args) == 1 {
			versionArg = args[0]
		} else {
			var err error
			versionArg, projectFile, err = findProjectPHPVersion()
			if err != nil {
				return err
			}
		}
		paths := GetPaths()

		installed := core.NewInstalledManager(paths)
		current := core.NewCurrentManager(paths)

		version, err := installed.Resolve(versionArg)
		if err != nil {
			resolutionErr := redact.Error(err, versionArg)
			if projectFile != "" {
				return fmt.Errorf("resolve %s: %w", projectFile, resolutionErr)
			}
			return resolutionErr
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
	Use: "current", Short: "Show the current PHP version", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		current := core.NewCurrentManager(paths)
		version, err := current.Get()
		if err != nil {
			return fmt.Errorf("resolve current PHP: %w; run phvm use <version>", err)
		}
		if _, err := installedBinaryPath(paths, version, core.PHPBinary()); err != nil {
			return err
		}
		showPath, _ := cmd.Flags().GetBool("path")
		if showPath {
			path, err := current.GetPath()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), version)
		return err
	},
}

func init() { currentCmd.Flags().Bool("path", false, "Show the path instead of version") }

var whichCmd = &cobra.Command{
	Use: "which [binary]", Short: "Show path to an existing executable in current PHP", Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := GetPaths()
		version, err := core.NewCurrentManager(paths).Get()
		if err != nil {
			return fmt.Errorf("resolve current PHP: %w", err)
		}
		binary := core.PHPBinary()
		if len(args) > 0 {
			binary = args[0]
			if binary == "php" {
				binary = core.PHPBinary()
			}
		}
		path, err := installedBinaryPath(paths, version, binary)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
		return err
	},
}

func installedBinaryPath(paths *core.Paths, version, binary string) (string, error) {
	if err := fsutil.ValidateName(binary); err != nil {
		return "", err
	}
	root, err := paths.OpenVersion(version, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.Join("bin", binary))
	if err != nil {
		return "", fmt.Errorf("binary %s is unavailable: %w", binary, err)
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("binary %s is not a regular executable", binary)
	}
	return filepath.Abs(filepath.Join(paths.VersionBin(version), binary))
}
