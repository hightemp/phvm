// Package ini provides PHP ini file management.
package ini

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

// PathInfo holds paths to PHP configuration files.
type PathInfo struct {
	PHPIniPath string
	ScanDir    string
	Version    string
}

// GetPaths returns the ini paths for a PHP version.
func GetPaths(paths *core.Paths, version string) *PathInfo {
	return &PathInfo{
		PHPIniPath: paths.VersionPhpIni(version),
		ScanDir:    paths.VersionConfD(version),
		Version:    version,
	}
}

// GetCurrentPaths returns the ini paths for the current PHP version.
func GetCurrentPaths(paths *core.Paths) (*PathInfo, error) {
	current := core.NewCurrentManager(paths)
	version, err := current.Get()
	if err != nil {
		return nil, err
	}
	return GetPaths(paths, version), nil
}

// GetPathsFromPHP queries php for its configuration paths.
func GetPathsFromPHP(phpBin string) (*PathInfo, error) {
	// Get php.ini path
	cmd := exec.Command(phpBin, "-r", "echo php_ini_loaded_file();")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	iniPath := strings.TrimSpace(string(output))

	// Get scan directory
	cmd = exec.Command(phpBin, "-r", "echo PHP_CONFIG_FILE_SCAN_DIR;")
	output, err = cmd.Output()
	if err != nil {
		return nil, err
	}
	scanDir := strings.TrimSpace(string(output))

	return &PathInfo{
		PHPIniPath: iniPath,
		ScanDir:    scanDir,
	}, nil
}

// Exists checks if the ini file exists.
func (p *PathInfo) Exists() bool {
	return fsutil.Exists(p.PHPIniPath)
}

// ScanDirExists checks if the scan directory exists.
func (p *PathInfo) ScanDirExists() bool {
	return fsutil.IsDir(p.ScanDir)
}

// IniFilename returns just the filename from a path.
//
//nolint:revive // IniFilename is more descriptive than just Filename
func IniFilename(path string) string {
	return filepath.Base(path)
}
