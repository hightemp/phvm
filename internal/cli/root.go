// Package cli provides the command-line interface for phvm.
package cli

import (
	"os"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time.
	Version = "dev"
	// Commit is set at build time.
	Commit = "unknown"

	// Global flags
	verbose bool
	debug   bool
	noColor bool
	phvmDir string

	// Shared instances
	paths *core.Paths
)

// rootCmd is the base command.
var rootCmd = &cobra.Command{
	Use:   "phvm",
	Short: "PHP Version Manager",
	Long: `phvm is a PHP version manager that allows you to install, manage, and switch
between multiple versions of PHP built from source.

Similar to nvm for Node.js, phvm provides an easy way to:
- Install PHP versions from source (php.net)
- Switch between installed versions
- Manage php.ini and extensions
- Build and install PECL extensions`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Setup logger
		level := log.LevelNormal
		if verbose {
			level = log.LevelVerbose
		}
		if debug {
			level = log.LevelDebug
		}

		logger := log.New(os.Stderr, level)
		logger.SetNoColor(noColor)
		log.SetDefault(logger)

		// Setup paths
		paths = core.NewPaths(phvmDir)
	},
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&phvmDir, "phvm-dir", "", "Override PHVM_DIR")

	// Add commands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(lsRemoteCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(useCmd)
	rootCmd.AddCommand(currentCmd)
	rootCmd.AddCommand(whichCmd)
	rootCmd.AddCommand(aliasCmd)
	rootCmd.AddCommand(unaliasCmd)
	rootCmd.AddCommand(iniCmd)
	rootCmd.AddCommand(extCmd)
	rootCmd.AddCommand(composerCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(initCmd)
}

// GetPaths returns the paths instance.
func GetPaths() *core.Paths {
	if paths == nil {
		paths = core.NewPaths(phvmDir)
	}
	return paths
}
