package cli

import (
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
)

// getClient creates a remote client with config settings.
func getClient(paths *core.Paths) *remote.Client {
	cfgMgr := core.NewConfigManager(paths)
	cfg := cfgMgr.Get()

	opts := remote.ClientOptions{
		UserAgent: cfg.Remote.UserAgent,
		Timeout:   time.Duration(cfg.Remote.Timeout) * time.Second,
		Retries:   cfg.Remote.Retries,
		Mirror:    cfg.Remote.Mirror,
	}

	return remote.NewClient(opts)
}

// getAPI creates a PHP.net API client.
func getAPI(client *remote.Client) *remote.PHPNetAPI {
	return remote.NewPHPNetAPI(client)
}

// getPECLAPI creates a PECL API client.
func getPECLAPI(client *remote.Client) *remote.PECLAPI {
	return remote.NewPECLAPI(client)
}

// getVerifier creates a verifier with config settings.
func getVerifier(paths *core.Paths, client *remote.Client) *remote.Verifier {
	cfgMgr := core.NewConfigManager(paths)
	cfg := cfgMgr.Get()

	verifier := remote.NewVerifier(client, paths.Downloads)
	verifier.SetGPGEnabled(cfg.Verify.GPG)
	verifier.SetGPGFallback(cfg.Verify.GPGFallbackSHA256)

	return verifier
}

// resolveVersion resolves a version string to a concrete version.
func resolveVersion(paths *core.Paths, client *remote.Client, version string) (string, error) {
	aliases := core.NewAliasManager(paths)

	// Try to resolve as alias first
	resolved, needsRemote, err := aliases.ResolveVersionOrAlias(version)
	if err != nil {
		return "", err
	}

	if !needsRemote {
		return resolved, nil
	}

	// Need to query remote for special aliases
	api := getAPI(client)
	resolvedVersion, _, err := api.ResolveVersion(nil, version)
	if err != nil {
		return "", err
	}

	return resolvedVersion, nil
}

// getCurrentVersion returns the current PHP version.
func getCurrentVersion(paths *core.Paths) (string, error) {
	current := core.NewCurrentManager(paths)
	return current.Get()
}

// ensureDirectories creates all required directories.
func ensureDirectories(paths *core.Paths) error {
	return paths.EnsureDirectories()
}
