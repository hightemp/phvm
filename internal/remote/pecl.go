package remote

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// PECLAPI provides access to pecl.php.net REST API.
type PECLAPI struct {
	client  *Client
	baseURL string
}

// NewPECLAPI creates a new PECLAPI.
func NewPECLAPI(client *Client) *PECLAPI {
	return &PECLAPI{
		client:  client,
		baseURL: "https://pecl.php.net",
	}
}

// PECLPackage represents a PECL package.
type PECLPackage struct {
	Name        string
	Summary     string
	Description string
	License     string
	Category    string
}

// PECLRelease represents a PECL package release.
type PECLRelease struct {
	Version     string
	Stability   string
	DownloadURL string
	Date        string
	ReleaseDate string
	FileSize    int64
}

// xmlPackages is the XML structure for the packages list.
type xmlPackages struct {
	XMLName  xml.Name `xml:"a"`
	Packages []string `xml:"p"`
}

// xmlPackageInfo is the XML structure for package info.
type xmlPackageInfo struct {
	XMLName     xml.Name `xml:"p"`
	Name        string   `xml:"n"`
	Summary     string   `xml:"s"`
	Description string   `xml:"d"`
	License     string   `xml:"l"`
}

// xmlAllReleases is the XML structure for all releases.
type xmlAllReleases struct {
	XMLName  xml.Name          `xml:"a"`
	Package  string            `xml:"p"`
	Releases []xmlReleaseShort `xml:"r"`
}

type xmlReleaseShort struct {
	Version   string `xml:"v"`
	Stability string `xml:"s"`
}

// xmlRelease is the XML structure for a release.
type xmlRelease struct {
	XMLName     xml.Name `xml:"r"`
	Version     string   `xml:"v"`
	Stability   string   `xml:"st"`
	License     string   `xml:"l"`
	Maintainer  string   `xml:"m"`
	Summary     string   `xml:"s"`
	Description string   `xml:"d"`
	Date        string   `xml:"da"`
	FileSize    string   `xml:"f"`
	DownloadURL string   `xml:"g"`
}

// ListPackages returns all available PECL packages.
func (api *PECLAPI) ListPackages(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/rest/p/packages.xml", api.baseURL)

	resp, err := api.client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch packages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var packages xmlPackages
	if err := xml.Unmarshal(body, &packages); err != nil {
		return nil, fmt.Errorf("parse packages: %w", err)
	}

	return packages.Packages, nil
}

// SearchPackages searches for packages by name.
func (api *PECLAPI) SearchPackages(ctx context.Context, query string) ([]string, error) {
	all, err := api.ListPackages(ctx)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var results []string
	for _, pkg := range all {
		if strings.Contains(strings.ToLower(pkg), query) {
			results = append(results, pkg)
		}
	}

	return results, nil
}

// GetPackageInfo returns information about a package.
func (api *PECLAPI) GetPackageInfo(ctx context.Context, name string) (*PECLPackage, error) {
	url := fmt.Sprintf("%s/rest/p/%s/info.xml", api.baseURL, strings.ToLower(name))

	resp, err := api.client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch package info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("package not found: %s", name)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var info xmlPackageInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse package info: %w", err)
	}

	return &PECLPackage{
		Name:        info.Name,
		Summary:     info.Summary,
		Description: info.Description,
		License:     info.License,
	}, nil
}

// GetAllReleases returns all releases for a package.
func (api *PECLAPI) GetAllReleases(ctx context.Context, name string) ([]PECLRelease, error) {
	url := fmt.Sprintf("%s/rest/r/%s/allreleases.xml", api.baseURL, strings.ToLower(name))

	resp, err := api.client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("package not found: %s", name)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var allReleases xmlAllReleases
	if err := xml.Unmarshal(body, &allReleases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var releases []PECLRelease
	for _, r := range allReleases.Releases {
		releases = append(releases, PECLRelease{
			Version:   r.Version,
			Stability: r.Stability,
		})
	}

	return releases, nil
}

// GetLatestVersion returns the latest stable version of a package.
func (api *PECLAPI) GetLatestVersion(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/rest/r/%s/stable.txt", api.baseURL, strings.ToLower(name))

	resp, err := api.client.Get(ctx, url)
	if err != nil {
		return "", fmt.Errorf("fetch latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// Try latest.txt instead
		url = fmt.Sprintf("%s/rest/r/%s/latest.txt", api.baseURL, strings.ToLower(name))
		resp, err = api.client.Get(ctx, url)
		if err != nil {
			return "", fmt.Errorf("fetch latest version: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}

// GetRelease returns information about a specific release.
func (api *PECLAPI) GetRelease(ctx context.Context, name, version string) (*PECLRelease, error) {
	url := fmt.Sprintf("%s/rest/r/%s/%s.xml", api.baseURL, strings.ToLower(name), version)

	resp, err := api.client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("release not found: %s-%s", name, version)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var release xmlRelease
	if err := xml.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}

	return &PECLRelease{
		Version:     release.Version,
		Stability:   release.Stability,
		DownloadURL: release.DownloadURL,
		Date:        release.Date,
	}, nil
}

// GetDownloadURL returns the download URL for a specific version.
func (api *PECLAPI) GetDownloadURL(name, version string) string {
	return fmt.Sprintf("%s/get/%s-%s.tgz", api.baseURL, name, version)
}

// DefaultPECLAPI returns a PECLAPI with the default client.
var DefaultPECLAPI = NewPECLAPI(DefaultClient)
