// Package core provides core functionality for phvm.
package core

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds all phvm directory paths.
type Paths struct {
	Root       string // ~/.phvm
	Versions   string // ~/.phvm/versions/php
	Current    string // ~/.phvm/current (symlink)
	Alias      string // ~/.phvm/alias
	Cache      string // ~/.phvm/cache
	Downloads  string // ~/.phvm/cache/downloads
	Sources    string // ~/.phvm/cache/sources
	Build      string // ~/.phvm/cache/build
	Extensions string // ~/.phvm/cache/extensions
	Config     string // ~/.phvm/config
	Profiles   string // ~/.phvm/config/ini-profiles
	Logs       string // ~/.phvm/logs
	Bin        string // ~/.phvm/bin
}

// DefaultRoot returns the default phvm root directory.
func DefaultRoot() string {
	// Check PHVM_DIR environment variable first
	if dir := os.Getenv("PHVM_DIR"); dir != "" {
		return dir
	}

	// Use XDG_DATA_HOME if set (Linux/macOS)
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "phvm")
	}

	// Default to ~/.phvm
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory in worst case
		return ".phvm"
	}

	return filepath.Join(home, ".phvm")
}

// NewPaths creates a new Paths instance with the given root.
func NewPaths(root string) *Paths {
	if root == "" {
		root = DefaultRoot()
	}

	return &Paths{
		Root:       root,
		Versions:   filepath.Join(root, "versions", "php"),
		Current:    filepath.Join(root, "current"),
		Alias:      filepath.Join(root, "alias"),
		Cache:      filepath.Join(root, "cache"),
		Downloads:  filepath.Join(root, "cache", "downloads"),
		Sources:    filepath.Join(root, "cache", "sources"),
		Build:      filepath.Join(root, "cache", "build"),
		Extensions: filepath.Join(root, "cache", "extensions"),
		Config:     filepath.Join(root, "config"),
		Profiles:   filepath.Join(root, "config", "ini-profiles"),
		Logs:       filepath.Join(root, "logs"),
		Bin:        filepath.Join(root, "bin"),
	}
}

// VersionDir returns the directory for a specific PHP version.
func (p *Paths) VersionDir(version string) string {
	return filepath.Join(p.Versions, version)
}

// VersionBin returns the bin directory for a specific PHP version.
func (p *Paths) VersionBin(version string) string {
	return filepath.Join(p.VersionDir(version), "bin")
}

// VersionEtc returns the etc directory for a specific PHP version.
func (p *Paths) VersionEtc(version string) string {
	return filepath.Join(p.VersionDir(version), "etc")
}

// VersionConfD returns the conf.d directory for a specific PHP version.
func (p *Paths) VersionConfD(version string) string {
	return filepath.Join(p.VersionEtc(version), "conf.d")
}

// VersionPhpIni returns the php.ini path for a specific PHP version.
func (p *Paths) VersionPhpIni(version string) string {
	return filepath.Join(p.VersionEtc(version), "php.ini")
}

// VersionMetadata returns the metadata file path for a specific PHP version.
func (p *Paths) VersionMetadata(version string) string {
	return filepath.Join(p.VersionDir(version), ".phvm-metadata.json")
}

// AliasFile returns the alias file path.
func (p *Paths) AliasFile(name string) string {
	return filepath.Join(p.Alias, name)
}

// ProfileDir returns the directory for a specific ini profile.
func (p *Paths) ProfileDir(name string) string {
	return filepath.Join(p.Profiles, name)
}

// ConfigFile returns the main config file path.
func (p *Paths) ConfigFile() string {
	return filepath.Join(p.Config, "phvm.toml")
}

// DownloadPath returns the path for a downloaded file.
func (p *Paths) DownloadPath(filename string) string {
	return filepath.Join(p.Downloads, filename)
}

// SourcePath returns the path for extracted sources.
func (p *Paths) SourcePath(version string) string {
	return filepath.Join(p.Sources, "php-"+version)
}

// BuildPath returns the path for build directory.
func (p *Paths) BuildPath(version string) string {
	return filepath.Join(p.Build, "php-"+version)
}

// ExtensionCachePath returns the path for a cached extension.
func (p *Paths) ExtensionCachePath(name, version string) string {
	return filepath.Join(p.Extensions, name+"-"+version+".tgz")
}

// LockFile returns the path to the global lock file.
func (p *Paths) LockFile() string {
	return filepath.Join(p.Root, ".lock")
}

// InstallLockFile returns the path to the install lock file for a version.
func (p *Paths) InstallLockFile(version string) string {
	return filepath.Join(p.Cache, ".install-"+version+".lock")
}

// LogFile returns the path for an install log.
func (p *Paths) LogFile(name string) string {
	return filepath.Join(p.Logs, name+".log")
}

// EnsureDirectories creates all necessary directories.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Root,
		p.Versions,
		p.Alias,
		p.Cache,
		p.Downloads,
		p.Sources,
		p.Build,
		p.Extensions,
		p.Config,
		p.Profiles,
		p.Logs,
		p.Bin,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

// PHPBinary returns the php binary name for the current OS.
func PHPBinary() string {
	if runtime.GOOS == "windows" {
		return "php.exe"
	}
	return "php"
}

// DefaultPaths returns paths with the default root.
var DefaultPaths = NewPaths("")
