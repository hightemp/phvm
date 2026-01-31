package core

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Version represents a PHP version.
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

var (
	// versionRegexFull matches full version like "8.3.30"
	versionRegexFull = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	// versionRegexMinor matches minor version like "8.3"
	versionRegexMinor = regexp.MustCompile(`^(\d+)\.(\d+)$`)
	// versionRegexMajor matches major version like "8"
	versionRegexMajor = regexp.MustCompile(`^(\d+)$`)
)

// ParseVersion parses a version string into a Version struct.
func ParseVersion(s string) (*Version, error) {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "php-")
	s = strings.TrimSpace(s)

	if s == "" {
		return nil, fmt.Errorf("empty version string")
	}

	v := &Version{Raw: s}

	// Try full version first
	if m := versionRegexFull.FindStringSubmatch(s); m != nil {
		v.Major, _ = strconv.Atoi(m[1])
		v.Minor, _ = strconv.Atoi(m[2])
		v.Patch, _ = strconv.Atoi(m[3])
		return v, nil
	}

	// Try minor version
	if m := versionRegexMinor.FindStringSubmatch(s); m != nil {
		v.Major, _ = strconv.Atoi(m[1])
		v.Minor, _ = strconv.Atoi(m[2])
		v.Patch = -1 // Indicates "any patch"
		return v, nil
	}

	// Try major version
	if m := versionRegexMajor.FindStringSubmatch(s); m != nil {
		v.Major, _ = strconv.Atoi(m[1])
		v.Minor = -1 // Indicates "any minor"
		v.Patch = -1 // Indicates "any patch"
		return v, nil
	}

	return nil, fmt.Errorf("invalid version format: %s", s)
}

// String returns the version as a string.
func (v *Version) String() string {
	if v.Patch < 0 {
		if v.Minor < 0 {
			return fmt.Sprintf("%d", v.Major)
		}
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Full returns the full version string (X.Y.Z).
func (v *Version) Full() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsComplete returns true if this is a complete version (X.Y.Z).
func (v *Version) IsComplete() bool {
	return v.Patch >= 0 && v.Minor >= 0
}

// IsPartial returns true if this is a partial version (X or X.Y).
func (v *Version) IsPartial() bool {
	return v.Patch < 0 || v.Minor < 0
}

// Matches returns true if the given full version matches this version pattern.
// For example, Version{8, 3, -1} matches "8.3.30".
func (v *Version) Matches(other *Version) bool {
	if v.Major != other.Major {
		return false
	}
	if v.Minor >= 0 && v.Minor != other.Minor {
		return false
	}
	if v.Patch >= 0 && v.Patch != other.Patch {
		return false
	}
	return true
}

// Compare compares two versions.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// LessThan returns true if v < other.
func (v *Version) LessThan(other *Version) bool {
	return v.Compare(other) < 0
}

// GreaterThan returns true if v > other.
func (v *Version) GreaterThan(other *Version) bool {
	return v.Compare(other) > 0
}

// Equal returns true if v == other.
func (v *Version) Equal(other *Version) bool {
	return v.Compare(other) == 0
}

// SortVersions sorts version strings in descending order (newest first).
func SortVersions(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		vi, _ := ParseVersion(versions[i])
		vj, _ := ParseVersion(versions[j])
		if vi == nil || vj == nil {
			return versions[i] > versions[j]
		}
		return vi.GreaterThan(vj)
	})
}

// SortVersionsAsc sorts version strings in ascending order (oldest first).
func SortVersionsAsc(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		vi, _ := ParseVersion(versions[i])
		vj, _ := ParseVersion(versions[j])
		if vi == nil || vj == nil {
			return versions[i] < versions[j]
		}
		return vi.LessThan(vj)
	})
}

// FilterVersions filters versions that match the given pattern.
func FilterVersions(versions []string, pattern *Version) []string {
	var result []string
	for _, vs := range versions {
		v, err := ParseVersion(vs)
		if err != nil {
			continue
		}
		if pattern.Matches(v) {
			result = append(result, vs)
		}
	}
	return result
}

// LatestVersion returns the latest version from a list.
func LatestVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	sorted := make([]string, len(versions))
	copy(sorted, versions)
	SortVersions(sorted)
	return sorted[0]
}

// LatestMatchingVersion returns the latest version matching the pattern.
func LatestMatchingVersion(versions []string, pattern string) (string, error) {
	p, err := ParseVersion(pattern)
	if err != nil {
		return "", err
	}

	matching := FilterVersions(versions, p)
	if len(matching) == 0 {
		return "", fmt.Errorf("no version matching %s found", pattern)
	}

	return LatestVersion(matching), nil
}

// NormalizeVersion normalizes a version string.
func NormalizeVersion(s string) string {
	v, err := ParseVersion(s)
	if err != nil {
		return s
	}
	return v.String()
}

// IsValidVersionString checks if a string is a valid version format.
func IsValidVersionString(s string) bool {
	_, err := ParseVersion(s)
	return err == nil
}

// VersionConstraint represents a version constraint.
type VersionConstraint struct {
	constraint *semver.Constraints
}

// NewVersionConstraint creates a new version constraint.
func NewVersionConstraint(s string) (*VersionConstraint, error) {
	c, err := semver.NewConstraint(s)
	if err != nil {
		return nil, fmt.Errorf("invalid constraint: %w", err)
	}
	return &VersionConstraint{constraint: c}, nil
}

// Check checks if a version satisfies the constraint.
func (c *VersionConstraint) Check(version string) bool {
	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return c.constraint.Check(v)
}

// SpecialVersionAliases are special version identifiers.
var SpecialVersionAliases = []string{"latest", "stable", "lts"}

// IsSpecialAlias checks if a string is a special alias.
func IsSpecialAlias(s string) bool {
	s = strings.ToLower(s)
	for _, alias := range SpecialVersionAliases {
		if s == alias {
			return true
		}
	}
	return false
}
