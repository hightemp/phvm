// Package doctor provides system requirements checking.
package doctor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CheckResult represents the result of a dependency check.
type CheckResult struct {
	Name     string
	Found    bool
	Path     string
	Version  string
	Required bool
	HelpText string
}

// DoctorResult holds all check results.
//
//nolint:revive // DoctorResult is more descriptive than just Result
type DoctorResult struct {
	Checks   []CheckResult
	AllOK    bool
	Warnings int
	Errors   int
}

// Check runs all system checks.
func Check() *DoctorResult {
	result := &DoctorResult{}

	// Required dependencies
	result.Checks = append(result.Checks, checkCommand("make", true, getInstallHint("make")))
	result.Checks = append(result.Checks, checkCommand("cc", true, getInstallHint("cc")))
	result.Checks = append(result.Checks, checkCommand("autoconf", true, getInstallHint("autoconf")))
	result.Checks = append(result.Checks, checkCommand("bison", false, getInstallHint("bison")))
	result.Checks = append(result.Checks, checkCommand("re2c", false, getInstallHint("re2c")))
	result.Checks = append(result.Checks, checkCommand("pkg-config", false, getInstallHint("pkg-config")))

	// Optional but recommended
	result.Checks = append(result.Checks, checkCommand("gpg", false, "Optional: for GPG signature verification"))
	result.Checks = append(result.Checks, checkCommand("curl", false, "Optional: for downloading"))
	result.Checks = append(result.Checks, checkCommand("tar", true, getInstallHint("tar")))

	// Common library headers (check pkg-config)
	result.Checks = append(result.Checks, checkPkgConfig("openssl", false))
	result.Checks = append(result.Checks, checkPkgConfig("libcurl", false))
	result.Checks = append(result.Checks, checkPkgConfig("zlib", false))
	result.Checks = append(result.Checks, checkPkgConfig("libxml-2.0", false))

	// Calculate totals
	result.AllOK = true
	for _, check := range result.Checks {
		if !check.Found {
			if check.Required {
				result.Errors++
				result.AllOK = false
			} else {
				result.Warnings++
			}
		}
	}

	return result
}

// checkCommand checks if a command is available.
func checkCommand(name string, required bool, helpText string) CheckResult {
	result := CheckResult{
		Name:     name,
		Required: required,
		HelpText: helpText,
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return result
	}

	result.Found = true
	result.Path = path

	// Try to get version
	result.Version = getCommandVersion(name)

	return result
}

// checkPkgConfig checks if a library is available via pkg-config.
func checkPkgConfig(name string, required bool) CheckResult { //nolint:unparam // required kept for API consistency
	result := CheckResult{
		Name:     name + " (lib)",
		Required: required,
		HelpText: fmt.Sprintf("Install %s development package", name),
	}

	cmd := exec.Command("pkg-config", "--exists", name)
	if err := cmd.Run(); err != nil {
		return result
	}

	result.Found = true

	// Get version
	cmd = exec.Command("pkg-config", "--modversion", name)
	if output, err := cmd.Output(); err == nil {
		result.Version = strings.TrimSpace(string(output))
	}

	return result
}

// getCommandVersion tries to get the version of a command.
func getCommandVersion(name string) string {
	versionFlags := []string{"--version", "-v", "-V", "version"}

	for _, flag := range versionFlags {
		cmd := exec.Command(name, flag)
		output, err := cmd.CombinedOutput()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 {
				// Extract version number from first line
				line := strings.TrimSpace(lines[0])
				// Try to find version pattern
				parts := strings.Fields(line)
				for _, part := range parts {
					if isVersionLike(part) {
						return part
					}
				}
				return line
			}
		}
	}

	return ""
}

// isVersionLike checks if a string looks like a version number.
func isVersionLike(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Check if starts with digit
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	// Check if contains a dot
	return strings.Contains(s, ".")
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
	packages := map[string]string{
		"make":       "build-essential",
		"cc":         "build-essential",
		"autoconf":   "autoconf",
		"bison":      "bison",
		"re2c":       "re2c",
		"pkg-config": "pkg-config",
		"tar":        "tar",
		"openssl":    "libssl-dev",
		"libcurl":    "libcurl4-openssl-dev",
		"zlib":       "zlib1g-dev",
		"libxml-2.0": "libxml2-dev",
	}

	if pkg, ok := packages[name]; ok {
		return fmt.Sprintf("sudo apt-get install %s", pkg)
	}
	return fmt.Sprintf("sudo apt-get install %s", name)
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

// FormatResults formats check results for display.
func FormatResults(result *DoctorResult) string {
	var sb strings.Builder

	sb.WriteString("System Requirements Check\n")
	sb.WriteString("=========================\n\n")

	for _, check := range result.Checks {
		status := "✓"
		if !check.Found {
			if check.Required {
				status = "✗"
			} else {
				status = "!"
			}
		}

		sb.WriteString(fmt.Sprintf("%s %s", status, check.Name))
		if check.Found {
			if check.Version != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", check.Version))
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString(" - NOT FOUND\n")
			if check.HelpText != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", check.HelpText))
			}
		}
	}

	sb.WriteString("\n")

	if result.AllOK {
		sb.WriteString("All required dependencies are installed!\n")
	} else {
		sb.WriteString(fmt.Sprintf("Missing: %d required, %d optional\n", result.Errors, result.Warnings))
	}

	return sb.String()
}
