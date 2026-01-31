// Package build provides PHP build functionality.
package build

// Profile represents a build profile with configure flags.
type Profile struct {
	Name        string
	Description string
	Flags       []string
}

// GetProfile returns a profile by name.
func GetProfile(name string) *Profile {
	switch name {
	case "minimal":
		return MinimalProfile()
	case "common":
		return CommonProfile()
	case "full":
		return FullProfile()
	default:
		return CommonProfile()
	}
}

// MinimalProfile returns a minimal CLI-only build profile.
func MinimalProfile() *Profile {
	return &Profile{
		Name:        "minimal",
		Description: "Minimal CLI-only build",
		Flags: []string{
			"--disable-all",
			"--enable-cli",
			"--disable-cgi",
			"--disable-fpm",
			"--disable-phpdbg",
			"--without-pear",
		},
	}
}

// CommonProfile returns a common build profile with popular extensions.
func CommonProfile() *Profile {
	return &Profile{
		Name:        "common",
		Description: "Common CLI build with popular extensions",
		Flags: []string{
			"--disable-cgi",
			"--disable-fpm",
			"--enable-cli",
			"--enable-bcmath",
			"--enable-calendar",
			"--enable-exif",
			"--enable-ftp",
			"--enable-mbstring",
			"--enable-opcache",
			"--enable-pcntl",
			"--enable-shmop",
			"--enable-soap",
			"--enable-sockets",
			"--enable-sysvmsg",
			"--enable-sysvsem",
			"--enable-sysvshm",
			"--with-bz2",
			"--with-curl",
			"--with-openssl",
			"--with-readline",
			"--with-zlib",
		},
	}
}

// FullProfile returns a full build profile with maximum extensions.
func FullProfile() *Profile {
	return &Profile{
		Name:        "full",
		Description: "Full CLI build with maximum extensions",
		Flags: []string{
			"--disable-cgi",
			"--disable-fpm",
			"--enable-cli",
			"--enable-bcmath",
			"--enable-calendar",
			"--enable-exif",
			"--enable-ftp",
			"--enable-gd",
			"--enable-intl",
			"--enable-mbstring",
			"--enable-opcache",
			"--enable-pcntl",
			"--enable-shmop",
			"--enable-soap",
			"--enable-sockets",
			"--enable-sysvmsg",
			"--enable-sysvsem",
			"--enable-sysvshm",
			"--with-bz2",
			"--with-curl",
			"--with-gettext",
			"--with-gmp",
			"--with-iconv",
			"--with-openssl",
			"--with-pdo-mysql",
			"--with-pdo-pgsql",
			"--with-pdo-sqlite",
			"--with-readline",
			"--with-sodium",
			"--with-xsl",
			"--with-zip",
			"--with-zlib",
		},
	}
}

// ListProfiles returns all available profiles.
func ListProfiles() []*Profile {
	return []*Profile{
		MinimalProfile(),
		CommonProfile(),
		FullProfile(),
	}
}

// MergeFlags merges profile flags with custom flags.
// Custom flags override profile flags with the same option.
func MergeFlags(profile *Profile, custom []string) []string {
	// Start with profile flags
	flags := make([]string, len(profile.Flags))
	copy(flags, profile.Flags)

	// Add custom flags (they take precedence)
	for _, flag := range custom {
		// Check if this flag overrides an existing one
		found := false
		for i, pf := range flags {
			if flagsConflict(pf, flag) {
				flags[i] = flag
				found = true
				break
			}
		}
		if !found {
			flags = append(flags, flag)
		}
	}

	return flags
}

// flagsConflict checks if two configure flags conflict.
func flagsConflict(a, b string) bool {
	// Extract the option name (e.g., "--enable-foo" -> "foo")
	optA := extractOption(a)
	optB := extractOption(b)
	return optA == optB
}

// extractOption extracts the option name from a configure flag.
func extractOption(flag string) string {
	// Handle --enable-X, --disable-X, --with-X, --without-X
	prefixes := []string{"--enable-", "--disable-", "--with-", "--without-"}
	for _, prefix := range prefixes {
		if len(flag) > len(prefix) && flag[:len(prefix)] == prefix {
			// Get everything up to = if present
			opt := flag[len(prefix):]
			for i, c := range opt {
				if c == '=' {
					return opt[:i]
				}
			}
			return opt
		}
	}
	return flag
}
