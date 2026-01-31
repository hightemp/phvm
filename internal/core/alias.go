package core

import (
	"fmt"
	"os"
	"path/filepath"
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

	aliasPath := m.paths.AliasFile(name)

	// Ensure alias directory exists
	if err := os.MkdirAll(m.paths.Alias, 0755); err != nil {
		return fmt.Errorf("create alias directory: %w", err)
	}

	// Write alias file atomically
	if err := fsutil.AtomicWriteFile(aliasPath, []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("write alias file: %w", err)
	}

	return nil
}

// Get retrieves the version for an alias.
func (m *AliasManager) Get(name string) (string, error) {
	aliasPath := m.paths.AliasFile(name)

	data, err := os.ReadFile(aliasPath)
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
	aliasPath := m.paths.AliasFile(name)

	if err := os.Remove(aliasPath); err != nil {
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

	entries, err := os.ReadDir(m.paths.Alias)
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
	aliasPath := m.paths.AliasFile(name)
	return fsutil.Exists(aliasPath)
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
	if name == "" {
		return fmt.Errorf("alias name cannot be empty")
	}

	// Check for invalid characters
	if strings.ContainsAny(name, "/\\:*?\"<>|") {
		return fmt.Errorf("alias name contains invalid characters: %s", name)
	}

	// Check if name starts with dot
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("alias name cannot start with dot: %s", name)
	}

	// Check for path traversal
	if filepath.Clean(name) != name || strings.Contains(name, "..") {
		return fmt.Errorf("invalid alias name: %s", name)
	}

	return nil
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
