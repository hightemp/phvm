package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsInvalidContract(t *testing.T) {
	for _, tt := range []struct{ name, toml string }{
		{"legacy keys", "default_profile='minimal'\njobs=3\nverify_gpg=false\n[configure]\nflags=['--without-curl']"},
		{"unknown key", "[general]\nparallel_job=3"},
		{"unknown table", "[network]\nmirror='https://example.invalid'"},
		{"wrong type", "[general]\nparallel_jobs='three'"},
		{"negative jobs", "[general]\nparallel_jobs=-1"},
		{"bad profile", "[general]\ndefault_profile='commmon'"},
		{"zero timeout", "[remote]\ntimeout=0"},
		{"negative retries", "[remote]\nretries=-1"},
		{"bad mirror", "[remote]\nmirror='not a URL'"},
		{"mirror query", "[remote]\nmirror='https://example.invalid/?token=secret'"},
		{"disabled SHA256", "[verify]\nsha256=false"},
		{"empty flag", "[build]\ndefault_flags=['']"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "phvm.toml")
			if err := os.WriteFile(path, []byte(tt.toml), 0600); err != nil {
				t.Fatal(err)
			}
			if cfg, err := LoadConfig(path); err == nil {
				t.Errorf("invalid configuration accepted: %+v", cfg)
			}
		})
	}
}

func TestREADMEConfigurationExample(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, section, ok := strings.Cut(readme, "\n## Configuration\n")
	if !ok {
		t.Fatal("README Configuration section missing")
	}
	_, example, ok := strings.Cut(section, "```toml\n")
	if !ok {
		t.Fatal("README TOML example missing")
	}
	example, _, ok = strings.Cut(example, "```")
	if !ok {
		t.Fatal("README TOML example is not closed")
	}
	path := filepath.Join(t.TempDir(), "phvm.toml")
	if err := os.WriteFile(path, []byte(example), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("README configuration cannot be loaded: %v", err)
	}
	if cfg.General.DefaultProfile != "common" || cfg.General.ParallelJobs != 4 || !cfg.Verify.SHA256 || !cfg.Verify.GPG || len(cfg.Build.DefaultFlags) != 1 || cfg.Build.DefaultFlags[0] != "--with-pear" {
		t.Errorf("README settings were not applied: %+v", cfg)
	}
	if !strings.Contains(section, "config/phvm.toml") || strings.Contains(section, "Create `~/.phvm/config/config.toml`") {
		t.Error("README must use the canonical configuration file")
	}
}

func TestConfigManagerGetReturnsParseError(t *testing.T) {
	p := NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile(), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := NewConfigManager(p).Get(); err == nil || cfg != nil {
		t.Errorf("Get hid parse error: %+v %v", cfg, err)
	}
}

func TestLoadConfigDoesNotHideReadErrors(t *testing.T) {
	dir := t.TempDir()
	block := filepath.Join(dir, "not-directory")
	if err := os.WriteFile(block, []byte("marker"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadConfig(filepath.Join(block, "phvm.toml")); err == nil {
		t.Errorf("read error silently replaced with %+v", cfg)
	}
}

func TestConfigManagerDetectsLegacyFile(t *testing.T) {
	p := NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Config, "config.toml"), []byte("[general]\ndefault_profile='minimal'"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := NewConfigManager(p).Load(); err == nil {
		t.Errorf("legacy file silently ignored: %+v", cfg)
	}
}

func TestConfigSaveRejectsInvalidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phvm.toml")
	if err := os.WriteFile(path, []byte("marker"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.General.ParallelJobs = -1
	if err := cfg.Save(path); err == nil {
		t.Error("invalid state saved")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "marker" {
		t.Errorf("previous config overwritten: %q %v", data, err)
	}
}

func TestNestedConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phvm.toml")
	data := "[general]\ndefault_profile='minimal'\nparallel_jobs=3\ncolor=false\n[remote]\nmirror='https://example.invalid'\nuser_agent='phvm-test'\ntimeout=12\nretries=0\n[verify]\ngpg=false\ngpg_fallback_sha256=false\n[build]\ndefault_flags=['--without-curl']\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.DefaultProfile != "minimal" || cfg.General.ParallelJobs != 3 || cfg.General.Color || cfg.Remote.Timeout != 12 || cfg.Remote.Retries != 0 || cfg.Verify.GPG || cfg.Verify.GPGFallbackSHA256 || len(cfg.Build.DefaultFlags) != 1 {
		t.Fatalf("settings not applied: %+v", cfg)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if again, err := LoadConfig(path); err != nil || again.General.ParallelJobs != 3 || again.General.Color || again.Verify.GPG {
		t.Fatalf("roundtrip: %+v %v", again, err)
	}
}
