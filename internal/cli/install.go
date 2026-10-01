package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/build"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/doctor"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

// parseVersionParts extracts major and minor version numbers.
func parseVersionParts(version string) (int, int) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	var major, minor int
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	return major, minor
}

var installCmd = &cobra.Command{
	Use:   "install <version>",
	Short: "Install a PHP version",
	Long: `Install a PHP version from source.

The version can be:
- A full version number: 8.3.30
- A minor version: 8.3 (installs latest 8.3.x)
- A major version: 8 (installs latest 8.x.x)
- An alias: latest, stable

Examples:
  phvm install 8.3
  phvm install 8.3.30
  phvm install latest
  phvm install 8 --profile minimal
  phvm install 8.3 --configure "--with-pdo-mysql"`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeInstallPHP,
	RunE:              runInstall,
}

var (
	installJobs      int
	installProfile   string
	installConfigure string
	installForce     bool
	installSkipGPG   bool
)

func init() {
	installCmd.Flags().IntVarP(&installJobs, "jobs", "j", 0, "Number of parallel build jobs (0: automatic)")
	installCmd.Flags().StringVar(&installProfile, "profile", "common", "Build profile (minimal, common, full)")
	installCmd.Flags().StringVar(&installConfigure, "configure", "", "Additional configure flags")
	installCmd.Flags().StringArray("configure-flag", nil, "One exact configure argument (repeatable; overrides --configure)")
	installCmd.Flags().BoolVar(&installForce, "force", false, "Force reinstall if already installed")
	installCmd.Flags().BoolVar(&installSkipGPG, "skip-gpg", false, "Skip GPG signature verification; SHA256 is always required")
	installCmd.Flags().Bool("skip-verify", false, "Deprecated alias of --skip-gpg; SHA256 remains required")
	_ = installCmd.Flags().MarkDeprecated("skip-verify", "use --skip-gpg; SHA256 verification is mandatory")
}

func runInstall(cmd *cobra.Command, args []string) error {
	versionArg := args[0]
	paths := GetPaths()

	// Ensure directories exist
	if err := ensureDirectories(paths); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	ctx := cmd.Context()
	client := getClient(paths)
	api := getAPI(client)

	// Resolve version
	log.Info("Resolving version %s...", versionArg)
	version, release, err := api.ResolveVersion(ctx, versionArg)
	if err != nil {
		return fmt.Errorf("failed to resolve version: %w", err)
	}
	log.Success("Resolved to PHP %s", version)
	version, err = paths.CheckVersionPath(version)
	if err != nil {
		return fmt.Errorf("unsafe PHP installation path: %w", err)
	}

	// Notify about dependencies that will be built
	if doctor.GetOpenSSLMajorVersion() >= 3 {
		major, minor := parseVersionParts(version)
		if major < 7 || (major == 7 && minor < 1) {
			log.Info("PHP %s requires OpenSSL 1.0.2 - will build it automatically", version)
		} else if major == 7 || (major == 8 && minor < 1) {
			log.Info("PHP %s requires OpenSSL 1.1.x - will build it automatically", version)
		}
	}

	// Check if already installed
	installed := core.NewInstalledManager(paths)
	if installed.IsInstalled(version) && !installForce {
		log.Warn("PHP %s is already installed", version)
		log.Print("Use --force to reinstall")
		return nil
	}

	// Check the selected build environment before downloading source/dependencies.
	requirements, err := doctor.CheckFor(ctx, doctor.Options{PHPVersion: version, Profile: effectiveConfig.General.DefaultProfile, ConfigFlags: configureBaseFlags, CustomFlags: configureCLIFlags, Paths: paths, GPGRequired: effectiveConfig.Verify.GPG && !effectiveConfig.Verify.GPGFallbackSHA256})
	if err != nil {
		return fmt.Errorf("build requirements: %w", err)
	}
	if !requirements.AllOK {
		log.Print("%s", doctor.FormatResultsWithPalette(requirements, commandPalette(cmd, cmd.ErrOrStderr())))
		return fmt.Errorf("build requirements check failed before source download")
	}

	// Get tarball info
	tarball, err := api.GetTarballInfo(ctx, version)
	if err != nil {
		return fmt.Errorf("failed to get download info: %w", err)
	}

	// Download and verify
	verifier := getVerifier(paths, client)

	tarballPath, verification, err := verifier.DownloadAndVerify(ctx, tarball, api.KeyringURL())
	if err != nil {
		return fmt.Errorf("download/verification failed: %w", err)
	}

	// Parse custom configure flags
	customFlags := append([]string{}, configureCLIFlags...)

	// Build
	builder := build.NewBuilder(paths, client)
	if err := builder.SetProfile(effectiveConfig.General.DefaultProfile); err != nil {
		return fmt.Errorf("build profile: %w", err)
	}

	// Open log file
	logPath := paths.LogFile("install-" + version)
	logFile, err := openInstallLog(paths, version)
	var logSink io.Writer
	if err != nil {
		log.Warn("Failed to create log file: %s", redact.Text(err.Error()))
	} else {
		defer logFile.Close()
		logSink = logFile
		log.Debug("Build log: %s", logPath)
	}
	diagnostics := newBuildDiagnostics(logSink)
	builder.SetLogWriter(diagnostics)

	buildOpts := build.BuildOptions{
		Version:      version,
		TarballPath:  tarballPath,
		Profile:      effectiveConfig.General.DefaultProfile,
		CustomFlags:  customFlags,
		ConfigFlags:  configureBaseFlags,
		Jobs:         effectiveConfig.General.ParallelJobs,
		SourceURL:    tarball.URL,
		SHA256:       tarball.SHA256,
		Verification: verification,
	}

	buildErr := builder.Build(ctx, buildOpts)
	flushErr := diagnostics.Flush()
	if logFile != nil {
		flushErr = errors.Join(flushErr, logFile.Sync())
	} else {
		logPath = ""
	}
	if buildErr != nil {
		return &installBuildError{cause: buildErr, outputErr: flushErr, stage: buildStage(buildErr), detail: diagnostics.cause(), logPath: logPath}
	}
	if flushErr != nil {
		log.Warn("Build log is incomplete: %s", redact.Text(flushErr.Error()))
	}

	// Set as current if no current version
	current := core.NewCurrentManager(paths)
	if !current.IsSet() {
		if err := current.Set(version); err != nil {
			return fmt.Errorf("PHP installed but failed to select current: %w", err)
		}
		log.Info("Set PHP %s as current", version)

		// Also set as default alias
		aliases := core.NewAliasManager(paths)
		if !aliases.Exists("default") {
			if err := aliases.SetDefault(version); err != nil {
				return fmt.Errorf("PHP installed but failed to set default alias: %w", err)
			}
		}
	}

	log.Success("PHP %s installed successfully", version)
	log.Print("Run 'phvm use %s' to switch to this version", version)

	// Suppress unused variable warnings
	_ = release
	return nil
}

func openInstallLog(paths *core.Paths, version string) (*os.File, error) {
	version, err := core.NormalizeInstalledVersion(version)
	if err != nil {
		return nil, err
	}
	root, err := paths.OpenDataDir(paths.Logs, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := "install-" + version + ".log"
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("install log is not a regular file")
		}
		if err := root.Chmod(name, 0600); err != nil {
			return nil, fmt.Errorf("restrict install log permissions: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
}

var uninstallCmd = &cobra.Command{
	Use:               "uninstall <version>",
	Short:             "Uninstall a PHP version",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeUninstallPHP,
	RunE: func(cmd *cobra.Command, args []string) error {
		version := args[0]
		paths := GetPaths()

		installed := core.NewInstalledManager(paths)
		if !installed.IsInstalled(version) {
			return fmt.Errorf("PHP %s is not installed", version)
		}

		// Check if it's the current version
		current := core.NewCurrentManager(paths)
		currentVer, _ := current.Get()
		if currentVer == version {
			log.Warn("PHP %s is the current version", version)
			log.Print("Switch to another version first: phvm use <version>")

			force, _ := cmd.Flags().GetBool("force")
			if !force {
				return fmt.Errorf("cannot uninstall current PHP; switch versions or pass --force")
			}
			// Clear current if forcing
			if err := current.Clear(); err != nil {
				return fmt.Errorf("clear current PHP: %w", err)
			}
		}

		log.Info("Uninstalling PHP %s...", version)
		if err := installed.RemoveContext(cmd.Context(), version); err != nil {
			return fmt.Errorf("failed to uninstall: %w", err)
		}

		log.Success("PHP %s uninstalled", version)
		return nil
	},
}

func init() {
	uninstallCmd.Flags().Bool("force", false, "Force uninstall even if current")
}
