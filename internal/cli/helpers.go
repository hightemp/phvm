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

// ensureDirectories creates all required directories.
func ensureDirectories(paths *core.Paths) error {
	return paths.EnsureDirectories()
}
