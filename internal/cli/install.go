package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/build"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/doctor"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/remote"
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
	Args: cobra.ExactArgs(1),
	Run:  runInstall,
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

func runInstall(cmd *cobra.Command, args []string) {
	versionArg := args[0]
	paths := GetPaths()

	// Ensure directories exist
	if err := ensureDirectories(paths); err != nil {
		log.Error("Failed to create directories: %v", err)
		os.Exit(1)
	}

	ctx := cmd.Context()
	client := getClient(paths)
	api := getAPI(client)

	// Resolve version
	log.Info("Resolving version %s...", versionArg)
	version, release, err := api.ResolveVersion(ctx, versionArg)
	if err != nil {
		log.Error("Failed to resolve version: %v", err)
		os.Exit(1)
	}
	log.Success("Resolved to PHP %s", version)
	version, err = paths.CheckVersionPath(version)
	if err != nil {
		log.Error("Unsafe PHP installation path: %v", err)
		os.Exit(1)
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
		return
	}

	// Check the selected build environment before downloading source/dependencies.
	requirements, err := doctor.CheckFor(ctx, doctor.Options{PHPVersion: version, Profile: effectiveConfig.General.DefaultProfile, ConfigFlags: configureBaseFlags, CustomFlags: configureCLIFlags, Paths: paths, GPGRequired: effectiveConfig.Verify.GPG && !effectiveConfig.Verify.GPGFallbackSHA256})
	if err != nil {
		log.Error("Build requirements: %v", err)
		os.Exit(1)
	}
	if !requirements.AllOK {
		log.Print("%s", doctor.FormatResults(requirements))
		log.Error("Build requirements check failed before source download")
		os.Exit(1)
	}

	// Get tarball info
	tarball, err := api.GetTarballInfo(ctx, version)
	if err != nil {
		log.Error("Failed to get download info: %v", err)
		os.Exit(1)
	}

	// Download and verify
	verifier := getVerifier(paths, client)

	tarballPath, verification, err := verifier.DownloadAndVerify(ctx, tarball, api.KeyringURL())
	if err != nil {
		log.Error("Download/verification failed: %v", err)
		os.Exit(1)
	}

	// Parse custom configure flags
	customFlags := append([]string{}, configureCLIFlags...)

	// Build
	builder := build.NewBuilder(paths, client)
	if err := builder.SetProfile(effectiveConfig.General.DefaultProfile); err != nil {
		log.Error("Build profile: %v", err)
		os.Exit(1)
	}

	// Open log file
	logPath := paths.LogFile("install-" + version)
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Warn("Failed to create log file: %v", err)
	} else {
		defer logFile.Close()
		builder.SetLogWriter(logFile)
		log.Debug("Build log: %s", logPath)
	}

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

	if err := builder.Build(ctx, buildOpts); err != nil {
		log.Error("Build failed: %v", err)
		log.Print("Check the log file: %s", logPath)
		os.Exit(1)
	}

	// Set as current if no current version
	current := core.NewCurrentManager(paths)
	if !current.IsSet() {
		if err := current.Set(version); err != nil {
			log.Warn("Failed to set as current: %v", err)
		} else {
			log.Info("Set PHP %s as current", version)
		}

		// Also set as default alias
		aliases := core.NewAliasManager(paths)
		if !aliases.Exists("default") {
			_ = aliases.SetDefault(version)
		}
	}

	log.Success("PHP %s installed successfully", version)
	log.Print("Run 'phvm use %s' to switch to this version", version)

	// Suppress unused variable warnings
	_ = release
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <version>",
	Short: "Uninstall a PHP version",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		version := args[0]
		paths := GetPaths()

		installed := core.NewInstalledManager(paths)
		if !installed.IsInstalled(version) {
			log.Error("PHP %s is not installed", version)
			os.Exit(1)
		}

		// Check if it's the current version
		current := core.NewCurrentManager(paths)
		currentVer, _ := current.Get()
		if currentVer == version {
			log.Warn("PHP %s is the current version", version)
			log.Print("Switch to another version first: phvm use <version>")

			force, _ := cmd.Flags().GetBool("force")
			if !force {
				os.Exit(1)
			}
			// Clear current if forcing
			_ = current.Clear()
		}

		log.Info("Uninstalling PHP %s...", version)
		if err := installed.RemoveContext(cmd.Context(), version); err != nil {
			log.Error("Failed to uninstall: %v", err)
			os.Exit(1)
		}

		log.Success("PHP %s uninstalled", version)
	},
}

func init() {
	uninstallCmd.Flags().Bool("force", false, "Force uninstall even if current")
}

// Suppress unused import warning
var _ = context.Background
var _ = remote.DefaultClient
