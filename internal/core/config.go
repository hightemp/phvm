package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/hightemp/phvm/internal/configure"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/redact"
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
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		absent, checkErr := configPathIsAbsent(path)
		if checkErr != nil {
			return nil, fmt.Errorf("read config file: %w", checkErr)
		}
		if !absent {
			return nil, fmt.Errorf("read config file: %w", err)
		}
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	return parseConfig(data)
}

func configPathIsAbsent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("configuration path is not a regular file: %s", path)
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return false, fmt.Errorf("configuration parent is not a directory: %s", dir)
			}
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
		if filepath.Dir(dir) == dir {
			return true, nil
		}
	}
}

func parseConfig(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", redact.Error(err, ""))
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces the configuration schema and mandatory integrity policy.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration is nil")
	}
	switch c.General.DefaultProfile {
	case "minimal", "common", "full":
	default:
		return fmt.Errorf("general.default_profile must be minimal, common or full")
	}
	if c.General.ParallelJobs < 0 {
		return fmt.Errorf("general.parallel_jobs must be nonnegative")
	}
	if c.Remote.Timeout <= 0 || int64(c.Remote.Timeout) > 86400 {
		return fmt.Errorf("remote.timeout must be between 1 and 86400 seconds")
	}
	if c.Remote.Retries < 0 || c.Remote.Retries > 10 {
		return fmt.Errorf("remote.retries must be between 0 and 10")
	}
	u, err := url.Parse(c.Remote.Mirror)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("remote.mirror must be an absolute HTTP(S) base URL without query or fragment")
	}
	if strings.TrimSpace(c.Remote.UserAgent) == "" || strings.ContainsAny(c.Remote.UserAgent, "\r\n\x00") {
		return fmt.Errorf("remote.user_agent must be a nonempty single line")
	}
	if !c.Verify.SHA256 {
		return fmt.Errorf("verify.sha256 must remain true: SHA256 verification is mandatory")
	}
	for _, flag := range c.Build.DefaultFlags {
		if strings.TrimSpace(flag) == "" || strings.ContainsAny(flag, "\r\n\x00") {
			return fmt.Errorf("build.default_flags must contain nonempty single-line arguments")
		}
	}
	if err := configure.ValidateManaged(c.Build.DefaultFlags, false); err != nil {
		return fmt.Errorf("build.default_flags: %w", err)
	}
	return nil
}

// Clone returns an independent configuration snapshot.
func (c *Config) Clone() *Config {
	copy := *c
	copy.Build.DefaultFlags = append([]string{}, c.Build.DefaultFlags...)
	return &copy
}

// ApplyEnvironment applies documented PHVM_* overrides; empty values are unset.
func (c *Config) ApplyEnvironment() error {
	for name, target := range map[string]*string{"PHVM_PROFILE": &c.General.DefaultProfile, "PHVM_MIRROR": &c.Remote.Mirror, "PHVM_USER_AGENT": &c.Remote.UserAgent} {
		if value := os.Getenv(name); value != "" {
			*target = value
		}
	}
	for name, target := range map[string]*int{"PHVM_JOBS": &c.General.ParallelJobs, "PHVM_TIMEOUT": &c.Remote.Timeout, "PHVM_RETRIES": &c.Remote.Retries} {
		if value := os.Getenv(name); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s must be an integer", name)
			}
			*target = parsed
		}
	}
	for name, target := range map[string]*bool{"PHVM_COLOR": &c.General.Color, "PHVM_GPG": &c.Verify.GPG, "PHVM_GPG_FALLBACK_SHA256": &c.Verify.GPGFallbackSHA256} {
		if value := os.Getenv(name); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s must be a boolean", name)
			}
			*target = parsed
		}
	}
	if value := os.Getenv("PHVM_CONFIGURE_FLAGS"); value != "" {
		var flags []string
		if err := json.Unmarshal([]byte(value), &flags); err != nil || strings.TrimSpace(value) == "null" {
			return fmt.Errorf("PHVM_CONFIGURE_FLAGS must be a JSON array of strings")
		}
		c.Build.DefaultFlags = flags
	}
	return c.Validate()
}

// Save saves configuration to a file.
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := fsutil.AtomicWriteFile(path, data, 0600); err != nil {
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

	root, err := m.paths.OpenDataDir(m.paths.Config, false)
	if os.IsNotExist(err) {
		m.config = DefaultConfig()
		return m.config, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open config directory: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(filepath.Base(m.paths.ConfigFile()))
	if os.IsNotExist(err) {
		if _, legacyErr := root.Lstat("config.toml"); legacyErr == nil {
			return nil, fmt.Errorf("legacy config/config.toml detected: move it to config/phvm.toml and use [general], [remote], [verify], [build]")
		} else if !os.IsNotExist(legacyErr) {
			return nil, legacyErr
		}
		m.config = DefaultConfig()
		return m.config, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfg, err := parseConfig(data)
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
func (m *ConfigManager) Get() (*Config, error) { return m.Load() }

// Set sets a new configuration.
func (m *ConfigManager) Set(config *Config) {
	m.config = config
}

// Reset resets to default configuration.
func (m *ConfigManager) Reset() {
	m.config = DefaultConfig()
}
