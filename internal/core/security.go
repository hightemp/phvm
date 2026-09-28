package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
)

// NormalizeInstalledVersion returns a concrete canonical version for paths.
func NormalizeInstalledVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "php-") {
		value = strings.TrimPrefix(value, "php-")
	} else {
		value = strings.TrimPrefix(value, "v")
	}
	if !versionRegexFull.MatchString(value) {
		return "", fmt.Errorf("expected a complete PHP version X.Y.Z")
	}
	parts := strings.Split(value, ".")
	var numbers [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return "", fmt.Errorf("invalid PHP version: %w", err)
		}
		numbers[i] = n
	}
	if numbers[0] == 0 {
		return "", fmt.Errorf("invalid PHP major version")
	}
	return fmt.Sprintf("%d.%d.%d", numbers[0], numbers[1], numbers[2]), nil
}

// OpenDataDir opens a managed directory without following escaping symlinks.
func (p *Paths) OpenDataDir(path string, create bool) (*os.Root, error) {
	return fsutil.OpenScopedDir(p.Root, path, create)
}

// OpenVersion opens a concrete version's directory within the managed root.
func (p *Paths) OpenVersion(version string, create bool) (*os.Root, error) {
	version, err := NormalizeInstalledVersion(version)
	if err != nil {
		return nil, err
	}
	root, err := p.OpenDataDir(p.VersionDir(version), create)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"bin", "etc", ".phvm-metadata.json", filepath.Join("bin", PHPBinary()), filepath.Join("bin", "composer"), filepath.Join("bin", "phpize"), filepath.Join("bin", "php-config"), filepath.Join("etc", "php.ini"), filepath.Join("etc", "conf.d")} {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			_ = root.Close()
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = root.Close()
			return nil, fmt.Errorf("managed installation path cannot be a symlink: %s", name)
		}
	}
	return root, nil
}

// CheckVersionPath validates a concrete version and its existing path parents.
func (p *Paths) CheckVersionPath(version string) (string, error) {
	version, err := NormalizeInstalledVersion(version)
	if err != nil {
		return "", err
	}
	root, err := p.OpenVersion(version, false)
	if os.IsNotExist(err) {
		return version, nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	return version, nil
}
