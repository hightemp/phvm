package ext

import (
	"context"

	"github.com/hightemp/phvm/internal/core"
)

// Extension represents an installed extension.
type Extension struct {
	Name        string
	Version     string
	Enabled     bool
	InstalledBy string // "phvm" or "builtin"
	IniFile     string
	Module      string
	Binary      string
	State       string
	Loaded      bool
	Problem     string
}

// ListInstalled joins runtime modules, managed configuration, metadata and libraries.
func ListInstalled(paths *core.Paths, phpVersion string) ([]Extension, error) {
	return ListInstalledContext(context.Background(), paths, phpVersion)
}

// ListInstalledContext reads a consistent inventory while holding the state lock.
func ListInstalledContext(ctx context.Context, paths *core.Paths, phpVersion string) ([]Extension, error) {
	var result []Extension
	err := paths.WithStateLock(ctx, func(context.Context) error { var err error; result, err = listInventory(paths, phpVersion); return err })
	return result, err
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

// Enable enables the exact extension's loading file.
func Enable(paths *core.Paths, phpVersion, extName string) error {
	return EnableContext(context.Background(), paths, phpVersion, extName)
}

// EnableContext changes exact extension state after acquiring the state lock.
func EnableContext(ctx context.Context, paths *core.Paths, phpVersion, extName string) error {
	return paths.WithStateLock(ctx, func(context.Context) error { return setEnabled(paths, phpVersion, extName, true) })
}

// Disable disables the exact extension's loading file.
func Disable(paths *core.Paths, phpVersion, extName string) error {
	return DisableContext(context.Background(), paths, phpVersion, extName)
}

// DisableContext changes exact extension state after acquiring the state lock.
func DisableContext(ctx context.Context, paths *core.Paths, phpVersion, extName string) error {
	return paths.WithStateLock(ctx, func(context.Context) error { return setEnabled(paths, phpVersion, extName, false) })
}
