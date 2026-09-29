// Package cli provides the command-line interface for phvm.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
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
	paths         *core.Paths
	commandUnlock func() error
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
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		paths = core.NewPaths(phvmDir)
		if cmd == versionCmd {
			fileConfig = core.DefaultConfig()
			effectiveConfig = fileConfig.Clone()
		} else if err := loadCommandConfig(cmd); err != nil {
			return err
		}
		// Setup logger
		level := log.LevelNormal
		if verbose {
			level = log.LevelVerbose
		}
		if debug {
			level = log.LevelDebug
		}

		logger := log.New(cmd.ErrOrStderr(), level)
		logger.SetNoColor(!effectiveConfig.General.Color)
		log.SetDefault(logger)
		if commandUsesState(cmd) {
			locked, release, err := paths.LockState(cmd.Context())
			if err != nil {
				return err
			}
			commandUnlock = release
			cmd.SetContext(locked)
		}
		return nil
	},
}

// Execute runs the CLI.
func Execute() (err error) {
	ctx, stop := interruptionContext(context.Background())
	defer stop()
	// Cobra's help renderer and some result formatters ignore write errors.
	// Record them so a failed output stream cannot produce a successful exit.
	out := rootCmd.OutOrStdout()
	output := &resultWriter{Writer: out}
	rootCmd.SetOut(output)
	defer rootCmd.SetOut(out)
	defer func() {
		if commandUnlock != nil {
			err = errors.Join(err, commandUnlock())
			commandUnlock = nil
		}
	}()
	err = rootCmd.ExecuteContext(ctx)
	if output.err != nil {
		err = errors.Join(err, fmt.Errorf("write command result: %w", output.err))
	}
	var interrupted *interruptionError
	if errors.As(context.Cause(ctx), &interrupted) {
		err = errors.Join(interrupted, err)
	}
	return err
}

type resultWriter struct {
	io.Writer
	err error
}

func (w *resultWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

func commandUsesState(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c == versionCmd || c == doctorCmd || c == initCmd || c == lsRemoteCmd || c == extListRemoteCmd || c == configCmd {
			return false
		}
	}
	return true
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&phvmDir, "phvm-dir", "", "Override PHVM_DIR")
	rootCmd.PersistentFlags().String("mirror", "", "PHP source mirror base URL")
	rootCmd.PersistentFlags().String("user-agent", "", "HTTP User-Agent")
	rootCmd.PersistentFlags().Int("timeout", 60, "HTTP timeout in seconds")
	rootCmd.PersistentFlags().Int("retries", 3, "HTTP retries (0-10)")
	rootCmd.PersistentFlags().Bool("gpg", true, "Verify PHP signatures")
	rootCmd.PersistentFlags().Bool("gpg-fallback-sha256", true, "Allow SHA256 fallback when GPG verification is unavailable")

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
	rootCmd.AddCommand(configCmd)
}

// GetPaths returns the paths instance.
func GetPaths() *core.Paths {
	if paths == nil {
		paths = core.NewPaths(phvmDir)
	}
	return paths
}
