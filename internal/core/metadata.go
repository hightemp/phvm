package core

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hightemp/phvm/internal/fsutil"
)

// Metadata holds build and installation metadata for a PHP version.
type Metadata struct {
	Version        string                 `json:"version"`
	InstalledAt    time.Time              `json:"installed_at"`
	SourceURL      string                 `json:"source_url"`
	SHA256         string                 `json:"sha256"`
	GPGVerified    bool                   `json:"gpg_verified"`
	ConfigureFlags []string               `json:"configure_flags"`
	BuildProfile   string                 `json:"build_profile"`
	BuildDuration  int64                  `json:"build_duration_seconds"`
	Extensions     map[string]ExtMetadata `json:"extensions,omitempty"`
}

// ExtMetadata holds metadata for an installed extension.
type ExtMetadata struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
	Enabled     bool      `json:"enabled"`
	SourceURL   string    `json:"source_url,omitempty"`
}

// NewMetadata creates a new Metadata instance.
func NewMetadata(version string) *Metadata {
	return &Metadata{
		Version:     version,
		InstalledAt: time.Now(),
		Extensions:  make(map[string]ExtMetadata),
	}
}

// LoadMetadata loads metadata from a file.
func LoadMetadata(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata file: %w", err)
	}

	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	if m.Extensions == nil {
		m.Extensions = make(map[string]ExtMetadata)
	}

	return &m, nil
}

// Save saves metadata to a file.
func (m *Metadata) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := fsutil.AtomicWriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write metadata file: %w", err)
	}

	return nil
}

// AddExtension adds extension metadata.
func (m *Metadata) AddExtension(name, version string, enabled bool) {
	m.Extensions[name] = ExtMetadata{
		Version:     version,
		InstalledAt: time.Now(),
		Enabled:     enabled,
	}
}

// RemoveExtension removes extension metadata.
func (m *Metadata) RemoveExtension(name string) {
	delete(m.Extensions, name)
}

// SetExtensionEnabled sets the enabled state of an extension.
func (m *Metadata) SetExtensionEnabled(name string, enabled bool) {
	if ext, ok := m.Extensions[name]; ok {
		ext.Enabled = enabled
		m.Extensions[name] = ext
	}
}

// HasExtension checks if an extension is installed.
func (m *Metadata) HasExtension(name string) bool {
	_, ok := m.Extensions[name]
	return ok
}

// GetExtension returns extension metadata.
func (m *Metadata) GetExtension(name string) (ExtMetadata, bool) {
	ext, ok := m.Extensions[name]
	return ext, ok
}
