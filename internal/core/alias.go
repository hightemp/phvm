package core

import (
	"fmt"
	"os"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
)

// AliasManager manages version aliases.
type AliasManager struct {
	paths *Paths
}

// NewAliasManager creates a new AliasManager.
func NewAliasManager(paths *Paths) *AliasManager {
	return &AliasManager{paths: paths}
}

// Set creates or updates an alias.
func (m *AliasManager) Set(name, version string) error {
	if err := m.validateAliasName(name); err != nil {
		return err
	}

	if err := fsutil.ValidateName(version); err != nil {
		return fmt.Errorf("invalid alias target: %w", err)
	}
	root, err := m.paths.OpenDataDir(m.paths.Alias, true)
	if err != nil {
		return fmt.Errorf("create alias directory: %w", err)
	}
	defer root.Close()

	// Write alias file atomically
	if err := fsutil.AtomicWriteRoot(root, name, []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("write alias file: %w", err)
	}

	return nil
}

// Get retrieves the version for an alias.
func (m *AliasManager) Get(name string) (string, error) {
	if err := m.validateAliasName(name); err != nil {
		return "", err
	}
	root, err := m.paths.OpenDataDir(m.paths.Alias, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("alias not found: %s", name)
		}
		return "", fmt.Errorf("read alias file: %w", err)
	}

	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("empty alias: %s", name)
	}

	return version, nil
}

// Resolve resolves an alias recursively to a version.
// Handles chains like: myalias -> default -> 8.3.30
func (m *AliasManager) Resolve(nameOrVersion string, maxDepth int) (string, error) {
	if maxDepth <= 0 {
		return "", fmt.Errorf("alias resolution loop detected")
	}

	// Try to get as alias first
	version, err := m.Get(nameOrVersion)
	if err != nil {
		// Not an alias, check if it's a valid version
		if IsValidVersionString(nameOrVersion) {
			return nameOrVersion, nil
		}
		// Check if it's a special alias
		if IsSpecialAlias(nameOrVersion) {
			return "", fmt.Errorf("special alias %s needs remote resolution", nameOrVersion)
		}
		return "", fmt.Errorf("not a valid version or alias: %s", nameOrVersion)
	}

	// Recursively resolve
	return m.Resolve(version, maxDepth-1)
}

// Delete removes an alias.
func (m *AliasManager) Delete(name string) error {
	if err := m.validateAliasName(name); err != nil {
		return err
	}
	root, err := m.paths.OpenDataDir(m.paths.Alias, false)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("alias not found: %s", name)
		}
		return fmt.Errorf("remove alias file: %w", err)
	}

	return nil
}

// List returns all defined aliases.
func (m *AliasManager) List() (map[string]string, error) {
	aliases := make(map[string]string)

	root, err := m.paths.OpenDataDir(m.paths.Alias, false)
	if os.IsNotExist(err) {
		return aliases, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		if os.IsNotExist(err) {
			return aliases, nil
		}
		return nil, fmt.Errorf("read alias directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		version, err := m.Get(name)
		if err != nil {
			continue
		}

		aliases[name] = version
	}

	return aliases, nil
}

// Exists checks if an alias exists.
func (m *AliasManager) Exists(name string) bool {
	_, err := m.Get(name)
	return err == nil
}

// GetDefault returns the default alias.
func (m *AliasManager) GetDefault() (string, error) {
	return m.Get("default")
}

// SetDefault sets the default alias.
func (m *AliasManager) SetDefault(version string) error {
	return m.Set("default", version)
}

// validateAliasName validates an alias name.
func (m *AliasManager) validateAliasName(name string) error {
	return fsutil.ValidateName(name)
}

// ResolveVersionOrAlias resolves a version string or alias to a concrete version.
// This is the main entry point for version resolution.
func (m *AliasManager) ResolveVersionOrAlias(input string) (string, bool, error) {
	// Check if it's a special alias that needs remote resolution
	if IsSpecialAlias(input) {
		return input, true, nil
	}

	// Try to resolve as alias
	version, err := m.Resolve(input, 10)
	if err == nil {
		return version, false, nil
	}

	// Try to parse as version
	if IsValidVersionString(input) {
		return input, false, nil
	}

	return "", false, fmt.Errorf("unknown version or alias: %s", input)
}
