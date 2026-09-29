// Package deps provides dependency management for PHP builds.
package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/remote"
	"github.com/hightemp/phvm/internal/toolchain"
)

// Dependency represents a library dependency.
type Dependency struct {
	Name         string
	Version      string
	URL          string
	SHA256       string
	ConfigureCmd []string
	Required     bool     // If false, skip if build fails
	DependsOn    []string // Dependencies that must be built first
}

// DepsManager manages dependencies for PHP builds.
//
//nolint:revive // DepsManager is more descriptive than just Manager.
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

	// PHP 5.x/7.0 require OpenSSL 1.0.2 (no support for 1.1+)
	if major < 7 || (major == 7 && minor < 1) {
		// OpenSSL must be built first
		deps = append(deps, Dependency{
			Name:    "openssl",
			Version: "1.0.2u",
			URL:     "https://www.openssl.org/source/openssl-1.0.2u.tar.gz",
			SHA256:  "ecd0c6ffb493dd06707d38b14bb4d8c2288bb7033735606569d8f90f89669d16",
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
			SHA256:  "05fc17ff25b793a437a0906e0484b82172a9f4de02be5ed447e0cab8c3475add",
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
		return deps
	}

	// PHP 7.1 - 8.0 need OpenSSL 1.1.x for compatibility with OpenSSL 3.x systems
	if major == 7 || (major == 8 && minor < 1) {
		// OpenSSL must be built first
		deps = append(deps, Dependency{
			Name:    "openssl",
			Version: "1.1.1w",
			URL:     "https://www.openssl.org/source/openssl-1.1.1w.tar.gz",
			SHA256:  "cf3098950cb4d853ad95c0841f1f9c6d3dc102dccfcacd521d93925208b76ac8",
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
			SHA256:  "05fc17ff25b793a437a0906e0484b82172a9f4de02be5ed447e0cab8c3475add",
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
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	return major, minor
}

// EnsureDeps ensures all required dependencies are built for a PHP version.
func (m *DepsManager) EnsureDeps(ctx context.Context, phpVersion string) error {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return err
	}
	deps := GetRequiredDeps(phpVersion)
	if len(deps) == 0 {
		return nil
	}

	depsDir := m.DepsDir(phpVersion)
	root, err := m.paths.OpenDataDir(depsDir, true)
	if err != nil {
		return fmt.Errorf("create deps directory: %w", err)
	}
	_ = root.Close()

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
	if err := fsutil.ValidateName(dep.Name); err != nil {
		return err
	}
	if err := fsutil.ValidateName(dep.Version); err != nil {
		return err
	}
	var err error
	dep.SHA256, err = remote.NormalizeSHA256(dep.SHA256)
	if err != nil {
		return err
	}
	dependencies, err := m.dependencyHashes(dep, depsDir)
	if err != nil {
		return fmt.Errorf("dependency trust markers: %w", err)
	}
	prefix := filepath.Join(depsDir, dep.Name)
	markerFile := filepath.Join(prefix, ".phvm-installed")

	// Check if already built
	if m.ready(dep, depsDir) {
		log.Debug("Dependency %s-%s already built", dep.Name, dep.Version)
		return nil
	}

	log.Info("Building dependency %s %s...", dep.Name, dep.Version)

	// Create directories
	sourceDir := filepath.Join(depsDir, "src", fmt.Sprintf("%s-%s", dep.Name, dep.Version))
	// Download
	downloader := remote.NewDownloader(m.downloader.Client(), filepath.Join(depsDir, "src"))
	tarballPath, err := downloader.DownloadVerified(ctx, dep.URL, filepath.Base(dep.URL), dep.SHA256)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	root, err := m.paths.OpenDataDir(prefix, true)
	if err != nil {
		return err
	}
	if err := root.Remove(".phvm-installed"); err != nil && !os.IsNotExist(err) {
		_ = root.Close()
		return err
	}
	_ = root.Close()
	if err := os.RemoveAll(sourceDir); err != nil {
		return err
	}
	if err := fsutil.EnsureDir(sourceDir); err != nil {
		return err
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
	if err := m.install(ctx, dep, sourceDir, depsDir); err != nil {
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
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(dependencyMarker{Schema: 1, Name: dep.Name, Version: dep.Version, URL: dep.URL, SHA256: dep.SHA256, ConfigureCmd: dep.ConfigureCmd, Dependencies: dependencies})
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWriteFile(markerFile, data, 0644); err != nil {
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
		cmd = process.CommandContext(ctx, "tar", "-xzf", tarballPath, "-C", destDir)
	} else if strings.HasSuffix(tarballPath, ".tar.xz") {
		cmd = process.CommandContext(ctx, "tar", "-xJf", tarballPath, "-C", destDir)
	} else if strings.HasSuffix(tarballPath, ".tar.bz2") {
		cmd = process.CommandContext(ctx, "tar", "-xjf", tarballPath, "-C", destDir)
	} else {
		return fmt.Errorf("unsupported archive format: %s", tarballPath)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar failed: %w\n%s", err, output)
	}
	return nil
}

// dependencyEnvironment prepends private paths while preserving each user flag.
func dependencyEnvironment(names []string, depsDir string) toolchain.Environment {
	var includes, libraries, pkgPaths []string
	for _, name := range names {
		prefix := filepath.Join(depsDir, name)
		if dir := filepath.Join(prefix, "include"); fsutil.Exists(dir) {
			includes = append(includes, "-I"+dir)
		}
		if dir := filepath.Join(prefix, "lib"); fsutil.Exists(dir) {
			libraries = append(libraries, "-L"+dir)
		}
		if dir := filepath.Join(prefix, "lib", "pkgconfig"); fsutil.Exists(dir) {
			pkgPaths = append(pkgPaths, dir)
		}
	}
	base := toolchain.Current()
	var overrides []string
	for _, item := range []struct {
		key       string
		flags     []string
		separator string
	}{
		{"CPPFLAGS", includes, " "}, {"CFLAGS", includes, " "}, {"LDFLAGS", libraries, " "}, {"PKG_CONFIG_PATH", pkgPaths, string(os.PathListSeparator)},
	} {
		if len(item.flags) == 0 {
			continue
		}
		value := strings.Join(item.flags, item.separator)
		if existing := base.Value(item.key, ""); existing != "" {
			value += item.separator + existing
		}
		overrides = append(overrides, item.key+"="+value)
	}
	return toolchain.Current(overrides...)
}

// configure runs configure for a dependency.
func (m *DepsManager) configure(ctx context.Context, dep Dependency, sourceDir, prefix, depsDir string) error {
	args := make([]string, len(dep.ConfigureCmd))
	for i, arg := range dep.ConfigureCmd {
		arg = strings.ReplaceAll(arg, "%PREFIX%", prefix)
		arg = strings.ReplaceAll(arg, "%DEPSDIR%", depsDir)
		args[i] = arg
	}

	cmd := process.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = sourceDir

	cmd.Env = []string(dependencyEnvironment(dep.DependsOn, depsDir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure failed: %w\n%s", err, output)
	}
	return nil
}

// makeDep runs make for a dependency.
func (m *DepsManager) makeDep(ctx context.Context, dep Dependency, sourceDir, depsDir string) error {
	cmd := process.CommandContext(ctx, "make", fmt.Sprintf("-j%d", m.jobs))
	cmd.Dir = sourceDir

	cmd.Env = []string(dependencyEnvironment(dep.DependsOn, depsDir))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("make failed: %w\n%s", err, output)
	}
	return nil
}

// install runs make install.
func (m *DepsManager) install(ctx context.Context, dep Dependency, sourceDir, depsDir string) error {
	cmd := process.CommandContext(ctx, "make", "install")
	cmd.Dir = sourceDir
	cmd.Env = []string(dependencyEnvironment(dep.DependsOn, depsDir))

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
	return m.buildEnv(phpVersion, GetRequiredDeps(phpVersion))
}

func (m *DepsManager) buildEnv(phpVersion string, deps []Dependency) []string {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return nil
	}
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
			ldflags = append(ldflags, quoteBuildFlag("-L"+libDir))
		}
		libDir64 := filepath.Join(prefix, "lib64")
		if fsutil.Exists(libDir64) {
			ldflags = append(ldflags, quoteBuildFlag("-L"+libDir64))
		}

		includeDir := filepath.Join(prefix, "include")
		if fsutil.Exists(includeDir) {
			cflags = append(cflags, quoteBuildFlag("-I"+includeDir))
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
				opensslCflags = append(opensslCflags, quoteBuildFlag("-I"+includeDir))
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
				curlCflags := quoteBuildFlag("-I"+filepath.Join(prefix, "include")) + " -DCURL_STATICLIB"
				curlLibs := quoteBuildFlag("-L"+curlLibDir) + " -lcurl"
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
		newCPPFlags := strings.Join(cflags, " ")
		if existing := os.Getenv("CPPFLAGS"); existing != "" {
			newCPPFlags += " " + existing
		}
		env = append(env, "CPPFLAGS="+newCPPFlags)
	}

	if len(libs) > 0 {
		for n, value := range libs {
			libs[n] = quoteBuildFlag(value)
		}
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

func quoteBuildFlag(value string) string {
	if !strings.ContainsAny(value, " \t'\"\\$`") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// GetConfigureFlags returns additional configure flags for PHP with dependencies.
func (m *DepsManager) GetConfigureFlags(phpVersion string) []string {
	var err error
	phpVersion, err = m.paths.CheckVersionPath(phpVersion)
	if err != nil {
		return nil
	}
	deps := GetRequiredDeps(phpVersion)
	if len(deps) == 0 {
		return nil
	}

	depsDir := m.DepsDir(phpVersion)
	var flags []string

	for _, dep := range deps {
		prefix := filepath.Join(depsDir, dep.Name)
		if !m.ready(dep, depsDir) {
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
