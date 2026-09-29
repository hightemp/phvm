package deps

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/configure"
)

// Selected selects private dependencies only for enabled features using defaults.
func (m *DepsManager) Selected(version string, flags []string) ([]Dependency, error) {
	var err error
	version, err = m.paths.CheckVersionPath(version)
	if err != nil {
		return nil, err
	}
	available := GetRequiredDeps(version)
	selected := make(map[string]bool)
	for _, dep := range available {
		value, _ := configure.FeatureValue(flags, dep.Name)
		value = strings.TrimPrefix(value, "shared,")
		if configure.Enabled(flags, dep.Name, false) && (value == "" || value == "yes" || value == "shared" || value == filepath.Join(m.DepsDir(version), dep.Name)) {
			selected[dep.Name] = true
		}
	}
	if selected["curl"] {
		value, set := configure.FeatureValue(flags, "openssl")
		value = strings.TrimPrefix(value, "shared,")
		if set && configure.Enabled(flags, "openssl", false) && value != "" && value != "yes" && value != "shared" && value != filepath.Join(m.DepsDir(version), "openssl") {
			return nil, fmt.Errorf("private curl uses private OpenSSL; with an explicit OpenSSL prefix, select an explicit curl prefix or --without-curl")
		}
		selected["openssl"] = true // curl's build dependency does not enable PHP's openssl.
	}
	var result []Dependency
	for _, dep := range available {
		if selected[dep.Name] {
			result = append(result, dep)
		}
	}
	return result, nil
}

// EnsureSelected builds only dependencies required by the resolved flag plan.
func (m *DepsManager) EnsureSelected(ctx context.Context, version string, flags []string) error {
	selected, err := m.Selected(version, flags)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return nil
	}
	root, err := m.paths.OpenDataDir(m.DepsDir(version), true)
	if err != nil {
		return err
	}
	_ = root.Close()
	for _, dep := range selected {
		if err := m.ensureDep(ctx, dep, m.DepsDir(version)); err != nil {
			return fmt.Errorf("build %s: %w", dep.Name, err)
		}
	}
	return nil
}

// GetSelectedBuildEnv excludes libraries disabled or replaced by explicit prefixes.
func (m *DepsManager) GetSelectedBuildEnv(version string, flags []string) []string {
	selected, err := m.Selected(version, flags)
	if err != nil {
		return nil
	}
	return m.buildEnv(version, selected)
}
