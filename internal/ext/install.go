// Package ext provides PHP extension management.
package ext

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/ini"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/remote"
)

// Installer installs PHP extensions from PECL.
type Installer struct {
	paths     *core.Paths
	client    *remote.Client
	peclAPI   *remote.PECLAPI
	jobs      int
	logWriter io.Writer
}

// NewInstaller creates a new Installer.
func NewInstaller(paths *core.Paths, client *remote.Client) *Installer {
	return &Installer{
		paths:   paths,
		client:  client,
		peclAPI: remote.NewPECLAPI(client),
		jobs:    2,
	}
}

// SetJobs sets the number of parallel build jobs.
func (i *Installer) SetJobs(jobs int) {
	if jobs > 0 {
		i.jobs = jobs
	}
}

// SetLogWriter sets the log writer for build output.
func (i *Installer) SetLogWriter(w io.Writer) {
	i.logWriter = w
}

// InstallOptions holds options for installing an extension.
type InstallOptions struct {
	Name        string
	Version     string // Empty for latest
	PHPVersion  string
	CustomFlags []string
	Jobs        int
}

// Install installs a PECL extension.
func (i *Installer) Install(ctx context.Context, opts InstallOptions) error {
	log.Info("Installing extension %s", opts.Name)

	if opts.Jobs > 0 {
		i.SetJobs(opts.Jobs)
	}

	// Get PHP paths
	phpDir := i.paths.VersionDir(opts.PHPVersion)
	phpBin := filepath.Join(phpDir, "bin", core.PHPBinary())
	phpize := filepath.Join(phpDir, "bin", "phpize")
	phpConfig := filepath.Join(phpDir, "bin", "php-config")

	// Verify PHP installation
	if !fsutil.Exists(phpBin) {
		return fmt.Errorf("PHP %s is not installed", opts.PHPVersion)
	}
	if !fsutil.Exists(phpize) {
		return fmt.Errorf("phpize not found for PHP %s", opts.PHPVersion)
	}
	if !fsutil.Exists(phpConfig) {
		return fmt.Errorf("php-config not found for PHP %s", opts.PHPVersion)
	}

	// Resolve extension version
	version := opts.Version
	if version == "" {
		var err error
		version, err = i.peclAPI.GetLatestVersion(ctx, opts.Name)
		if err != nil {
			return fmt.Errorf("get latest version: %w", err)
		}
		log.Info("Using version %s", version)
	}

	// Download extension
	downloadURL := i.peclAPI.GetDownloadURL(opts.Name, version)
	filename := fmt.Sprintf("%s-%s.tgz", opts.Name, version)

	downloader := remote.NewDownloader(i.client, i.paths.Extensions)
	tgzPath, err := downloader.Download(ctx, downloadURL, filename)
	if err != nil {
		return fmt.Errorf("download extension: %w", err)
	}

	// Create temp directory for building
	buildDir, err := os.MkdirTemp("", "phvm-ext-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Extract
	log.Info("Extracting...")
	if err := i.extract(ctx, tgzPath, buildDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Find the source directory
	srcDir, err := i.findSourceDir(buildDir, opts.Name)
	if err != nil {
		return fmt.Errorf("find source directory: %w", err)
	}

	// Run phpize
	log.Info("Running phpize...")
	if err := i.runPhpize(ctx, srcDir, phpize); err != nil {
		return fmt.Errorf("phpize: %w", err)
	}

	// Run configure
	log.Info("Configuring...")
	if err := i.runConfigure(ctx, srcDir, phpConfig, opts.CustomFlags); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	// Run make
	log.Info("Building...")
	if err := i.runMake(ctx, srcDir); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	// Run make install
	log.Info("Installing...")
	if err := i.runMakeInstall(ctx, srcDir); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	// Get extension directory
	extDir, err := i.getExtensionDir(phpConfig)
	if err != nil {
		return fmt.Errorf("get extension directory: %w", err)
	}

	// Find the installed .so file
	soFile := filepath.Join(extDir, opts.Name+".so")
	if !fsutil.Exists(soFile) {
		// Try to find it with different name
		entries, _ := os.ReadDir(extDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), opts.Name) && strings.HasSuffix(e.Name(), ".so") {
				soFile = filepath.Join(extDir, e.Name())
				break
			}
		}
	}

	// Create ini file
	confD := i.paths.VersionConfD(opts.PHPVersion)
	confDMgr := ini.NewConfDManager(confD)

	isZend := isZendExtension(opts.Name)
	if err := confDMgr.CreateExtensionIni(opts.Name, opts.Name+".so", 20, isZend); err != nil {
		return fmt.Errorf("create ini file: %w", err)
	}

	// Update metadata
	metadataPath := i.paths.VersionMetadata(opts.PHPVersion)
	if fsutil.Exists(metadataPath) {
		metadata, err := core.LoadMetadata(metadataPath)
		if err == nil {
			metadata.AddExtension(opts.Name, version, true)
			metadata.Save(metadataPath)
		}
	}

	log.Success("Extension %s %s installed successfully", opts.Name, version)
	return nil
}

// extract extracts the tgz file.
func (i *Installer) extract(ctx context.Context, tgzPath, destDir string) error {
	cmd := exec.CommandContext(ctx, "tar", "-xzf", tgzPath, "-C", destDir)
	if i.logWriter != nil {
		cmd.Stdout = i.logWriter
		cmd.Stderr = i.logWriter
	}
	return cmd.Run()
}

// findSourceDir finds the source directory after extraction.
func (i *Installer) findSourceDir(buildDir, extName string) (string, error) {
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Check if it contains config.m4 or configure
			dir := filepath.Join(buildDir, entry.Name())
			if fsutil.Exists(filepath.Join(dir, "config.m4")) ||
				fsutil.Exists(filepath.Join(dir, "configure")) {
				return dir, nil
			}
		}
	}

	return "", fmt.Errorf("source directory not found")
}

// runPhpize runs phpize.
func (i *Installer) runPhpize(ctx context.Context, srcDir, phpize string) error {
	cmd := exec.CommandContext(ctx, phpize)
	cmd.Dir = srcDir
	if i.logWriter != nil {
		cmd.Stdout = i.logWriter
		cmd.Stderr = i.logWriter
	}
	return cmd.Run()
}

// runConfigure runs ./configure.
func (i *Installer) runConfigure(ctx context.Context, srcDir, phpConfig string, customFlags []string) error {
	args := []string{"--with-php-config=" + phpConfig}
	args = append(args, customFlags...)

	cmd := exec.CommandContext(ctx, "./configure", args...)
	cmd.Dir = srcDir
	if i.logWriter != nil {
		cmd.Stdout = i.logWriter
		cmd.Stderr = i.logWriter
	}
	return cmd.Run()
}

// runMake runs make.
func (i *Installer) runMake(ctx context.Context, srcDir string) error {
	cmd := exec.CommandContext(ctx, "make", fmt.Sprintf("-j%d", i.jobs))
	cmd.Dir = srcDir
	if i.logWriter != nil {
		cmd.Stdout = i.logWriter
		cmd.Stderr = i.logWriter
	}
	return cmd.Run()
}

// runMakeInstall runs make install.
func (i *Installer) runMakeInstall(ctx context.Context, srcDir string) error {
	cmd := exec.CommandContext(ctx, "make", "install")
	cmd.Dir = srcDir
	if i.logWriter != nil {
		cmd.Stdout = i.logWriter
		cmd.Stderr = i.logWriter
	}
	return cmd.Run()
}

// getExtensionDir gets the extension directory from php-config.
func (i *Installer) getExtensionDir(phpConfig string) (string, error) {
	cmd := exec.Command(phpConfig, "--extension-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// isZendExtension checks if an extension is a Zend extension.
func isZendExtension(name string) bool {
	zendExts := []string{"xdebug", "opcache", "zend_test", "ionCube"}
	name = strings.ToLower(name)
	for _, ze := range zendExts {
		if strings.ToLower(ze) == name {
			return true
		}
	}
	return false
}

// Uninstall removes an extension.
func (i *Installer) Uninstall(ctx context.Context, name, phpVersion string) error {
	log.Info("Uninstalling extension %s", name)

	// Remove ini file
	confD := i.paths.VersionConfD(phpVersion)
	confDMgr := ini.NewConfDManager(confD)

	files, _ := confDMgr.List()
	for _, f := range files {
		if strings.Contains(f.Name, name) {
			if err := confDMgr.Remove(f.Name); err != nil {
				log.Warn("Failed to remove ini file: %v", err)
			}
		}
	}

	// Update metadata
	metadataPath := i.paths.VersionMetadata(phpVersion)
	if fsutil.Exists(metadataPath) {
		metadata, err := core.LoadMetadata(metadataPath)
		if err == nil {
			metadata.RemoveExtension(name)
			metadata.Save(metadataPath)
		}
	}

	log.Success("Extension %s uninstalled", name)
	return nil
}
