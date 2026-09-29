package build

import (
	"fmt"
	"strings"

	"github.com/hightemp/phvm/internal/configure"
)

func profileFlagsForVersion(profile *Profile, version string) []string {
	var major, minor int
	_, _ = fmt.Sscanf(version, "%d.%d", &major, &minor)
	before := func(m, n int) bool { return major < m || major == m && minor < n }
	var flags []string
	for _, flag := range profile.Flags {
		name := extractOption(flag)
		if name == "phpdbg" && before(5, 6) || name == "opcache" && before(5, 5) || name == "sodium" && before(7, 2) || name == "fpm" && before(5, 3) {
			continue
		}
		if before(7, 4) {
			if flag == "--enable-gd" {
				flag = "--with-gd"
			}
			if flag == "--with-zip" {
				flag = "--enable-zip"
			}
		}
		flags = append(flags, flag)
	}
	return flags
}

func validateFlagsForVersion(flags []string, version string) error {
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return fmt.Errorf("invalid PHP version for configure flags")
	}
	before := func(m, n int) bool { return major < m || major == m && minor < n }
	for _, flag := range flags {
		key := configure.Key(flag)
		if key == "enable:gd" && before(7, 4) {
			return fmt.Errorf("PHP %s uses --with-gd/--without-gd, not --enable-gd", version)
		}
		if key == "with:gd" && !before(7, 4) {
			return fmt.Errorf("PHP %s uses --enable-gd/--disable-gd, not --with-gd", version)
		}
		if key == "with:zip" && before(7, 4) {
			return fmt.Errorf("PHP %s uses --enable-zip/--disable-zip", version)
		}
		if key == "enable:zip" && !before(7, 4) {
			return fmt.Errorf("PHP %s uses --with-zip/--without-zip", version)
		}
		if key == "enable:opcache" && before(5, 5) || key == "enable:phpdbg" && before(5, 6) || key == "with:sodium" && before(7, 2) {
			return fmt.Errorf("configure option %s is unavailable for PHP %s", flag, version)
		}
		if key == "enable:json" && major >= 8 {
			return fmt.Errorf("JSON is built into PHP %s; remove %s", version, flag)
		}
	}
	return nil
}

func explicitConfigureArguments(flags ...[]string) []string {
	args := configure.Merge(flags...)
	var options []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			options = append(options, arg)
		}
	}
	return options
}
