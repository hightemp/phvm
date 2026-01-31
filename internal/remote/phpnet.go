package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hightemp/phvm/internal/core"
)

// PHPNetAPI provides access to php.net releases API.
type PHPNetAPI struct {
	client *Client
}

// NewPHPNetAPI creates a new PHPNetAPI.
func NewPHPNetAPI(client *Client) *PHPNetAPI {
	return &PHPNetAPI{client: client}
}

// Release represents a PHP release from the API.
type Release struct {
	Version           string          `json:"version"`
	Date              string          `json:"date"`
	Source            []ReleaseSource `json:"source"`
	Announcement      interface{}     `json:"announcement"`
	Tags              []string        `json:"tags"`
	SupportedVersions []string        `json:"supported_versions"`
	Museum            bool            `json:"museum"`
}

// ReleaseSource represents a source tarball.
type ReleaseSource struct {
	Filename string `json:"filename"`
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
	MD5      string `json:"md5"`
	Date     string `json:"date"`
}

// GetLatestReleases returns the latest release for each major version.
func (api *PHPNetAPI) GetLatestReleases(ctx context.Context) (map[string]*Release, error) {
	url := fmt.Sprintf("%s/releases/?json", api.client.Mirror())

	resp, err := api.client.GetJSON(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// The API returns a map keyed by major version
	var rawReleases map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawReleases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	releases := make(map[string]*Release)
	for major, raw := range rawReleases {
		var release Release
		if err := json.Unmarshal(raw, &release); err != nil {
			continue
		}
		releases[major] = &release
	}

	return releases, nil
}

// GetRelease returns a specific release by version.
func (api *PHPNetAPI) GetRelease(ctx context.Context, version string) (*Release, error) {
	url := fmt.Sprintf("%s/releases/?json&version=%s", api.client.Mirror(), version)

	resp, err := api.client.GetJSON(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Check for error response
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return nil, fmt.Errorf("version not found: %s", version)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}

	// If version is not set in response (happens with multi-version requests),
	// use the requested version
	if release.Version == "" {
		release.Version = version
	}

	return &release, nil
}

// GetReleasesByMajor returns multiple releases for a major version.
func (api *PHPNetAPI) GetReleasesByMajor(ctx context.Context, major string, max int) (map[string]*Release, error) {
	url := fmt.Sprintf("%s/releases/?json&version=%s&max=%d", api.client.Mirror(), major, max)

	resp, err := api.client.GetJSON(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Check for error
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return nil, fmt.Errorf("version not found: %s", major)
	}

	// Response is keyed by full version
	var rawReleases map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawReleases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	releases := make(map[string]*Release)
	for version, raw := range rawReleases {
		var release Release
		if err := json.Unmarshal(raw, &release); err != nil {
			continue
		}
		if release.Version == "" {
			release.Version = version
		}
		releases[version] = &release
	}

	return releases, nil
}

// ResolveVersion resolves a partial version to a full version.
func (api *PHPNetAPI) ResolveVersion(ctx context.Context, version string) (string, *Release, error) {
	// Parse the version
	v, err := core.ParseVersion(version)
	if err != nil {
		return "", nil, err
	}

	// If it's a complete version, just fetch it
	if v.IsComplete() {
		release, err := api.GetRelease(ctx, v.String())
		if err != nil {
			return "", nil, err
		}
		return release.Version, release, nil
	}

	// For partial versions, query the API
	release, err := api.GetRelease(ctx, version)
	if err != nil {
		return "", nil, err
	}

	return release.Version, release, nil
}

// GetSupportedVersions returns currently supported PHP versions.
func (api *PHPNetAPI) GetSupportedVersions(ctx context.Context) ([]string, error) {
	releases, err := api.GetLatestReleases(ctx)
	if err != nil {
		return nil, err
	}

	var supported []string
	for _, release := range releases {
		if len(release.SupportedVersions) > 0 {
			for _, v := range release.SupportedVersions {
				// Avoid duplicates
				found := false
				for _, s := range supported {
					if s == v {
						found = true
						break
					}
				}
				if !found {
					supported = append(supported, v)
				}
			}
			break // All releases have the same supported_versions
		}
	}

	return supported, nil
}

// GetAvailableVersions returns all available versions from the API.
func (api *PHPNetAPI) GetAvailableVersions(ctx context.Context, limit int) ([]string, error) {
	var allVersions []string

	// Get major versions from latest releases
	releases, err := api.GetLatestReleases(ctx)
	if err != nil {
		return nil, err
	}

	// For each major version, get multiple releases
	for major := range releases {
		majorReleases, err := api.GetReleasesByMajor(ctx, major, limit)
		if err != nil {
			continue
		}

		for version := range majorReleases {
			allVersions = append(allVersions, version)
		}
	}

	core.SortVersions(allVersions)
	return allVersions, nil
}

// GetTarballInfo returns the download info for a version.
func (api *PHPNetAPI) GetTarballInfo(ctx context.Context, version string) (*TarballInfo, error) {
	release, err := api.GetRelease(ctx, version)
	if err != nil {
		return nil, err
	}

	// Find the .tar.xz source
	for _, src := range release.Source {
		if strings.HasSuffix(src.Filename, ".tar.xz") {
			return &TarballInfo{
				Version:  release.Version,
				Filename: src.Filename,
				URL:      fmt.Sprintf("%s/distributions/%s", api.client.Mirror(), src.Filename),
				SHA256:   src.SHA256,
				ASCURL:   fmt.Sprintf("%s/distributions/%s.asc", api.client.Mirror(), src.Filename),
			}, nil
		}
	}

	// Fallback to .tar.gz
	for _, src := range release.Source {
		if strings.HasSuffix(src.Filename, ".tar.gz") {
			return &TarballInfo{
				Version:  release.Version,
				Filename: src.Filename,
				URL:      fmt.Sprintf("%s/distributions/%s", api.client.Mirror(), src.Filename),
				SHA256:   src.SHA256,
				ASCURL:   fmt.Sprintf("%s/distributions/%s.asc", api.client.Mirror(), src.Filename),
			}, nil
		}
	}

	return nil, fmt.Errorf("no suitable tarball found for version %s", version)
}

// TarballInfo holds information about a PHP tarball.
type TarballInfo struct {
	Version  string
	Filename string
	URL      string
	SHA256   string
	ASCURL   string
}

// KeyringURL returns the URL to the PHP keyring.
func (api *PHPNetAPI) KeyringURL() string {
	return fmt.Sprintf("%s/distributions/php-keyring.gpg", api.client.Mirror())
}

// DefaultPHPNetAPI returns a PHPNetAPI with the default client.
var DefaultPHPNetAPI = NewPHPNetAPI(DefaultClient)
