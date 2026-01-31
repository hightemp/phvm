package ext

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/ini"
)

// Extension represents an installed extension.
type Extension struct {
	Name        string
	Version     string
	Enabled     bool
	InstalledBy string // "phvm" or "builtin"
	IniFile     string
}

// ListInstalled lists installed extensions for a PHP version.
func ListInstalled(paths *core.Paths, phpVersion string) ([]Extension, error) {
	phpDir := paths.VersionDir(phpVersion)
	phpBin := filepath.Join(phpDir, "bin", core.PHPBinary())

	if !fsutil.Exists(phpBin) {
		return nil, nil
	}

	// Get all loaded extensions from php -m
	cmd := exec.Command(phpBin, "-m")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse output
	lines := strings.Split(string(output), "\n")
	extensionMap := make(map[string]bool)
	inZend := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "[PHP Modules]" {
			inZend = false
			continue
		}
		if line == "[Zend Modules]" {
			inZend = true
			continue
		}
		if line[0] == '[' {
			continue
		}
		extensionMap[line] = !inZend // true = PHP module, false = Zend module
	}

	// Get metadata to check which were installed by phvm
	metadataPath := paths.VersionMetadata(phpVersion)
	var metadata *core.Metadata
	if fsutil.Exists(metadataPath) {
		metadata, _ = core.LoadMetadata(metadataPath)
	}

	// Get conf.d files
	confD := paths.VersionConfD(phpVersion)
	confDMgr := ini.NewConfDManager(confD)
	iniFiles, _ := confDMgr.List()

	// Build extension list
	var extensions []Extension
	for name := range extensionMap {
		ext := Extension{
			Name:        name,
			Enabled:     true,
			InstalledBy: "builtin",
		}

		// Check if installed by phvm
		if metadata != nil {
			if meta, ok := metadata.GetExtension(name); ok {
				ext.Version = meta.Version
				ext.InstalledBy = "phvm"
				ext.Enabled = meta.Enabled
			}
		}

		// Find ini file
		for _, f := range iniFiles {
			if strings.Contains(strings.ToLower(f.Name), strings.ToLower(name)) {
				ext.IniFile = f.Name
				ext.Enabled = f.Enabled
				break
			}
		}

		extensions = append(extensions, ext)
	}

	return extensions, nil
}

// ListRemote lists available PECL extensions.
func ListRemote(ctx context.Context, client *Client, search string, limit int) ([]string, error) {
	peclAPI := NewPECLAPI(client)

	if search != "" {
		return peclAPI.SearchPackages(ctx, search)
	}

	packages, err := peclAPI.ListPackages(ctx)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(packages) > limit {
		packages = packages[:limit]
	}

	return packages, nil
}

// Client is an alias for remote.Client for convenience.
type Client = struct{}

// NewPECLAPI creates a new PECL API - placeholder for import.
func NewPECLAPI(client *Client) interface {
	SearchPackages(ctx context.Context, query string) ([]string, error)
	ListPackages(ctx context.Context) ([]string, error)
} {
	return nil // Will use actual remote.PECLAPI in CLI
}

// Enable enables an extension.
func Enable(paths *core.Paths, phpVersion, extName string) error {
	confD := paths.VersionConfD(phpVersion)
	confDMgr := ini.NewConfDManager(confD)

	// Find the ini file for this extension
	files, err := confDMgr.List()
	if err != nil {
		return err
	}

	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Name), strings.ToLower(extName)) {
			if err := confDMgr.Enable(f.Name); err != nil {
				return err
			}

			// Update metadata
			updateMetadataEnabled(paths, phpVersion, extName, true)
			return nil
		}
	}

	return nil
}

// Disable disables an extension.
func Disable(paths *core.Paths, phpVersion, extName string) error {
	confD := paths.VersionConfD(phpVersion)
	confDMgr := ini.NewConfDManager(confD)

	// Find the ini file for this extension
	files, err := confDMgr.List()
	if err != nil {
		return err
	}

	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Name), strings.ToLower(extName)) {
			if err := confDMgr.Disable(f.Name); err != nil {
				return err
			}

			// Update metadata
			updateMetadataEnabled(paths, phpVersion, extName, false)
			return nil
		}
	}

	return nil
}

// updateMetadataEnabled updates the enabled state in metadata.
func updateMetadataEnabled(paths *core.Paths, phpVersion, extName string, enabled bool) {
	metadataPath := paths.VersionMetadata(phpVersion)
	if fsutil.Exists(metadataPath) {
		metadata, err := core.LoadMetadata(metadataPath)
		if err == nil {
			metadata.SetExtensionEnabled(extName, enabled)
			metadata.Save(metadataPath)
		}
	}
}
