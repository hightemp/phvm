package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/hightemp/phvm/internal/core"
)

const composerVersionsURL = "https://getcomposer.org/versions"

var exactComposerVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type composerRelease struct {
	Version string `json:"version"`
	MinPHP  int    `json:"min-php"`
	Path    string `json:"path"`
}

func (r composerRelease) urls() (string, string) {
	base := "https://getcomposer.org/download/" + r.Version + "/composer.phar"
	return base, base + ".sha256sum"
}

func phpVersionID(version string) (int, error) {
	version, err := core.NormalizeInstalledVersion(version)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])
	if major > 999 || minor > 99 || patch > 99 {
		return 0, fmt.Errorf("PHP version is outside supported Composer metadata range: %s", version)
	}
	return major*10000 + minor*100 + patch, nil
}

func (m *Manager) selectRelease(ctx context.Context, phpVersion, requested string) (composerRelease, error) {
	if requested != "" {
		if !exactComposerVersion.MatchString(requested) {
			return composerRelease{}, fmt.Errorf("composer version must be an exact X.Y.Z release")
		}
		return composerRelease{Version: requested}, nil
	}
	phpID, err := phpVersionID(phpVersion)
	if err != nil {
		return composerRelease{}, err
	}
	if phpID < 50302 {
		return composerRelease{}, fmt.Errorf("composer 2 requires PHP 5.3.2 or newer; PHP %s is unsupported", phpVersion)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, composerVersionsURL, nil)
	if err != nil {
		return composerRelease{}, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return composerRelease{}, fmt.Errorf("fetch Composer versions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return composerRelease{}, fmt.Errorf("fetch Composer versions: HTTP %d", resp.StatusCode)
	}
	var catalog struct {
		Stable []composerRelease `json:"stable"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&catalog); err != nil {
		return composerRelease{}, fmt.Errorf("decode Composer versions: %w", err)
	}
	var chosen composerRelease
	var chosenVersion *semver.Version
	for _, candidate := range catalog.Stable {
		if !exactComposerVersion.MatchString(candidate.Version) || candidate.MinPHP < 50300 || candidate.MinPHP > phpID || candidate.Path != "/download/"+candidate.Version+"/composer.phar" {
			continue
		}
		version, err := semver.NewVersion(candidate.Version)
		if err != nil {
			continue
		}
		if chosenVersion == nil || version.GreaterThan(chosenVersion) {
			chosen, chosenVersion = candidate, version
		}
	}
	if chosenVersion == nil {
		return composerRelease{}, fmt.Errorf("no compatible stable Composer release for PHP %s", phpVersion)
	}
	return chosen, nil
}
