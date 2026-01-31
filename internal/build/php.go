package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/deps"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/remote"
)

// Builder builds PHP from source.
type Builder struct {
	paths       *core.Paths
	client      *remote.Client
	jobs        int
	profile     *Profile
	customFlags []string
	logWriter   io.Writer
	depsManager *deps.DepsManager
}

// NewBuilder creates a new Builder.
func NewBuilder(paths *core.Paths, client *remote.Client) *Builder {
	jobs := runtime.NumCPU()
	if jobs > 1 {
		jobs = jobs / 2
	}
	if jobs < 1 {
		jobs = 1
	}

	return &Builder{
		paths:       paths,
		client:      client,
		jobs:        jobs,
		profile:     CommonProfile(),
		depsManager: deps.NewDepsManager(paths, client, jobs),
	}
}

// SetJobs sets the number of parallel build jobs.
func (b *Builder) SetJobs(jobs int) {
	if jobs > 0 {
		b.jobs = jobs
	}
}

// SetProfile sets the build profile.
func (b *Builder) SetProfile(name string) {
	b.profile = GetProfile(name)
}

// SetCustomFlags sets custom configure flags.
func (b *Builder) SetCustomFlags(flags []string) {
	b.customFlags = flags
}

// SetLogWriter sets the log writer for build output.
func (b *Builder) SetLogWriter(w io.Writer) {
	b.logWriter = w
}

// BuildOptions holds options for building PHP.
//
//nolint:revive // BuildOptions is more descriptive than just Options
type BuildOptions struct {
	Version     string
	TarballPath string
	Profile     string
	CustomFlags []string
	Jobs        int
	SkipVerify  bool
}

// Build builds PHP from source.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) error {
	startTime := time.Now()

	version := opts.Version
	log.Info("Building PHP %s", version)

	// Set options
	if opts.Profile != "" {
		b.SetProfile(opts.Profile)
	}
	if len(opts.CustomFlags) > 0 {
		b.SetCustomFlags(opts.CustomFlags)
	}
	if opts.Jobs > 0 {
		b.SetJobs(opts.Jobs)
		b.depsManager = deps.NewDepsManager(b.paths, b.client, opts.Jobs)
	}

	// Build dependencies if needed
	if deps.NeedsDeps(version) {
		log.Info("Building required dependencies for PHP %s...", version)
		if err := b.depsManager.EnsureDeps(ctx, version); err != nil {
			return fmt.Errorf("build dependencies: %w", err)
		}
	}

	// Setup directories
	sourceDir := b.paths.SourcePath(version)
	buildDir := b.paths.BuildPath(version)
	installDir := b.paths.VersionDir(version)

	// Extract source
	if err := b.extract(ctx, opts.TarballPath, sourceDir); err != nil {
		return fmt.Errorf("extract source: %w", err)
	}

	// Create build directory
	if err := fsutil.EnsureDir(buildDir); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}

	// Configure
	if err := b.configure(ctx, version, sourceDir, buildDir, installDir); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	// Build
	if err := b.make(ctx, version, buildDir); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	// Install
	if err := b.install(ctx, buildDir, installDir); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	// Post-install setup
	if err := b.postInstall(ctx, version, installDir); err != nil {
		return fmt.Errorf("post-install: %w", err)
	}

	// Cleanup build directory
	if err := os.RemoveAll(buildDir); err != nil {
		log.Warn("Failed to cleanup build directory: %v", err)
	}

	// Cleanup source directory
	if err := os.RemoveAll(sourceDir); err != nil {
		log.Warn("Failed to cleanup source directory: %v", err)
	}

	duration := time.Since(startTime)
	log.Success("PHP %s built successfully in %v", version, duration.Round(time.Second))

	return nil
}

// extract extracts the tarball to the source directory.
func (b *Builder) extract(ctx context.Context, tarballPath, sourceDir string) error {
	log.Info("Extracting source...")

	// Remove existing source directory
	if fsutil.Exists(sourceDir) {
		if err := os.RemoveAll(sourceDir); err != nil {
			return fmt.Errorf("remove existing source: %w", err)
		}
	}

	// Create parent directory
	parentDir := filepath.Dir(sourceDir)
	if err := fsutil.EnsureDir(parentDir); err != nil {
		return fmt.Errorf("create source parent: %w", err)
	}

	// Determine extraction command based on file extension
	var cmd *exec.Cmd
	if strings.HasSuffix(tarballPath, ".tar.xz") {
		cmd = exec.CommandContext(ctx, "tar", "-xJf", tarballPath, "-C", parentDir)
	} else if strings.HasSuffix(tarballPath, ".tar.gz") {
		cmd = exec.CommandContext(ctx, "tar", "-xzf", tarballPath, "-C", parentDir)
	} else if strings.HasSuffix(tarballPath, ".tar.bz2") {
		cmd = exec.CommandContext(ctx, "tar", "-xjf", tarballPath, "-C", parentDir)
	} else {
		return fmt.Errorf("unsupported archive format: %s", tarballPath)
	}

	if b.logWriter != nil {
		cmd.Stdout = b.logWriter
		cmd.Stderr = b.logWriter
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract failed: %w", err)
	}

	// The archive extracts to php-X.Y.Z, rename if needed
	extractedDir := filepath.Join(parentDir, "php-"+filepath.Base(sourceDir)[4:])
	if extractedDir != sourceDir && fsutil.Exists(extractedDir) {
		if err := os.Rename(extractedDir, sourceDir); err != nil {
			return fmt.Errorf("rename extracted directory: %w", err)
		}
	}

	return nil
}

// configure runs ./configure with the appropriate flags.
func (b *Builder) configure(ctx context.Context, version, sourceDir, buildDir, installDir string) error {
	log.Info("Configuring...")

	// Merge profile and custom flags
	flags := MergeFlags(b.profile, b.customFlags)

	// Add dependency-specific configure flags
	if deps.NeedsDeps(version) {
		depsFlags := b.depsManager.GetConfigureFlags(version)
		if len(depsFlags) > 0 {
			log.Debug("Adding dependency flags: %v", depsFlags)
			flags = MergeFlags(&Profile{Flags: flags}, depsFlags)
		}
	}

	// Add prefix
	flags = append([]string{"--prefix=" + installDir}, flags...)

	// Add config file paths
	etcDir := filepath.Join(installDir, "etc")
	confDDir := filepath.Join(etcDir, "conf.d")
	flags = append(flags,
		"--with-config-file-path="+etcDir,
		"--with-config-file-scan-dir="+confDDir,
	)

	log.Debug("Configure flags: %v", flags)

	// Run configure from build directory
	configurePath := filepath.Join(sourceDir, "configure")
	cmd := exec.CommandContext(ctx, configurePath, flags...)
	cmd.Dir = buildDir

	// Set environment with dependency paths
	env := os.Environ()
	if deps.NeedsDeps(version) {
		depsEnv := b.depsManager.GetBuildEnv(version)
		if len(depsEnv) > 0 {
			log.Debug("Adding dependency env: %v", depsEnv)
			env = append(env, depsEnv...)
		}
	}
	cmd.Env = env

	if b.logWriter != nil {
		cmd.Stdout = b.logWriter
		cmd.Stderr = b.logWriter
	} else {
		// Show configure output for verbose logging
		if log.Default().Level() >= log.LevelVerbose {
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
		}
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configure failed: %w", err)
	}

	return nil
}

// make runs make.
func (b *Builder) make(ctx context.Context, version, buildDir string) error {
	log.Info("Building (this may take a while)...")

	cmd := exec.CommandContext(ctx, "make", fmt.Sprintf("-j%d", b.jobs))
	cmd.Dir = buildDir

	// Set environment with dependency paths
	env := os.Environ()
	if deps.NeedsDeps(version) {
		depsEnv := b.depsManager.GetBuildEnv(version)
		if len(depsEnv) > 0 {
			env = append(env, depsEnv...)
		}
	}
	cmd.Env = env

	if b.logWriter != nil {
		cmd.Stdout = b.logWriter
		cmd.Stderr = b.logWriter
	} else if log.Default().Level() >= log.LevelVerbose {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make failed: %w", err)
	}

	return nil
}

// install runs make install.
func (b *Builder) install(ctx context.Context, buildDir, installDir string) error {
	log.Info("Installing...")

	// Ensure install directory exists
	if err := fsutil.EnsureDir(installDir); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, "make", "install")
	cmd.Dir = buildDir
	cmd.Env = os.Environ()

	if b.logWriter != nil {
		cmd.Stdout = b.logWriter
		cmd.Stderr = b.logWriter
	} else if log.Default().Level() >= log.LevelVerbose {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make install failed: %w", err)
	}

	return nil
}

// postInstall performs post-installation setup.
func (b *Builder) postInstall(ctx context.Context, version, installDir string) error {
	log.Debug("Running post-install setup...")

	etcDir := filepath.Join(installDir, "etc")
	confDDir := filepath.Join(etcDir, "conf.d")

	// Create etc and conf.d directories
	if err := fsutil.EnsureDir(etcDir); err != nil {
		return fmt.Errorf("create etc directory: %w", err)
	}
	if err := fsutil.EnsureDir(confDDir); err != nil {
		return fmt.Errorf("create conf.d directory: %w", err)
	}

	// Create default php.ini if not exists
	iniPath := filepath.Join(etcDir, "php.ini")
	if !fsutil.Exists(iniPath) {
		// Copy php.ini-production from source if available, otherwise create empty
		sourceIni := b.paths.SourcePath(version)
		prodIni := filepath.Join(sourceIni, "php.ini-production")
		if fsutil.Exists(prodIni) {
			if err := fsutil.AtomicCopyFile(prodIni, iniPath, 0644); err != nil {
				log.Warn("Failed to copy php.ini-production: %v", err)
			}
		} else {
			// Create minimal php.ini
			minimalIni := `; PHP Configuration
; Created by phvm

[PHP]
; Production settings
error_reporting = E_ALL & ~E_DEPRECATED & ~E_STRICT
display_errors = Off
log_errors = On

[Date]
; Set your timezone
; date.timezone = UTC

[opcache]
opcache.enable=1
opcache.enable_cli=0
`
			if err := fsutil.AtomicWriteFile(iniPath, []byte(minimalIni), 0644); err != nil {
				log.Warn("Failed to create php.ini: %v", err)
			}
		}
	}

	// Verify installation
	phpBin := filepath.Join(installDir, "bin", core.PHPBinary())
	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("php binary not found after install")
	}

	// Test php binary
	cmd := exec.CommandContext(ctx, phpBin, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("php binary test failed: %w\nOutput: %s", err, string(output))
	}

	log.Debug("PHP binary test: %s", strings.TrimSpace(string(output)))

	return nil
}

// SaveMetadata saves build metadata.
func (b *Builder) SaveMetadata(version, sourceURL, sha256 string, gpgVerified bool, duration time.Duration) error {
	metadata := core.NewMetadata(version)
	metadata.SourceURL = sourceURL
	metadata.SHA256 = sha256
	metadata.GPGVerified = gpgVerified
	metadata.ConfigureFlags = MergeFlags(b.profile, b.customFlags)
	metadata.BuildProfile = b.profile.Name
	metadata.BuildDuration = int64(duration.Seconds())

	metadataPath := b.paths.VersionMetadata(version)
	return metadata.Save(metadataPath)
}
