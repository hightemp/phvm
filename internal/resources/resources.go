// Package resources estimates local capacity before source builds.
package resources

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

const (
	mib                     = uint64(1 << 20)
	gib                     = uint64(1 << 30)
	memoryReserve           = gib
	memoryPerJob            = 2 * gib
	cacheBase               = 256 * mib
	installBase             = 128 * mib
	minimumCache            = 512 * mib
	minimumInstall          = 256 * mib
	extensionBase           = 64 * mib
	extensionInstallBase    = 32 * mib
	minimumExtensionBuild   = 128 * mib
	minimumExtensionInstall = 64 * mib
)

// ResolveJobs chooses a build concurrency, preserving an explicit override.
func ResolveJobs(explicit, cpus int, availableBytes uint64) int {
	if explicit > 0 {
		return explicit
	}
	if cpus < 1 {
		cpus = runtime.NumCPU()
	}
	if cpus < 1 {
		cpus = 1
	}
	if cpus > 1 {
		cpus /= 2
	}
	if availableBytes <= memoryReserve {
		return 1
	}
	memoryJobs := (availableBytes - memoryReserve) / memoryPerJob
	if memoryJobs < 1 {
		memoryJobs = 1
	}
	if memoryJobs < uint64(cpus) {
		return int(memoryJobs)
	}
	return cpus
}

// EstimatePHPBuildSpace returns approximate cache and installation requirements.
func EstimatePHPBuildSpace(archiveBytes int64) (uint64, uint64) {
	if archiveBytes < 0 {
		archiveBytes = 0
	}
	cache := saturatingMultiplyAdd(uint64(archiveBytes), 12, cacheBase)
	install := saturatingMultiplyAdd(uint64(archiveBytes), 8, installBase)
	if cache < minimumCache {
		cache = minimumCache
	}
	if install < minimumInstall {
		install = minimumInstall
	}
	return cache, install
}

// EstimateExtensionBuildSpace reserves temporary build and managed install space.
func EstimateExtensionBuildSpace(archiveBytes int64) (uint64, uint64) {
	if archiveBytes < 0 {
		archiveBytes = 0
	}
	build := saturatingMultiplyAdd(uint64(archiveBytes), 12, extensionBase)
	install := saturatingMultiplyAdd(uint64(archiveBytes), 8, extensionInstallBase)
	if build < minimumExtensionBuild {
		build = minimumExtensionBuild
	}
	if install < minimumExtensionInstall {
		install = minimumExtensionInstall
	}
	return build, install
}

func saturatingMultiplyAdd(value, factor, extra uint64) uint64 {
	if value > (math.MaxUint64-extra)/factor {
		return math.MaxUint64
	}
	return value*factor + extra
}

// RequireSpace checks a path before beginning a disk-consuming phase.
func RequireSpace(path, phase string, required uint64, probe func(string) (uint64, error)) error {
	if probe == nil {
		return fmt.Errorf("check free disk space before %s: probe unavailable", phase)
	}
	available, err := probe(path)
	if err != nil {
		return fmt.Errorf("check free disk space before %s on %s: %w", phase, path, err)
	}
	if available < required {
		neededMiB := required / mib
		if required%mib != 0 {
			neededMiB++
		}
		return fmt.Errorf("insufficient free disk space before %s on %s: need approximately %d MiB, available %d MiB", phase, path, neededMiB, available/mib)
	}
	return nil
}

func existingDiskPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return path, nil
			}
			return filepath.Dir(path), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

// DirectorySize counts existing regular files without following symlinks.
// A missing directory has zero footprint.
func DirectorySize(path string) (uint64, error) {
	root, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !root.IsDir() {
		return 0, fmt.Errorf("existing installation path is not a directory: %s", path)
	}
	var total uint64
	err = filepath.WalkDir(path, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("existing installation contains a nonregular file: %s", name)
		}
		size := info.Size()
		if size < 0 {
			return fmt.Errorf("existing installation has a negative file size: %s", name)
		}
		total = saturatingMultiplyAdd(uint64(size), 1, total)
		return nil
	})
	return total, err
}
