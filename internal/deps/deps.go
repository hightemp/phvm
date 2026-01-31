// Package deps provides dependency management for PHP builds.
package deps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/remote"
)

// Dependency represents a library dependency.
type Dependency struct {
	Name         string
	Version      string
	URL          string
	ConfigureCmd []string
	Required     bool     // If false, skip if build fails
	DependsOn    []string // Dependencies that must be built first
}

// DepsManager manages dependencies for PHP builds.
type DepsManager struct {
	paths      *core.Paths
	downloader *remote.Downloader
	jobs       int
}

// NewDepsManager creates a new DepsManager.
func NewDepsManager(paths *core.Paths, client *remote.Client, jobs int) *DepsManager {
	downloader := remote.NewDownloader(client, paths.Downloads)
	downloader.SetShowProgress(true)
	return &DepsManager{
		paths:      paths,
		downloader: downloader,
		jobs:       jobs,
	}
}

// DepsDir returns the directory for dependencies of a PHP version.
func (m *DepsManager) DepsDir(phpVersion string) string {
	return filepath.Join(m.paths.Root, "deps", phpVersion)
}

// GetRequiredDeps returns the required dependencies for a PHP version.
func GetRequiredDeps(phpVersion string) []Dependency {
	major, minor := parseVersion(phpVersion)

	var deps []Dependency

	// PHP < 8.1 needs OpenSSL 1.1.x for compatibility with OpenSSL 3.x systems
	if major < 8 || (major == 8 && minor < 1) {
		// OpenSSL must be built first
		deps = append(deps, Dependency{
			Name:    "openssl",
			Version: "1.1.1w",
			URL:     "https://www.openssl.org/source/openssl-1.1.1w.tar.gz",
			ConfigureCmd: []string{
				"./config",
				"--prefix=%PREFIX%",
				"--openssldir=%PREFIX%/ssl",
				"no-shared",
				"no-tests",
			},
			Required: true,
		})

		// curl must be built with our OpenSSL
		deps = append(deps, Dependency{
			Name:    "curl",
			Version: "8.5.0",
			URL:     "https://curl.se/download/curl-8.5.0.tar.gz",
			ConfigureCmd: []string{
				"./configure",
				"--prefix=%PREFIX%",
				"--with-openssl=%DEPSDIR%/openssl",
				"--without-libpsl",
				"--without-brotli",
				"--without-zstd",
				"--without-nghttp2",
				"--without-libidn2",
				"--without-librtmp",
				"--disable-shared",
				"--enable-static",
				"--disable-ldap",
				"--disable-ldaps",
				"--disable-rtsp",
				"--disable-dict",
				"--disable-telnet",
				"--disable-tftp",
				"--disable-pop3",
				"--disable-imap",
				"--disable-smb",
				"--disable-smtp",
				"--disable-gopher",
				"--disable-mqtt",
				"--disable-manual",
			},
			Required:  true,
			DependsOn: []string{"openssl"},
		})
	}

	return deps
}

// parseVersion extracts major and minor version numbers.
func parseVersion(version string) (int, int) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	var major, minor int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)
	return major, minor
}

// EnsureDeps ensures all required dependencies are built for a PHP version.
func (m *DepsManager) EnsureDeps(ctx context.Context, phpVersion string) error {
	deps := GetRequiredDeps(phpVersion)
	if len(deps) == 0 {
		return nil
	}

	depsDir := m.DepsDir(phpVersion)
	if err := fsutil.EnsureDir(depsDir); err != nil {
		return fmt.Errorf("create deps directory: %w", err)
	}

	for _, dep := range deps {
		if err := m.ensureDep(ctx, dep, depsDir); err != nil {
			if dep.Required {
				return fmt.Errorf("build %s: %w", dep.Name, err)
			}
			log.Warn("Optional dependency %s failed: %v", dep.Name, err)
		}
	}

	return nil
}

// ensureDep ensures a single dependency is built.
func (m *DepsManager) ensureDep(ctx context.Context, dep Dependency, depsDir string) error {
	prefix := filepath.Join(depsDir, dep.Name)
	markerFile := filepath.Join(prefix, ".phvm-installed")

	// Check if already built
	if fsutil.Exists(markerFile) {
		log.Debug("Dependency %s-%s already built", dep.Name, dep.Version)
		return nil
	}

	log.Info("Building dependency %s %s...", dep.Name, dep.Version)

	// Create directories
	sourceDir := filepath.Join(depsDir, "src", fmt.Sprintf("%s-%s", dep.Name, dep.Version))
	if err := fsutil.EnsureDir(sourceDir); err != nil {
		return fmt.Errorf("create source dir: %w", err)
	}

	// Download
	tarballPath := filepath.Join(depsDir, "src", filepath.Base(dep.URL))
	if !fsutil.Exists(tarballPath) {
		log.Info("Downloading %s...", dep.Name)
		if err := m.downloader.DownloadToPath(ctx, dep.URL, tarballPath); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}

	// Extract
	log.Info("Extracting %s...", dep.Name)
	parentDir := filepath.Dir(sourceDir)
	if err := m.extract(ctx, tarballPath, parentDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Configure
	log.Info("Configuring %s...", dep.Name)
	if err := m.configure(ctx, dep, sourceDir, prefix, depsDir); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	// Build
	log.Info("Building %s...", dep.Name)
	if err := m.makeDep(ctx, dep, sourceDir, depsDir); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	// Install
	log.Info("Installing %s...", dep.Name)
	if err := m.install(ctx, sourceDir); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	// Post-install: fix pkg-config files
	if dep.Name == "openssl" {
		if err := m.fixOpenSSLPkgConfig(depsDir); err != nil {
			log.Warn("Failed to fix openssl pkg-config: %v", err)
		}
	}
	if dep.Name == "curl" {
		if err := m.fixCurlPkgConfig(depsDir); err != nil {
			log.Warn("Failed to fix curl pkg-config: %v", err)
		}
	}

	// Create marker file
	if err := os.WriteFile(markerFile, []byte(dep.Version), 0644); err != nil {
		return fmt.Errorf("create marker: %w", err)
	}

	// Cleanup source
	os.RemoveAll(sourceDir)

	log.Success("Dependency %s %s built successfully", dep.Name, dep.Version)
	return nil
}

// extract extracts a tarball.
func (m *DepsManager) extract(ctx context.Context, tarballPath, destDir string) error {
	var cmd *exec.Cmd
	if strings.HasSuffix(tarballPath, ".tar.gz") || strings.HasSuffix(tarballPath, ".tgz") {
		cmd = exec.CommandContext(ctx, "tar", "-xzf", tarballPath, "-C", destDir)
	} else if strings.HasSuffix(tarballPath, ".tar.xz") {
		cmd = exec.CommandContext(ctx, "tar", "-xJf", tarballPath, "-C", destDir)
	} else if strings.HasSuffix(tarballPath, ".tar.bz2") {
		cmd = exec.CommandContext(ctx, "tar", "-xjf", tarballPath, "-C", destDir)
	} else {
		return fmt.Errorf("unsupported archive format: %s", tarballPath)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar failed: %w\n%s", err, output)
	}
	return nil
}

// configure runs configure for a dependency.
func (m *DepsManager) configure(ctx context.Context, dep Dependency, sourceDir, prefix, depsDir string) error {
	args := make([]string, len(dep.ConfigureCmd))
	for i, arg := range dep.ConfigureCmd {
		arg = strings.ReplaceAll(arg, "%PREFIX%", prefix)
		arg = strings.ReplaceAll(arg, "%DEPSDIR%", depsDir)
		args[i] = arg
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = sourceDir

	// Set up environment with paths to already-built dependencies
	env := os.Environ()
	var pkgConfigPaths []string
	var ldflags []string
	var cflags []string

	for _, depName := range dep.DependsOn {
		depPrefix := filepath.Join(depsDir, depName)
		pkgConfigDir := filepath.Join(depPrefix, "lib", "pkgconfig")
		if fsutil.Exists(pkgConfigDir) {
			pkgConfigPaths = append(pkgConfigPaths, pkgConfigDir)
		}
		libDir := filepath.Join(depPrefix, "lib")
		if fsutil.Exists(libDir) {
			ldflags = append(ldflags, "-L"+libDir)
		}
		includeDir := filepath.Join(depPrefix, "include")
		if fsutil.Exists(includeDir) {
			cflags = append(cflags, "-I"+includeDir)
		}
	}

	if len(pkgConfigPaths) > 0 {
		existingPkgConfig := os.Getenv("PKG_CONFIG_PATH")
		newPath := strings.Join(pkgConfigPaths, ":")
		if existingPkgConfig != "" {
			newPath = newPath + ":" + existingPkgConfig
		}
		env = append(env, "PKG_CONFIG_PATH="+newPath)
	}
	if len(ldflags) > 0 {
		env = append(env, "LDFLAGS="+strings.Join(ldflags, " "))
	}
	if len(cflags) > 0 {
		env = append(env, "CFLAGS="+strings.Join(cflags, " "))
		env = append(env, "CPPFLAGS="+strings.Join(cflags, " "))
	}

	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure failed: %w\n%s", err, output)
	}
	return nil
}

// makeDep runs make for a dependency.
func (m *DepsManager) makeDep(ctx context.Context, dep Dependency, sourceDir, depsDir string) error {
	cmd := exec.CommandContext(ctx, "make", fmt.Sprintf("-j%d", m.jobs))
	cmd.Dir = sourceDir

	// Set up environment with paths to already-built dependencies
	env := os.Environ()
	var ldflags []string
	var cflags []string

	for _, depName := range dep.DependsOn {
		depPrefix := filepath.Join(depsDir, depName)
		libDir := filepath.Join(depPrefix, "lib")
		if fsutil.Exists(libDir) {
			ldflags = append(ldflags, "-L"+libDir)
		}
		includeDir := filepath.Join(depPrefix, "include")
		if fsutil.Exists(includeDir) {
			cflags = append(cflags, "-I"+includeDir)
		}
	}

	if len(ldflags) > 0 {
		env = append(env, "LDFLAGS="+strings.Join(ldflags, " "))
	}
	if len(cflags) > 0 {
		env = append(env, "CFLAGS="+strings.Join(cflags, " "))
		env = append(env, "CPPFLAGS="+strings.Join(cflags, " "))
	}

	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("make failed: %w\n%s", err, output)
	}
	return nil
}

// install runs make install.
func (m *DepsManager) install(ctx context.Context, sourceDir string) error {
	cmd := exec.CommandContext(ctx, "make", "install")
	cmd.Dir = sourceDir
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("make install failed: %w\n%s", err, output)
	}
	return nil
}

// fixOpenSSLPkgConfig fixes OpenSSL pkg-config files to use absolute paths.
func (m *DepsManager) fixOpenSSLPkgConfig(depsDir string) error {
	opensslLibDir := filepath.Join(depsDir, "openssl", "lib")
	libssl := filepath.Join(opensslLibDir, "libssl.a")
	libcrypto := filepath.Join(opensslLibDir, "libcrypto.a")

	// Fix libssl.pc
	libsslPC := filepath.Join(opensslLibDir, "pkgconfig", "libssl.pc")
	if fsutil.Exists(libsslPC) {
		content, err := os.ReadFile(libsslPC)
		if err != nil {
			return err
		}
		newContent := strings.ReplaceAll(string(content), "-lssl", libssl)
		if err := os.WriteFile(libsslPC, []byte(newContent), 0644); err != nil {
			return err
		}
	}

	// Fix libcrypto.pc
	libcryptoPC := filepath.Join(opensslLibDir, "pkgconfig", "libcrypto.pc")
	if fsutil.Exists(libcryptoPC) {
		content, err := os.ReadFile(libcryptoPC)
		if err != nil {
			return err
		}
		newContent := strings.ReplaceAll(string(content), "-lcrypto", libcrypto+" -ldl -lpthread")
		if err := os.WriteFile(libcryptoPC, []byte(newContent), 0644); err != nil {
			return err
		}
	}

	return nil
}

// fixCurlPkgConfig fixes libcurl.pc to use absolute paths for OpenSSL.
func (m *DepsManager) fixCurlPkgConfig(depsDir string) error {
	pcFile := filepath.Join(depsDir, "curl", "lib", "pkgconfig", "libcurl.pc")
	if !fsutil.Exists(pcFile) {
		return nil
	}

	content, err := os.ReadFile(pcFile)
	if err != nil {
		return err
	}

	opensslLibDir := filepath.Join(depsDir, "openssl", "lib")
	libssl := filepath.Join(opensslLibDir, "libssl.a")
	libcrypto := filepath.Join(opensslLibDir, "libcrypto.a")

	// Replace -lssl -lcrypto with absolute paths to static libraries
	newContent := string(content)
	newContent = strings.ReplaceAll(newContent, "-lssl -lcrypto", libssl+" "+libcrypto+" -ldl -lpthread")
	newContent = strings.ReplaceAll(newContent, "-lssl", libssl)
	newContent = strings.ReplaceAll(newContent, "-lcrypto", libcrypto)

	return os.WriteFile(pcFile, []byte(newContent), 0644)
}

// GetBuildEnv returns environment variables for building PHP with dependencies.
func (m *DepsManager) GetBuildEnv(phpVersion string) []string {
	deps := GetRequiredDeps(phpVersion)
	if len(deps) == 0 {
		return nil
	}

	depsDir := m.DepsDir(phpVersion)
	var pkgConfigPaths []string
	var ldflags []string
	var cflags []string
	var libs []string
	var opensslCflags []string
	var env []string

	for _, dep := range deps {
		prefix := filepath.Join(depsDir, dep.Name)
		pkgConfigDir := filepath.Join(prefix, "lib", "pkgconfig")
		if fsutil.Exists(pkgConfigDir) {
			pkgConfigPaths = append(pkgConfigPaths, pkgConfigDir)
		}
		// Also check lib64 for some systems
		pkgConfigDir64 := filepath.Join(prefix, "lib64", "pkgconfig")
		if fsutil.Exists(pkgConfigDir64) {
			pkgConfigPaths = append(pkgConfigPaths, pkgConfigDir64)
		}

		libDir := filepath.Join(prefix, "lib")
		if fsutil.Exists(libDir) {
			ldflags = append(ldflags, "-L"+libDir)
		}
		libDir64 := filepath.Join(prefix, "lib64")
		if fsutil.Exists(libDir64) {
			ldflags = append(ldflags, "-L"+libDir64)
		}

		includeDir := filepath.Join(prefix, "include")
		if fsutil.Exists(includeDir) {
			cflags = append(cflags, "-I"+includeDir)
		}

		// Add explicit static library paths for OpenSSL
		if dep.Name == "openssl" {
			libDir := filepath.Join(prefix, "lib")
			libDir64 := filepath.Join(prefix, "lib64")
			libssl := filepath.Join(libDir, "libssl.a")
			libcrypto := filepath.Join(libDir, "libcrypto.a")
			if !fsutil.Exists(libssl) || !fsutil.Exists(libcrypto) {
				libssl = filepath.Join(libDir64, "libssl.a")
				libcrypto = filepath.Join(libDir64, "libcrypto.a")
			}
			if fsutil.Exists(libssl) && fsutil.Exists(libcrypto) {
				libs = append(libs, libssl, libcrypto, "-ldl", "-lpthread")
			}
			includeDir := filepath.Join(prefix, "include")
			if fsutil.Exists(includeDir) {
				opensslCflags = append(opensslCflags, "-I"+includeDir)
			}
		}

		// Add curl library with OpenSSL dependencies
		if dep.Name == "curl" {
			opensslPrefix := filepath.Join(depsDir, "openssl")
			libDir := filepath.Join(prefix, "lib")
			libDir64 := filepath.Join(prefix, "lib64")
			curlLibDir := libDir
			libcurl := filepath.Join(curlLibDir, "libcurl.a")
			if !fsutil.Exists(libcurl) {
				curlLibDir = libDir64
				libcurl = filepath.Join(curlLibDir, "libcurl.a")
			}

			opensslLibDir := filepath.Join(opensslPrefix, "lib")
			opensslLibDir64 := filepath.Join(opensslPrefix, "lib64")
			libssl := filepath.Join(opensslLibDir, "libssl.a")
			libcrypto := filepath.Join(opensslLibDir, "libcrypto.a")
			if !fsutil.Exists(libssl) || !fsutil.Exists(libcrypto) {
				libssl = filepath.Join(opensslLibDir64, "libssl.a")
				libcrypto = filepath.Join(opensslLibDir64, "libcrypto.a")
			}

			if fsutil.Exists(libcurl) {
				// Override pkg-config to control link order for static libcurl.
				curlCflags := fmt.Sprintf("-I%s/include -DCURL_STATICLIB", prefix)
				curlLibs := fmt.Sprintf("-L%s -lcurl", curlLibDir)
				env = append(env, "CURL_CFLAGS="+curlCflags)
				env = append(env, "CURL_LIBS="+curlLibs)
			}

			// Ensure OpenSSL libs are available in LIBS (appended at end by configure).
			if fsutil.Exists(libssl) && fsutil.Exists(libcrypto) {
				libs = append(libs, libssl, libcrypto, "-ldl", "-lpthread", "-lz")
			}
		}
	}

	if len(pkgConfigPaths) > 0 {
		existingPkgConfig := os.Getenv("PKG_CONFIG_PATH")
		newPath := strings.Join(pkgConfigPaths, ":")
		if existingPkgConfig != "" {
			newPath = newPath + ":" + existingPkgConfig
		}
		env = append(env, "PKG_CONFIG_PATH="+newPath)
	}

	if len(ldflags) > 0 {
		existingLdflags := os.Getenv("LDFLAGS")
		newLdflags := strings.Join(ldflags, " ")
		if existingLdflags != "" {
			newLdflags = newLdflags + " " + existingLdflags
		}
		env = append(env, "LDFLAGS="+newLdflags)
	}

	if len(cflags) > 0 {
		existingCflags := os.Getenv("CFLAGS")
		newCflags := strings.Join(cflags, " ")
		if existingCflags != "" {
			newCflags = newCflags + " " + existingCflags
		}
		env = append(env, "CFLAGS="+newCflags)
		env = append(env, "CPPFLAGS="+newCflags)
	}

	if len(libs) > 0 {
		libsStr := strings.Join(libs, " ")
		env = append(env, "OPENSSL_LIBS="+libsStr)
		// Also add to LIBS so linker tests include OpenSSL
		existingLibs := os.Getenv("LIBS")
		if existingLibs != "" {
			libsStr = libsStr + " " + existingLibs
		}
		env = append(env, "LIBS="+libsStr)
	}

	if len(opensslCflags) > 0 {
		existing := os.Getenv("OPENSSL_CFLAGS")
		newFlags := strings.Join(opensslCflags, " ")
		if existing != "" {
			newFlags = newFlags + " " + existing
		}
		env = append(env, "OPENSSL_CFLAGS="+newFlags)
	}

	return env
}

// GetConfigureFlags returns additional configure flags for PHP with dependencies.
func (m *DepsManager) GetConfigureFlags(phpVersion string) []string {
	deps := GetRequiredDeps(phpVersion)
	if len(deps) == 0 {
		return nil
	}

	depsDir := m.DepsDir(phpVersion)
	var flags []string

	for _, dep := range deps {
		prefix := filepath.Join(depsDir, dep.Name)
		markerFile := filepath.Join(prefix, ".phvm-installed")
		if !fsutil.Exists(markerFile) {
			continue
		}

		switch dep.Name {
		case "openssl":
			flags = append(flags, "--with-openssl="+prefix)
		case "curl":
			flags = append(flags, "--with-curl="+prefix)
		}
	}

	return flags
}

// NeedsDeps returns true if the PHP version needs custom dependencies.
func NeedsDeps(phpVersion string) bool {
	return len(GetRequiredDeps(phpVersion)) > 0
}
