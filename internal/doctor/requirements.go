package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/build"
	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/deps"
	"github.com/hightemp/phvm/internal/toolchain"
)

// Options selects the same build plan used by PHP installation.
type Options struct {
	PHPVersion  string
	Profile     string
	CustomFlags []string
	Paths       *core.Paths
	GPGRequired bool
}

// CheckFor checks the selected profile and version without downloading or building PHP.
func CheckFor(ctx context.Context, opts Options) (*DoctorResult, error) {
	if opts.PHPVersion == "" {
		opts.PHPVersion = "8.3"
	}
	if opts.Profile == "" {
		opts.Profile = "common"
	}
	if opts.Profile != "minimal" && opts.Profile != "common" && opts.Profile != "full" {
		return nil, fmt.Errorf("invalid build profile")
	}
	version, err := core.ParseVersion(opts.PHPVersion)
	if err != nil || version.Minor < 0 {
		return nil, fmt.Errorf("doctor requires a PHP version X.Y or X.Y.Z")
	}
	if version.Patch < 0 {
		version.Patch = 0
	}
	if opts.Paths == nil {
		opts.Paths = core.NewPaths("")
	}
	builder := build.NewBuilder(opts.Paths, nil)
	builder.SetProfile(opts.Profile)
	builder.SetCustomFlags(opts.CustomFlags)
	flags := builder.ConfigureFlags(version.Full())
	env := builder.Environment(version.Full())
	probes := requiredLibraries(version, flags)
	needsPkgConfig := false
	for _, probe := range probes {
		if len(probe.modules) > 0 && !explicitLibraryFlags(env, probe) {
			needsPkgConfig = true
		}
	}
	result := &DoctorResult{PHPVersion: opts.PHPVersion, Profile: opts.Profile, Environment: env.Describe(ctx)}
	for _, spec := range []struct {
		key, command string
		required     bool
	}{
		{"", "make", true}, {"CC", "cc", true}, {"", "autoconf", true},
		{"", "bison", false}, {"", "re2c", false}, {"PKG_CONFIG", "pkg-config", needsPkgConfig},
		{"", "gpg", opts.GPGRequired}, {"", "curl", false}, {"", "tar", true},
	} {
		result.Checks = append(result.Checks, checkTool(ctx, env, spec.key, spec.command, spec.required))
	}
	if optionEnabled(flags, "intl", false) {
		result.Checks = append(result.Checks, checkTool(ctx, env, "CXX", "c++", true))
	}
	for _, probe := range probes {
		deferred := false
		for _, dep := range deps.GetRequiredDeps(version.Full()) {
			if dep.Name != "openssl" && dep.Name != "curl" {
				continue
			}
			if probe.name != "openssl" && probe.name != "libcurl" {
				continue
			}
			if (dep.Name == "curl") != (probe.name == "libcurl") {
				continue
			}
			marker := filepath.Join(opts.Paths.Root, "deps", version.Full(), dep.Name, ".phvm-installed")
			if _, err := os.Stat(marker); os.IsNotExist(err) {
				result.Checks = append(result.Checks, CheckResult{Name: probe.name + " (lib)", Required: true, Deferred: true, HelpText: "phvm will build private " + dep.Name + " " + dep.Version + "; configure will check it after the build"})
				deferred = true
			}
		}
		if !deferred {
			result.Checks = append(result.Checks, checkLibrary(ctx, env, probe, true))
		}
	}
	result.AllOK = true
	for _, check := range result.Checks {
		if check.Deferred {
			result.Deferred++
			continue
		}
		if !check.Found {
			if check.Required {
				result.AllOK = false
				result.Errors++
			} else {
				result.Warnings++
			}
		}
	}
	return result, ctx.Err()
}

func checkTool(ctx context.Context, env toolchain.Environment, key, fallback string, required bool) CheckResult {
	result := CheckResult{Name: fallback, Required: required, HelpText: getInstallHint(fallback)}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd, err := env.Command(probeCtx, key, fallback, "--version")
	if err != nil {
		result.Problem = err.Error()
		return result
	}
	result.Path = cmd.Path
	if cmd.Err != nil {
		result.Problem = cmd.Err.Error()
		if env.Value(key, "") == "" {
			result.ProblemKind = "missing"
		} else {
			result.ProblemKind = "toolchain"
			result.HelpText = "Check the configured tool command and PATH."
		}
		return result
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Problem = "tool --version failed: " + err.Error()
		result.ProblemKind = "toolchain"
		result.HelpText = "Check the configured tool command and PATH."
		return result
	}
	result.Found = true
	result.Version = strings.Split(strings.TrimSpace(string(output)), "\n")[0]
	if key == "CC" || key == "CXX" {
		probe := libraryProbe{source: "int main(void) { return 0; }\n", compiler: key}
		if err := linkLibrary(ctx, env, probe); err != nil {
			result.Found = false
			result.Problem = err.Error()
			result.ProblemKind = "toolchain"
			result.HelpText = "The selected compiler cannot link a program; check CC/CXX, PATH, linker and build flags."
		}
	}
	return result
}

func checkLibrary(ctx context.Context, env toolchain.Environment, probe libraryProbe, required bool) CheckResult {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = probeCtx
	result := CheckResult{Name: probe.name + " (lib)", Required: required, HelpText: getInstallHint(probe.name)}
	if explicitLibraryFlags(env, probe) {
		result.Path = probe.prefix + "_CFLAGS / " + probe.prefix + "_LIBS (explicit; version metadata not verified)"
	} else if len(probe.modules) > 0 {
		cmd, err := env.Command(ctx, "PKG_CONFIG", "pkg-config", append([]string{"--exists"}, probe.modules...)...)
		if err != nil {
			result.Problem = err.Error()
			return result
		}
		if err := cmd.Run(); err != nil {
			return result
		}
		cmd, _ = env.Command(ctx, "PKG_CONFIG", "pkg-config", "--modversion", probe.modules[0])
		if output, err := cmd.Output(); err == nil {
			result.Version = strings.TrimSpace(string(output))
		}
		cmd, _ = env.Command(ctx, "PKG_CONFIG", "pkg-config", "--variable=pcfiledir", probe.modules[0])
		if output, err := cmd.Output(); err == nil && strings.TrimSpace(string(output)) != "" {
			result.Path = filepath.Join(strings.TrimSpace(string(output)), probe.modules[0]+".pc")
		}
		if probe.minVersion != "" && !versionAtLeast(result.Version, probe.minVersion) {
			result.ProblemKind = "version"
			result.Problem = fmt.Sprintf("version %s is below required %s", result.Version, probe.minVersion)
			result.HelpText = "Upgrade/select a development library satisfying this PHP version; check PKG_CONFIG_PATH/PKG_CONFIG_LIBDIR."
			return result
		}
		for _, bad := range probe.excluded {
			if result.Version == bad {
				result.ProblemKind = "version"
				result.Problem = "unsupported library version " + bad
				return result
			}
		}
	}
	if err := linkLibrary(ctx, env, probe); err != nil {
		// iconv/gettext are in libc on Linux and separate libraries on macOS.
		if (probe.name == "iconv" || probe.name == "gettext") && probe.libs == "" {
			if probe.name == "iconv" {
				probe.libs = "-liconv"
			} else {
				probe.libs = "-lintl"
			}
			if retryErr := linkLibrary(ctx, env, probe); retryErr == nil {
				result.Found = true
				return result
			}
		}
		result.ProblemKind = "toolchain"
		result.Problem = err.Error()
		result.HelpText = "Check CC, linker and pkg-config selection/flags; headers or libraries may be missing or incompatible."
		return result
	}
	result.Found = true
	return result
}

func explicitLibraryFlags(env toolchain.Environment, probe libraryProbe) bool {
	return probe.prefix != "" && env.Value(probe.prefix+"_CFLAGS", "") != "" && env.Value(probe.prefix+"_LIBS", "") != ""
}

func needsPackage(check CheckResult) bool {
	return !check.Found && !check.Deferred && (check.Problem == "" || check.ProblemKind == "missing" || check.ProblemKind == "version")
}

func optionEnabled(flags []string, option string, fallback bool) bool {
	for _, flag := range flags {
		for _, prefix := range []string{"--with-", "--without-", "--enable-", "--disable-"} {
			name, value, _ := strings.Cut(strings.TrimPrefix(flag, prefix), "=")
			if strings.HasPrefix(flag, prefix) && name == option {
				fallback = (prefix == "--with-" || prefix == "--enable-") && value != "no"
			}
		}
	}
	return fallback
}

// Version floors follow PHP's configure macros; see README sources.
func requiredLibraries(v *core.Version, flags []string) []libraryProbe {
	defaults := true // --disable-all disables default extensions.
	for _, f := range flags {
		if f == "--disable-all" {
			defaults = false
		}
		if f == "--enable-all" {
			defaults = true
		}
	}
	enabled := func(name string, def bool) bool { return optionEnabled(flags, name, def) }
	var probes []libraryProbe
	add := func(name, floor string) {
		p := probeFor(name)
		p.minVersion = floor
		probes = append(probes, p)
	}
	modern := v.Major > 8 || (v.Major == 8 && v.Minor >= 4)
	if enabled("openssl", false) {
		floor := "1.0.1"
		if v.Major >= 8 {
			floor = "1.0.2"
		}
		if modern {
			floor = "1.1.1"
		}
		add("openssl", floor)
	}
	if enabled("curl", false) {
		floor := "7.15.5"
		if v.Major >= 8 {
			floor = "7.29.0"
		}
		if modern {
			floor = "7.61.0"
		}
		add("libcurl", floor)
	}
	if enabled("zlib", false) || enabled("gd", false) {
		floor := "1.2.0.4"
		if modern {
			floor = "1.2.11"
		}
		add("zlib", floor)
	}
	if enabled("libxml", defaults) || enabled("soap", false) || enabled("xsl", false) {
		floor := "2.7.6"
		if v.Major >= 8 {
			floor = "2.9.0"
		}
		if modern {
			floor = "2.9.4"
		}
		add("libxml-2.0", floor)
	}
	if enabled("mbstring", false) && enabled("mbregex", true) && (v.Major > 7 || v.Major == 7 && v.Minor >= 4) {
		add("oniguruma", "")
	}
	if enabled("bz2", false) {
		add("bzip2", "")
	}
	if enabled("readline", false) {
		add("readline", "")
	}
	if (v.Major > 7 || v.Major == 7 && v.Minor >= 4) && (enabled("sqlite3", defaults) || enabled("pdo-sqlite", enabled("pdo", defaults))) {
		floor := "3.7.7"
		if v.Major > 8 || v.Major == 8 && v.Minor >= 5 {
			floor = "3.7.17"
		}
		add("sqlite3", floor)
	}
	if enabled("gd", false) {
		add("libpng", "")
		if enabled("jpeg", false) {
			add("libjpeg", "")
		}
		if enabled("freetype", false) {
			add("freetype2", "")
		}
	}
	if enabled("intl", false) {
		floor := "50.1"
		if v.Major > 8 || v.Major == 8 && v.Minor >= 5 {
			floor = "57.1"
		}
		add("icu-uc", floor)
	}
	if enabled("gmp", false) {
		add("gmp", "")
	}
	if enabled("iconv", defaults) {
		add("iconv", "")
	}
	if enabled("gettext", false) {
		add("gettext", "")
	}
	if enabled("pdo-pgsql", false) || enabled("pgsql", false) {
		floor := ""
		if modern {
			floor = "10.0"
		}
		add("libpq", floor)
	}
	if enabled("sodium", false) {
		add("libsodium", "1.0.8")
	}
	if enabled("xsl", false) {
		add("libxslt", "1.1.0")
	}
	if enabled("zip", false) && (v.Major > 7 || v.Major == 7 && v.Minor >= 4) {
		add("libzip", "0.11")
		probes[len(probes)-1].excluded = []string{"1.3.1", "1.7.0"}
	}
	return probes
}

var numericVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*`)

func versionAtLeast(actual, minimum string) bool {
	a, b := strings.Split(numericVersion.FindString(actual), "."), strings.Split(minimum, ".")
	if a[0] == "" {
		return false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		var err error
		if i < len(a) {
			x, err = strconv.Atoi(a[i])
			if err != nil {
				return false
			}
		}
		if i < len(b) {
			y, _ = strconv.Atoi(b[i])
		}
		if x != y {
			return x > y
		}
	}
	return true
}
