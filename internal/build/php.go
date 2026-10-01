package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/configure"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/deps"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/remote"
	"github.com/hightemp/phvm/internal/toolchain"
)

// Builder builds PHP from source.
type Builder struct {
	paths             *core.Paths
	client            *remote.Client
	jobs              int
	profile           *Profile
	customFlags       []string
	configFlags       []string
	profileErr        error
	actualFlags       []string
	actualEnvironment map[string]string
	logWriter         io.Writer
	depsManager       *deps.DepsManager
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
func (b *Builder) SetProfile(name string) error {
	b.profile = GetProfile(name)
	b.profileErr = nil
	if b.profile == nil {
		b.profileErr = fmt.Errorf("unknown build profile %q; use minimal, common or full", name)
	}
	return b.profileErr
}

// SetCustomFlags sets custom configure flags.
func (b *Builder) SetCustomFlags(flags []string) {
	b.customFlags = append([]string{}, flags...)
}

// SetConfigFlags sets defaults below explicit configure arguments.
func (b *Builder) SetConfigFlags(flags []string) { b.configFlags = append([]string{}, flags...) }

// SetLogWriter sets the log writer for build output.
func (b *Builder) SetLogWriter(w io.Writer) {
	b.logWriter = w
	if b.depsManager != nil {
		b.depsManager.SetLogWriter(w)
	}
}

// BuildOptions holds options for building PHP.
//
//nolint:revive // BuildOptions is more descriptive than just Options
type BuildOptions struct {
	Version      string
	TarballPath  string
	Profile      string
	CustomFlags  []string
	ConfigFlags  []string
	Jobs         int
	SkipVerify   bool
	SourceURL    string
	SHA256       string
	Verification *remote.VerifyResult
}

// Build builds PHP from source.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) error {
	return b.paths.WithStateLock(ctx, func(locked context.Context) error { return b.build(locked, opts) })
}

func (b *Builder) build(ctx context.Context, opts BuildOptions) error {
	startTime := time.Now()

	version, err := b.paths.CheckVersionPath(opts.Version)
	if err != nil {
		return err
	}
	opts.Version = version
	log.Info("Building PHP %s", version)

	// Set options
	if opts.Profile != "" {
		if err := b.SetProfile(opts.Profile); err != nil {
			return err
		}
	}
	b.SetCustomFlags(opts.CustomFlags)
	b.SetConfigFlags(opts.ConfigFlags)
	flags, err := b.ResolvedConfigureFlags(version)
	if err != nil {
		return err
	}
	if opts.Jobs > 0 {
		b.SetJobs(opts.Jobs)
		b.depsManager = deps.NewDepsManager(b.paths, b.client, opts.Jobs)
		b.depsManager.SetLogWriter(b.logWriter)
	}

	// Build dependencies if needed
	if deps.NeedsDeps(version) {
		log.Info("Building required dependencies for PHP %s...", version)
		if err := b.depsManager.EnsureSelected(ctx, version, flags); err != nil {
			return fmt.Errorf("build dependencies: %w", err)
		}
	}

	// Setup directories
	sourceDir := b.paths.SourcePath(version)
	buildDir := b.paths.BuildPath(version)
	installDir := b.paths.VersionDir(version)
	installDir, err = filepath.Abs(installDir)
	if err != nil {
		return err
	}
	tx, err := newPHPTransaction(b.paths, version, installDir)
	if err != nil {
		return err
	}
	defer tx.close()

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
	if deps.NeedsDeps(version) {
		if err := stripSystemInclude(b.paths, buildDir); err != nil {
			return fmt.Errorf("patch makefile includes: %w", err)
		}
	}

	// Build
	if err := b.make(ctx, version, buildDir); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	// Install
	if err := b.install(ctx, version, buildDir, tx.installRoot); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	// Post-install setup
	if err := b.postInstall(ctx, version, tx.candidate); err != nil {
		return fmt.Errorf("post-install: %w", err)
	}
	if err := tx.preservePrevious(); err != nil {
		return fmt.Errorf("preserve previous installation: %w", err)
	}
	metadata, err := b.candidateMetadata(opts, time.Since(startTime), tx.previousMetadata)
	if err != nil {
		return err
	}
	metadata.InstallationState = "staging"
	if err := tx.writeMetadata(metadata); err != nil {
		return err
	}
	identity, err := validatePHPInstallation(ctx, tx.candidate, installDir, version)
	if err != nil {
		return fmt.Errorf("validate staged PHP: %w", err)
	}
	metadata.InstallationState = "ready"
	metadata.PHPAPI, metadata.ZTS, metadata.Debug = identity.API, identity.ZTS, identity.Debug
	if err := tx.writeMetadata(metadata); err != nil {
		return err
	}
	if err := tx.publish(ctx, func() error { _, err := validatePHPInstallation(ctx, installDir, installDir, version); return err }); err != nil {
		return err
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
		cmd = process.CommandContext(ctx, "tar", "-xJf", tarballPath, "-C", parentDir)
	} else if strings.HasSuffix(tarballPath, ".tar.gz") {
		cmd = process.CommandContext(ctx, "tar", "-xzf", tarballPath, "-C", parentDir)
	} else if strings.HasSuffix(tarballPath, ".tar.bz2") {
		cmd = process.CommandContext(ctx, "tar", "-xjf", tarballPath, "-C", parentDir)
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

// ConfigureFlags returns the same merged options used by configure and doctor.
func (b *Builder) ConfigureFlags(version string) []string {
	flags, _ := b.ResolvedConfigureFlags(version)
	return flags
}

// ResolvedConfigureFlags applies profile, config and explicit options with private defaults.
func (b *Builder) ResolvedConfigureFlags(version string) ([]string, error) {
	if b.profileErr != nil {
		return nil, b.profileErr
	}
	if b.profile == nil {
		return nil, fmt.Errorf("build profile is not set")
	}
	if err := configure.ValidateManaged(append(append([]string{}, b.configFlags...), b.customFlags...), false); err != nil {
		return nil, err
	}
	profile := profileFlagsForVersion(b.profile, version)
	if err := validateFlagsForVersion(append(append([]string{}, b.configFlags...), b.customFlags...), version); err != nil {
		return nil, err
	}
	flags := configure.Merge(profile, b.configFlags, b.customFlags)
	if deps.NeedsDeps(version) && b.depsManager != nil {
		selected, err := b.depsManager.Selected(version, flags)
		if err != nil {
			return nil, err
		}
		for _, dep := range selected {
			for n, flag := range flags {
				switch flag {
				case "--with-" + dep.Name, "--with-" + dep.Name + "=yes":
					flags[n] = "--with-" + dep.Name + "=" + filepath.Join(b.depsManager.DepsDir(version), dep.Name)
				case "--with-" + dep.Name + "=shared":
					flags[n] = "--with-" + dep.Name + "=shared," + filepath.Join(b.depsManager.DepsDir(version), dep.Name)
				}
			}
		}
	}
	return flags, nil
}

// Environment snapshots configure's environment, including private dependencies.
func (b *Builder) Environment(version string) toolchain.Environment {
	flags := b.ConfigureFlags(version)
	var overrides []string
	if deps.NeedsDeps(version) && b.depsManager != nil {
		overrides = b.depsManager.GetSelectedBuildEnv(version, flags)
	}
	for _, arg := range flags {
		if !strings.HasPrefix(arg, "--") {
			overrides = append(overrides, arg)
		}
	}
	return toolchain.Current(overrides...)
}

// configure runs ./configure with the appropriate flags.
func (b *Builder) configure(ctx context.Context, version, sourceDir, buildDir, installDir string) error {
	log.Info("Configuring...")
	if _, err := b.ResolvedConfigureFlags(version); err != nil {
		return err
	}
	flags := b.configureArguments(version, installDir)

	log.Debug("Configure flags: %v", flags)

	// Run configure from build directory
	configurePath := filepath.Join(sourceDir, "configure")
	env := b.Environment(version)
	if err := configure.CheckSupported(ctx, configurePath, buildDir, env, explicitConfigureArguments(b.configFlags, b.customFlags)); err != nil {
		if b.logWriter != nil {
			_, _ = fmt.Fprintln(b.logWriter, err)
		}
		return err
	}
	cmd := process.CommandContext(ctx, configurePath, flags...)
	cmd.Dir = buildDir

	// Set environment with dependency paths
	cmd.Env = []string(env)
	b.actualFlags = append([]string{}, flags...)
	b.actualEnvironment = configure.RecordEnvironment(env)

	var tail configureOutputTail
	var output io.Writer = &tail
	if b.logWriter != nil {
		output = io.MultiWriter(b.logWriter, &tail)
	} else if log.Default().Level() >= log.LevelVerbose {
		output = io.MultiWriter(os.Stderr, &tail)
	}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		details := ""
		lines := strings.Split(strings.TrimSpace(string(tail.data)), "\n")
		if len(lines) > 8 {
			lines = lines[len(lines)-8:]
		}
		if text := strings.TrimSpace(strings.Join(lines, "\n")); text != "" {
			details = "\n" + text
		}
		configLog := filepath.Join(buildDir, "config.log")
		if fsutil.Exists(configLog) {
			if appendErr := b.appendConfigLog(buildDir); appendErr != nil {
				log.Warn("Could not include config.log in build log: %s", redact.Text(appendErr.Error()))
			}
			details += "\nSee " + configLog + " for compiler/linker details"
		}
		return fmt.Errorf("configure failed: %w%s\n%s", err, redact.Text(details), env.Describe(ctx))
	}

	return nil
}

func (b *Builder) appendConfigLog(buildDir string) error {
	if b.logWriter == nil {
		return nil
	}
	var root *os.Root
	var err error
	if b.paths != nil {
		root, err = b.paths.OpenDataDir(buildDir, false)
	} else {
		root, err = os.OpenRoot(buildDir)
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat("config.log")
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config.log is not a regular file")
	}
	file, err := root.Open("config.log")
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(b.logWriter, "\n--- config.log ---\n"); err != nil {
		return err
	}
	_, err = io.Copy(b.logWriter, file)
	return err
}

func (b *Builder) configureArguments(version, installDir string) []string {
	if absolute, err := filepath.Abs(installDir); err == nil {
		installDir = absolute
	}
	flags := b.ConfigureFlags(version)
	return append(flags, "--prefix="+installDir, "--with-config-file-path="+filepath.Join(installDir, "etc"), "--with-config-file-scan-dir="+filepath.Join(installDir, "etc", "conf.d"))
}

// configureOutputTail retains bounded output while the full log is streamed.
type configureOutputTail struct {
	data []byte
}

func (w *configureOutputTail) Write(p []byte) (int, error) {
	const limit = 16 * 1024
	n := len(p)
	if len(p) >= limit {
		w.data = append(w.data[:0], p[len(p)-limit:]...)
	} else {
		if len(w.data)+len(p) > limit {
			w.data = w.data[len(w.data)+len(p)-limit:]
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}

func stripSystemInclude(paths *core.Paths, buildDir string) error {
	root, err := paths.OpenDataDir(buildDir, false)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat("Makefile")
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("makefile is not a regular file")
	}
	content, err := root.ReadFile("Makefile")
	if err != nil {
		return err
	}

	updated := strings.ReplaceAll(string(content), "CFLAGS_CLEAN = -I/usr/include ", "CFLAGS_CLEAN = ")
	updated = strings.ReplaceAll(updated, "CFLAGS_CLEAN = -I/usr/include", "CFLAGS_CLEAN =")
	if updated == string(content) {
		return nil
	}

	return fsutil.AtomicWriteRoot(root, "Makefile", []byte(updated), info.Mode().Perm())
}

// make runs make.
func (b *Builder) make(ctx context.Context, version, buildDir string) error {
	log.Info("Building (this may take a while)...")

	cmd := process.CommandContext(ctx, "make", fmt.Sprintf("-j%d", b.jobs))
	cmd.Dir = buildDir

	// Set environment with dependency paths
	cmd.Env = []string(b.Environment(version))

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
func (b *Builder) install(ctx context.Context, version, buildDir, installDir string) error {
	log.Info("Installing...")

	// Ensure install directory exists
	if err := fsutil.EnsureDir(installDir); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}

	cmd := process.CommandContext(ctx, "make", "install", "INSTALL_ROOT="+installDir, "DESTDIR="+installDir)
	cmd.Dir = buildDir
	cmd.Env = []string(b.Environment(version))

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
	if err := ctx.Err(); err != nil {
		return err
	}
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
		productionIni, found, err := b.readProductionIni(version)
		if err != nil {
			return fmt.Errorf("read php.ini-production: %w", err)
		}
		if found {
			if err := fsutil.AtomicWriteFile(iniPath, productionIni, 0644); err != nil {
				return fmt.Errorf("copy php.ini-production: %w", err)
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
				return fmt.Errorf("create php.ini: %w", err)
			}
		}
	}

	// Verify installation
	phpBin := filepath.Join(installDir, "bin", core.PHPBinary())
	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("php binary not found after install")
	}

	return nil
}

func (b *Builder) readProductionIni(version string) ([]byte, bool, error) {
	root, err := b.paths.OpenDataDir(b.paths.SourcePath(version), false)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	const name = "php.ini-production"
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("php.ini-production is not a regular file")
	}
	data, err := root.ReadFile(name)
	return data, err == nil, err
}

// SaveMetadata saves build metadata.
func (b *Builder) SaveMetadata(version, sourceURL, sha256 string, verification *remote.VerifyResult, duration time.Duration) error {
	return b.SaveMetadataContext(context.Background(), version, sourceURL, sha256, verification, duration)
}

// SaveMetadataContext publishes metadata while holding the installation state lock.
func (b *Builder) SaveMetadataContext(ctx context.Context, version, sourceURL, sha256 string, verification *remote.VerifyResult, duration time.Duration) error {
	return b.paths.WithStateLock(ctx, func(context.Context) error { return b.saveMetadata(version, sourceURL, sha256, verification, duration) })
}

func (b *Builder) saveMetadata(version, sourceURL, sha256 string, verification *remote.VerifyResult, duration time.Duration) error {
	if verification == nil || !verification.SHA256Verified {
		return fmt.Errorf("missing successful SHA256 verification")
	}
	var err error
	version, err = b.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	metadata := core.NewMetadata(version)
	metadata.SourceURL = redact.URL(sourceURL)
	metadata.SHA256 = sha256
	metadata.SHA256Verified = verification.SHA256Verified
	metadata.GPGVerified = verification.GPGVerified
	metadata.GPGSkipped = verification.GPGSkipped
	metadata.GPGSkipReason = verification.GPGSkipReason
	metadata.GPGFingerprint = verification.GPGFingerprint
	metadata.ConfigureFlags = append([]string{}, b.actualFlags...)
	metadata.BuildEnvironment = b.actualEnvironment
	metadata.BuildProfile = b.profile.Name
	metadata.BuildDuration = int64(duration.Seconds())

	root, err := b.paths.OpenVersion(version, true)
	if err != nil {
		return err
	}
	defer root.Close()
	previousData, err := root.ReadFile(".phvm-metadata.json")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		var previous core.Metadata
		if err := json.Unmarshal(previousData, &previous); err != nil {
			return err
		}
		metadata.InstallationState, metadata.InstallationID, metadata.PHPAPI = previous.InstallationState, previous.InstallationID, previous.PHPAPI
		metadata.ZTS, metadata.Debug, metadata.Extensions = previous.ZTS, previous.Debug, previous.Extensions
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteRoot(root, ".phvm-metadata.json", data, 0644)
}
