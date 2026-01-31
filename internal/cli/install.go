package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/build"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/doctor"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/remote"
)

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
	installJobs       int
	installProfile    string
	installConfigure  string
	installForce      bool
	installSkipVerify bool
)

func init() {
	installCmd.Flags().IntVarP(&installJobs, "jobs", "j", 0, "Number of parallel build jobs")
	installCmd.Flags().StringVar(&installProfile, "profile", "common", "Build profile (minimal, common, full)")
	installCmd.Flags().StringVar(&installConfigure, "configure", "", "Additional configure flags")
	installCmd.Flags().BoolVar(&installForce, "force", false, "Force reinstall if already installed")
	installCmd.Flags().BoolVar(&installSkipVerify, "skip-verify", false, "Skip checksum verification")
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

	// Check OpenSSL compatibility
	if compatErr := doctor.CheckPHPOpenSSLCompatibility(version); compatErr != "" {
		log.Warn("Compatibility warning:\n%s", compatErr)
	}

	// Check if already installed
	installed := core.NewInstalledManager(paths)
	if installed.IsInstalled(version) && !installForce {
		log.Warn("PHP %s is already installed", version)
		log.Print("Use --force to reinstall")
		return
	}

	// Get tarball info
	tarball, err := api.GetTarballInfo(ctx, version)
	if err != nil {
		log.Error("Failed to get download info: %v", err)
		os.Exit(1)
	}

	// Acquire lock
	lockPath := paths.InstallLockFile(version)
	lock := fsutil.NewFileLock(lockPath)
	acquired, err := lock.TryLock()
	if err != nil {
		log.Error("Failed to acquire lock: %v", err)
		os.Exit(1)
	}
	if !acquired {
		log.Error("Another installation of PHP %s is in progress", version)
		os.Exit(1)
	}
	defer func() { _ = lock.Unlock() }()

	// Download and verify
	startTime := time.Now()
	verifier := getVerifier(paths, client)
	if installSkipVerify {
		verifier.SetGPGEnabled(false)
	}

	tarballPath, err := verifier.DownloadAndVerify(ctx, tarball, api.KeyringURL())
	if err != nil {
		log.Error("Download/verification failed: %v", err)
		os.Exit(1)
	}

	// Parse custom configure flags
	var customFlags []string
	if installConfigure != "" {
		customFlags = strings.Fields(installConfigure)
	}

	// Build
	builder := build.NewBuilder(paths, client)
	builder.SetProfile(installProfile)

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
		Version:     version,
		TarballPath: tarballPath,
		Profile:     installProfile,
		CustomFlags: customFlags,
		Jobs:        installJobs,
	}

	if err := builder.Build(ctx, buildOpts); err != nil {
		log.Error("Build failed: %v", err)
		log.Print("Check the log file: %s", logPath)
		os.Exit(1)
	}

	// Save metadata
	duration := time.Since(startTime)
	gpgVerified := !installSkipVerify && verifier.IsGPGAvailable()
	if err := builder.SaveMetadata(version, tarball.URL, tarball.SHA256, gpgVerified, duration); err != nil {
		log.Warn("Failed to save metadata: %v", err)
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
		if err := installed.Remove(version); err != nil {
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
