// Package configure defines configure argv parsing, merging and validation.
package configure

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hightemp/phvm/internal/toolchain"
)

var optionName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Parse splits legacy arguments without executing a shell or expanding variables.
func Parse(value string) ([]string, error) {
	args, err := toolchain.SplitArguments(value)
	if err != nil {
		return nil, fmt.Errorf("configure arguments: %w", err)
	}
	if err := Validate(args); err != nil {
		return nil, err
	}
	return args, nil
}

// Validate requires one complete long option or environment assignment per entry.
func Validate(args []string) error {
	for _, arg := range args {
		if strings.TrimSpace(arg) != arg || arg == "" || strings.ContainsAny(arg, "\r\n\x00") {
			return fmt.Errorf("invalid configure argument %q", arg)
		}
		name, _, assignment := strings.Cut(arg, "=")
		if strings.HasPrefix(name, "--") {
			if !optionName.MatchString(strings.TrimPrefix(name, "--")) {
				return fmt.Errorf("invalid configure option %q; use --name=value for values", arg)
			}
			for _, prefix := range []string{"--enable-", "--disable-", "--with-", "--without-"} {
				if name == prefix {
					return fmt.Errorf("configure feature name is empty: %q", arg)
				}
			}
		} else if !assignment || !variableName.MatchString(name) {
			return fmt.Errorf("invalid configure argument %q; expected --option or NAME=value", arg)
		}
	}
	return nil
}

// Key distinguishes enable/disable, with/without and plain option namespaces.
func Key(arg string) string {
	name, _, _ := strings.Cut(arg, "=")
	for _, prefix := range []string{"--enable-", "--disable-"} {
		if strings.HasPrefix(name, prefix) {
			return "enable:" + strings.TrimPrefix(name, prefix)
		}
	}
	for _, prefix := range []string{"--with-", "--without-"} {
		if strings.HasPrefix(name, prefix) {
			return "with:" + strings.TrimPrefix(name, prefix)
		}
	}
	return name
}

// Merge applies layers in ascending priority; the last value for an option wins.
func Merge(layers ...[]string) []string {
	var out []string
	positions := make(map[string]int)
	for _, layer := range layers {
		for _, arg := range layer {
			key := Key(arg)
			if index, ok := positions[key]; ok {
				out[index] = arg
			} else {
				positions[key] = len(out)
				out = append(out, arg)
			}
		}
	}
	return out
}

// Enabled returns the final explicit selection for a feature.
func Enabled(args []string, feature string, fallback bool) bool {
	for _, arg := range args {
		for _, prefix := range []string{"--enable-", "--disable-", "--with-", "--without-"} {
			name, value, _ := strings.Cut(strings.TrimPrefix(arg, prefix), "=")
			if strings.HasPrefix(arg, prefix) && name == feature {
				fallback = (prefix == "--enable-" || prefix == "--with-") && value != "no"
			}
		}
	}
	return fallback
}

// FeatureValue returns the final with/enable value, retaining whether it was set.
func FeatureValue(args []string, feature string) (string, bool) {
	var value string
	set := false
	for _, arg := range args {
		for _, prefix := range []string{"--with-", "--enable-"} {
			name, v, _ := strings.Cut(strings.TrimPrefix(arg, prefix), "=")
			if strings.HasPrefix(arg, prefix) && name == feature {
				value, set = v, true
			}
		}
	}
	return value, set
}

// ValidateManaged rejects options that redirect phvm installation or SDK paths.
func ValidateManaged(args []string, extension bool) error {
	if err := Validate(args); err != nil {
		return err
	}
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		for _, reserved := range []string{"--help", "--version", "--no-create", "--prefix", "--exec-prefix", "--bindir", "--sbindir", "--libdir", "--libexecdir", "--includedir", "--oldincludedir", "--datadir", "--datarootdir", "--mandir", "--sysconfdir", "--localstatedir", "--with-config-file-path", "--with-config-file-scan-dir", "--with-php-config", "--without-php-config"} {
			if name == reserved {
				return fmt.Errorf("configure option %s is managed by phvm", name)
			}
		}
		if !extension && (arg == "--disable-cli" || arg == "--enable-cli=no") {
			return fmt.Errorf("phvm requires the PHP CLI; cannot disable it")
		}
	}
	return nil
}
