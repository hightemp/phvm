package remote

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNativePECLChannelArchiveValidation(t *testing.T) {
	if os.Getenv("PHVM_PECL_TRUST_NATIVE") != "1" {
		t.Skip("opt-in read-only PECL download; no PHP or build code is executed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := NewClient(ClientOptions{Retries: 0, Timeout: 30 * time.Second})
	api := NewPECLAPI(client)
	release, err := api.GetRelease(ctx, "redis", "6.0.0")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := api.GetManifest(ctx, "redis", "6.0.0")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDownloader(client, t.TempDir())
	d.SetShowProgress(false)
	path, err := d.DownloadChecked(ctx, api.GetDownloadURL("redis", "6.0.0"), "redis-6.0.0.tgz", DownloadChecks{Size: release.FileSize, Validate: func(ctx context.Context, path string) error {
		return ValidatePECLArchive(ctx, path, "redis", "6.0.0", manifest)
	}})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := NewVerifier(client, "").ComputeSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PECL redis 6.0.0 checked: %d source files, %d archive bytes, SHA256 %s", len(manifest.Files), release.FileSize, hash)
}
