package core

import (
	"errors"
	"fmt"
	"strings"
)

// Resolve selects a concrete installed version, following local aliases first.
// Latest and stable mean the newest installed release; no network is used.
func (m *InstalledManager) Resolve(input string) (string, error) {
	aliases := NewAliasManager(m.paths)
	value := strings.TrimSpace(input)
	seen := make(map[string]bool)
	for depth := 0; depth <= 10; depth++ {
		if seen[value] {
			return "", fmt.Errorf("alias cycle detected at %s", value)
		}
		seen[value] = true
		target, err := aliases.Get(value)
		if err == nil {
			value = strings.TrimSpace(target)
			continue
		}
		if !errors.Is(err, ErrAliasNotFound) {
			return "", fmt.Errorf("resolve %s: %w", value, err)
		}
		if strings.EqualFold(value, "latest") || strings.EqualFold(value, "stable") {
			versions, err := m.List()
			if err != nil {
				return "", err
			}
			if len(versions) == 0 {
				return "", fmt.Errorf("no PHP versions installed")
			}
			return versions[0], nil
		}
		if strings.EqualFold(value, "lts") {
			return "", fmt.Errorf("define a local lts alias before using it")
		}
		pattern, err := ParseVersion(value)
		if err != nil {
			return "", fmt.Errorf("unknown installed version or alias %s: %w", value, err)
		}
		if pattern.IsComplete() {
			version := pattern.String()
			if !m.IsInstalled(version) {
				return "", fmt.Errorf("PHP %s is not installed", version)
			}
			return version, nil
		}
		return m.GetLatestInstalled(pattern.String())
	}
	return "", fmt.Errorf("alias chain exceeds 10 links")
}
