package deps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/remote"
)

type dependencyMarker struct {
	Schema                     int
	Name, Version, URL, SHA256 string
	ConfigureCmd               []string
	Dependencies               map[string]string
}

// IsReady reports whether the private dependency marker matches the exact source.
func (m *DepsManager) IsReady(phpVersion string, dep Dependency) bool {
	version, err := m.paths.CheckVersionPath(phpVersion)
	return err == nil && m.ready(dep, m.DepsDir(version))
}

func (m *DepsManager) ready(dep Dependency, dir string) bool {
	if fsutil.ValidateName(dep.Name) != nil || fsutil.ValidateName(dep.Version) != nil {
		return false
	}
	hash, err := remote.NormalizeSHA256(dep.SHA256)
	if err != nil {
		return false
	}
	root, err := m.paths.OpenDataDir(filepath.Join(dir, dep.Name), false)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Lstat(".phvm-installed")
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := root.ReadFile(".phvm-installed")
	var marker dependencyMarker
	dependencies, depsErr := m.dependencyHashes(dep, dir)
	return err == nil && depsErr == nil && json.Unmarshal(data, &marker) == nil && marker.Schema == 1 && marker.Name == dep.Name && marker.Version == dep.Version && marker.URL == dep.URL && marker.SHA256 == hash && reflect.DeepEqual(marker.ConfigureCmd, dep.ConfigureCmd) && reflect.DeepEqual(marker.Dependencies, dependencies)
}

func (m *DepsManager) dependencyHashes(dep Dependency, dir string) (map[string]string, error) {
	if len(dep.DependsOn) == 0 {
		return nil, nil
	}
	result := make(map[string]string)
	for _, name := range dep.DependsOn {
		if err := fsutil.ValidateName(name); err != nil {
			return nil, err
		}
		root, err := m.paths.OpenDataDir(filepath.Join(dir, name), false)
		if err != nil {
			return nil, err
		}
		info, err := root.Lstat(".phvm-installed")
		if err != nil || !info.Mode().IsRegular() {
			_ = root.Close()
			return nil, fmt.Errorf("dependency %s has no regular trust marker", name)
		}
		data, err := root.ReadFile(".phvm-installed")
		_ = root.Close()
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(data)
		result[name] = hex.EncodeToString(hash[:])
	}
	return result, nil
}
