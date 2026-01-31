package core

import (
	"fmt"
	"os"

	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/pelletier/go-toml/v2"
)

// Config holds phvm configuration.
type Config struct {
	General GeneralConfig `toml:"general"`
	Remote  RemoteConfig  `toml:"remote"`
	Verify  VerifyConfig  `toml:"verify"`
	Build   BuildConfig   `toml:"build"`
}

// GeneralConfig holds general settings.
type GeneralConfig struct {
	DefaultProfile string `toml:"default_profile"`
	ParallelJobs   int    `toml:"parallel_jobs"`
	Color          bool   `toml:"color"`
}

// RemoteConfig holds remote/download settings.
type RemoteConfig struct {
	Mirror    string `toml:"mirror"`
	UserAgent string `toml:"user_agent"`
	Timeout   int    `toml:"timeout"`
	Retries   int    `toml:"retries"`
}

// VerifyConfig holds verification settings.
type VerifyConfig struct {
	SHA256            bool `toml:"sha256"`
	GPG               bool `toml:"gpg"`
	GPGFallbackSHA256 bool `toml:"gpg_fallback_sha256"`
}

// BuildConfig holds build settings.
type BuildConfig struct {
	DefaultFlags []string `toml:"default_flags"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		General: GeneralConfig{
			DefaultProfile: "common",
			ParallelJobs:   0, // 0 means auto-detect
			Color:          true,
		},
		Remote: RemoteConfig{
			Mirror:    "https://www.php.net",
			UserAgent: "phvm/1.0.0",
			Timeout:   60,
			Retries:   3,
		},
		Verify: VerifyConfig{
			SHA256:            true,
			GPG:               true,
			GPGFallbackSHA256: true,
		},
		Build: BuildConfig{
			DefaultFlags: []string{},
		},
	}
}

// LoadConfig loads configuration from a file.
func LoadConfig(path string) (*Config, error) {
	if !fsutil.Exists(path) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

// Save saves configuration to a file.
func (c *Config) Save(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := fsutil.AtomicWriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// ConfigManager manages phvm configuration.
type ConfigManager struct {
	paths  *Paths
	config *Config
}

// NewConfigManager creates a new ConfigManager.
func NewConfigManager(paths *Paths) *ConfigManager {
	return &ConfigManager{paths: paths}
}

// Load loads the configuration.
func (m *ConfigManager) Load() (*Config, error) {
	if m.config != nil {
		return m.config, nil
	}

	cfg, err := LoadConfig(m.paths.ConfigFile())
	if err != nil {
		return nil, err
	}

	m.config = cfg
	return cfg, nil
}

// Save saves the configuration.
func (m *ConfigManager) Save() error {
	if m.config == nil {
		return nil
	}

	return m.config.Save(m.paths.ConfigFile())
}

// Get returns the current configuration.
// Loads from file if not already loaded.
func (m *ConfigManager) Get() *Config {
	cfg, err := m.Load()
	if err != nil {
		return DefaultConfig()
	}
	return cfg
}

// Set sets a new configuration.
func (m *ConfigManager) Set(config *Config) {
	m.config = config
}

// Reset resets to default configuration.
func (m *ConfigManager) Reset() {
	m.config = DefaultConfig()
}
