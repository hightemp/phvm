// Package doctor provides system requirements checking.
package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/redact"
	"github.com/hightemp/phvm/internal/toolchain"
)

// CheckResult represents the result of a dependency check.
type CheckResult struct {
	Name        string
	Found       bool
	Path        string
	Version     string
	Required    bool
	HelpText    string
	Problem     string
	ProblemKind string
	Deferred    bool
}

// DoctorResult holds all check results.
//
//nolint:revive // DoctorResult is more descriptive than just Result
type DoctorResult struct {
	Checks      []CheckResult
	AllOK       bool
	Warnings    int
	Errors      int
	Deferred    int
	PHPVersion  string
	Profile     string
	Environment string
}

// GetOpenSSLMajorVersion returns the major version of OpenSSL (1 or 3).
// Returns 0 if OpenSSL is not found or version cannot be determined.
func GetOpenSSLMajorVersion() int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd, err := toolchain.Current().Command(ctx, "PKG_CONFIG", "pkg-config", "--modversion", "openssl")
	if err != nil {
		return 0
	}
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	version := strings.TrimSpace(string(output))
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return major
}

// CheckPHPOpenSSLCompatibility checks if PHP version is compatible with system OpenSSL.
// Returns an error message if incompatible, empty string if compatible.
func CheckPHPOpenSSLCompatibility(phpVersion string) string {
	opensslMajor := GetOpenSSLMajorVersion()
	if opensslMajor == 0 {
		return ""
	}

	// Parse PHP version
	parts := strings.Split(phpVersion, ".")
	if len(parts) < 2 {
		return ""
	}
	phpMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		return ""
	}
	phpMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}

	// PHP < 8.1 is not compatible with OpenSSL 3.0
	if opensslMajor >= 3 && (phpMajor < 8 || (phpMajor == 8 && phpMinor < 1)) {
		return fmt.Sprintf(
			"PHP %s is not compatible with OpenSSL 3.x (you have OpenSSL %d.x).\n"+
				"Options:\n"+
				"  1. Build without OpenSSL: phvm install %s --configure=\"--without-openssl --without-curl\"\n"+
				"  2. Use minimal profile: phvm install %s --profile=minimal\n"+
				"  3. Install PHP 8.1+ which supports OpenSSL 3.x",
			phpVersion, opensslMajor, phpVersion, phpVersion)
	}

	return ""
}

// Check runs common-profile checks for current and returns resolution failures.
func Check() (*DoctorResult, error) {
	return CheckFor(context.Background(), Options{})
}

// checkPkgConfig is also used by isolated library regression tests.
func checkPkgConfig(name string, required bool) CheckResult {
	return checkLibrary(context.Background(), toolchain.Current(), probeFor(name), required)
}

// getInstallHint returns installation instructions for a dependency.
func getInstallHint(name string) string {
	switch runtime.GOOS {
	case "linux":
		return getLinuxInstallHint(name)
	case "darwin":
		return getDarwinInstallHint(name)
	case "windows":
		return getWindowsInstallHint(name)
	default:
		return fmt.Sprintf("Install %s using your package manager", name)
	}
}

// getLinuxInstallHint returns Linux installation instructions.
func getLinuxInstallHint(name string) string {
	// Try to detect distro
	distro := detectLinuxDistro()

	switch distro {
	case "debian", "ubuntu":
		return getDebianHint(name)
	case "fedora", "rhel", "centos":
		return getFedoraHint(name)
	case "arch":
		return getArchHint(name)
	default:
		return fmt.Sprintf("Install %s using your package manager", name)
	}
}

// detectLinuxDistro tries to detect the Linux distribution.
func detectLinuxDistro() string {
	cmd := exec.Command("cat", "/etc/os-release")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	content := strings.ToLower(string(output))
	if strings.Contains(content, "ubuntu") || strings.Contains(content, "debian") {
		return "debian"
	}
	if strings.Contains(content, "fedora") {
		return "fedora"
	}
	if strings.Contains(content, "centos") || strings.Contains(content, "rhel") {
		return "rhel"
	}
	if strings.Contains(content, "arch") {
		return "arch"
	}

	return ""
}

func getDebianHint(name string) string {
	return fmt.Sprintf("sudo apt-get install %s", getPackageName(name, "debian"))
}

func getFedoraHint(name string) string {
	packages := map[string]string{
		"make":       "make",
		"cc":         "gcc",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkgconfig",
		"tar":        "tar",
		"openssl":    "openssl-devel",
		"libcurl":    "libcurl-devel",
		"zlib":       "zlib-devel",
		"libxml-2.0": "libxml2-devel",
	}

	if pkg, ok := packages[name]; ok {
		return fmt.Sprintf("sudo dnf install %s", pkg)
	}
	return fmt.Sprintf("sudo dnf install %s", name)
}

func getArchHint(name string) string {
	packages := map[string]string{
		"make":       "base-devel",
		"cc":         "base-devel",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkgconf",
		"tar":        "tar",
		"openssl":    "openssl",
		"libcurl":    "curl",
		"zlib":       "zlib",
		"libxml-2.0": "libxml2",
	}

	if pkg, ok := packages[name]; ok {
		return fmt.Sprintf("sudo pacman -S %s", pkg)
	}
	return fmt.Sprintf("sudo pacman -S %s", name)
}

func getDarwinInstallHint(name string) string {
	packages := map[string]string{
		"make":       "Xcode Command Line Tools (xcode-select --install)",
		"cc":         "Xcode Command Line Tools (xcode-select --install)",
		"autoconf":   "brew install autoconf",
		"bison":      "brew install bison",
		"re2c":       "brew install re2c",
		"pkg-config": "brew install pkg-config",
		"tar":        "Built-in on macOS",
		"openssl":    "brew install openssl",
		"libcurl":    "Built-in on macOS",
		"zlib":       "Built-in on macOS",
		"libxml-2.0": "Built-in on macOS",
	}

	if hint, ok := packages[name]; ok {
		return hint
	}
	return fmt.Sprintf("brew install %s", name)
}

func getWindowsInstallHint(_ string) string { //nolint:unparam // name kept for API consistency with other hint functions
	return `PHP compilation on Windows requires Visual Studio and Windows SDK.
Recommended: Use WSL2 (Windows Subsystem for Linux) for building PHP.
Run: wsl --install
Then follow Linux instructions inside WSL.`
}

// getPackageName returns the package name for a dependency on the given distro.
func getPackageName(name, distro string) string {
	debianPackages := map[string]string{
		"make":       "build-essential",
		"cc":         "build-essential",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkg-config",
		"tar":        "tar",
		"gpg":        "gnupg",
		"curl":       "curl",
		"openssl":    "libssl-dev",
		"libcurl":    "libcurl4-openssl-dev",
		"zlib":       "zlib1g-dev",
		"libxml-2.0": "libxml2-dev",
		"readline":   "libreadline-dev",
		"bz2":        "libbz2-dev",
		"bzip2":      "libbz2-dev",
		"sqlite3":    "libsqlite3-dev",
		"oniguruma":  "libonig-dev",
		"icu-uc":     "libicu-dev", "libpng": "libpng-dev", "libjpeg": "libjpeg-dev", "freetype2": "libfreetype6-dev", "gmp": "libgmp-dev", "iconv": "libc6-dev", "gettext": "gettext", "libpq": "libpq-dev", "libsodium": "libsodium-dev", "libxslt": "libxslt1-dev", "libzip": "libzip-dev",
	}

	fedoraPackages := map[string]string{
		"make":       "make",
		"cc":         "gcc",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkgconfig",
		"tar":        "tar",
		"gpg":        "gnupg2",
		"curl":       "curl",
		"openssl":    "openssl-devel",
		"libcurl":    "libcurl-devel",
		"zlib":       "zlib-devel",
		"libxml-2.0": "libxml2-devel",
		"readline":   "readline-devel",
		"bz2":        "bzip2-devel",
		"bzip2":      "bzip2-devel",
		"sqlite3":    "sqlite-devel",
		"oniguruma":  "oniguruma-devel",
		"icu-uc":     "libicu-devel", "libpng": "libpng-devel", "libjpeg": "libjpeg-turbo-devel", "freetype2": "freetype-devel", "gmp": "gmp-devel", "iconv": "glibc-devel", "gettext": "gettext-devel", "libpq": "libpq-devel", "libsodium": "libsodium-devel", "libxslt": "libxslt-devel", "libzip": "libzip-devel",
	}

	archPackages := map[string]string{
		"make":       "base-devel",
		"cc":         "base-devel",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkgconf",
		"tar":        "tar",
		"gpg":        "gnupg",
		"curl":       "curl",
		"openssl":    "openssl",
		"libcurl":    "curl",
		"zlib":       "zlib",
		"libxml-2.0": "libxml2",
		"readline":   "readline",
		"bz2":        "bzip2",
		"bzip2":      "bzip2",
		"sqlite3":    "sqlite",
		"oniguruma":  "oniguruma",
		"icu-uc":     "icu", "libpng": "libpng", "libjpeg": "libjpeg-turbo", "freetype2": "freetype2", "gmp": "gmp", "iconv": "glibc", "gettext": "gettext", "libpq": "postgresql-libs", "libsodium": "libsodium", "libxslt": "libxslt", "libzip": "libzip",
	}

	var packages map[string]string
	switch distro {
	case "debian", "ubuntu":
		packages = debianPackages
	case "fedora", "rhel", "centos":
		packages = fedoraPackages
	case "arch":
		packages = archPackages
	default:
		return name
	}

	// Strip " (lib)" suffix if present
	cleanName := name
	if len(name) > 6 && name[len(name)-6:] == " (lib)" {
		cleanName = name[:len(name)-6]
	}

	if pkg, ok := packages[cleanName]; ok {
		return pkg
	}
	return cleanName
}

// GetInstallCommand returns a single command to install all missing packages.
func GetInstallCommand(result *DoctorResult) string {
	if runtime.GOOS != "linux" {
		if runtime.GOOS == "darwin" {
			return getMacOSInstallCommand(result)
		}
		return ""
	}

	distro := detectLinuxDistro()
	if distro == "" {
		return ""
	}

	// Collect unique packages
	packageSet := make(map[string]bool)
	for _, check := range result.Checks {
		if needsPackage(check) {
			pkg := getPackageName(check.Name, distro)
			if pkg != "" {
				packageSet[pkg] = true
			}
		}
	}

	if len(packageSet) == 0 {
		return ""
	}

	// Convert to sorted slice for consistent output
	packages := make([]string, 0, len(packageSet))
	for pkg := range packageSet {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)

	// Build command based on distro
	var cmd string
	switch distro {
	case "debian", "ubuntu":
		cmd = "sudo apt-get install -y"
	case "fedora":
		cmd = "sudo dnf install -y"
	case "rhel", "centos":
		cmd = "sudo yum install -y"
	case "arch":
		cmd = "sudo pacman -S --noconfirm"
	default:
		return ""
	}

	for _, pkg := range packages {
		cmd += " " + pkg
	}

	return cmd
}

// getMacOSInstallCommand returns brew install command for macOS.
func getMacOSInstallCommand(result *DoctorResult) string {
	brewPackages := map[string]string{
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkg-config",
		"openssl":    "openssl",
		"libcurl":    "", // Built-in
		"zlib":       "", // Built-in
		"libxml-2.0": "", // Built-in
		"readline":   "readline",
		"bz2":        "", // Built-in
		"bzip2":      "", "icu-uc": "icu4c", "libpng": "libpng", "libjpeg": "jpeg", "freetype2": "freetype", "gmp": "gmp", "iconv": "libiconv", "gettext": "gettext", "libpq": "libpq", "libsodium": "libsodium", "libxslt": "libxslt", "libzip": "libzip", "oniguruma": "oniguruma",
	}

	packages := make([]string, 0)
	for _, check := range result.Checks {
		if needsPackage(check) {
			cleanName := check.Name
			if len(cleanName) > 6 && cleanName[len(cleanName)-6:] == " (lib)" {
				cleanName = cleanName[:len(cleanName)-6]
			}
			if pkg, ok := brewPackages[cleanName]; ok && pkg != "" {
				packages = append(packages, pkg)
			}
		}
	}

	if len(packages) == 0 {
		return ""
	}

	cmd := "brew install"
	for _, pkg := range packages {
		cmd += " " + pkg
	}

	return cmd
}

// FormatResults formats check results for display.
func FormatResults(result *DoctorResult) string {
	var sb strings.Builder

	sb.WriteString("System Requirements Check\n")
	sb.WriteString("=========================\n\n")
	if result.PHPVersion != "" {
		fmt.Fprintf(&sb, "PHP %s; profile: %s\n\n", result.PHPVersion, result.Profile)
	}
	sb.WriteString(result.Environment)

	for _, check := range result.Checks {
		status := "✓"
		if !check.Found {
			if check.Required {
				status = "✗"
			} else {
				status = "!"
			}
		}
		if check.Deferred {
			status = "→"
		}

		_, _ = fmt.Fprintf(&sb, "%s %s", status, check.Name)
		if check.Deferred {
			fmt.Fprintf(&sb, " - DEFERRED: %s\n", check.HelpText)
			continue
		}
		if check.Found {
			if check.Version != "" {
				_, _ = fmt.Fprintf(&sb, " (%s)", check.Version)
			}
			if check.Path != "" {
				fmt.Fprintf(&sb, " [%s]", check.Path)
			}
			sb.WriteString("\n")
		} else {
			if check.Problem != "" {
				sb.WriteString(" - UNUSABLE\n")
				sb.WriteString("    " + strings.ReplaceAll(check.Problem, "\n", "\n    ") + "\n")
				if check.Path != "" {
					label := "selected path"
					if strings.HasSuffix(check.Path, ".pc") {
						label = "pkg-config file"
					}
					_, _ = fmt.Fprintf(&sb, "    %s: %s\n", label, check.Path)
				}
			} else {
				sb.WriteString(" - NOT FOUND\n")
			}
			if check.HelpText != "" {
				_, _ = fmt.Fprintf(&sb, "    %s\n", check.HelpText)
			}
		}
	}

	sb.WriteString("\n")
	for _, check := range result.Checks {
		if check.Problem != "" && check.ProblemKind != "version" {
			sb.WriteString("Check toolchain selection: PATH, CC, PKG_CONFIG, selected .pc files, CPPFLAGS/CFLAGS/LDFLAGS/LIBS. A Homebrew/system toolchain mix or stale /usr/local metadata can cause this; reinstalling packages alone may not fix it.\n\n")
			break
		}
	}

	if result.AllOK && result.Warnings == 0 && result.Deferred == 0 {
		sb.WriteString("All required build checks passed!\n")
	} else if result.AllOK {
		sb.WriteString("All checked required dependencies are usable.\n")
		if result.Warnings > 0 {
			_, _ = fmt.Fprintf(&sb, "Optional missing or unusable: %d\n", result.Warnings)
		}
	} else {
		_, _ = fmt.Fprintf(&sb, "Missing or unusable: %d required, %d optional\n", result.Errors, result.Warnings)
	}
	if result.Deferred > 0 {
		fmt.Fprintf(&sb, "Deferred private libraries: %d (checked by configure after phvm builds them)\n", result.Deferred)
	}

	// Add install command suggestion if anything is missing
	if result.Errors > 0 || result.Warnings > 0 {
		if installCmd := GetInstallCommand(result); installCmd != "" {
			sb.WriteString("\nTo install missing packages, run:\n")
			_, _ = fmt.Fprintf(&sb, "  %s\n", installCmd)
		}
	}

	return redact.Text(sb.String())
}
