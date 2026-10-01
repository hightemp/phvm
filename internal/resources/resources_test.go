package resources

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveJobsUsesAvailableMemoryButExplicitValueWins(t *testing.T) {
	const gib = uint64(1 << 30)
	for _, tt := range []struct {
		name     string
		explicit int
		cpus     int
		memory   uint64
		want     int
	}{
		{"low memory", 0, 16, 2 * gib, 1},
		{"moderate memory", 0, 16, 5 * gib, 2},
		{"many cores and memory", 0, 16, 32 * gib, 8},
		{"one core", 0, 1, 32 * gib, 1},
		{"unknown memory is conservative", 0, 16, 0, 1},
		{"explicit override", 12, 16, 2 * gib, 12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveJobs(tt.explicit, tt.cpus, tt.memory); got != tt.want {
				t.Errorf("ResolveJobs(%d,%d,%d)=%d want=%d", tt.explicit, tt.cpus, tt.memory, got, tt.want)
			}
		})
	}
}

func TestFreeDiskSpaceUsesExistingParentForFutureDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future", "source")
	free, err := FreeDiskSpace(path)
	if err != nil || free == 0 {
		t.Errorf("free space for new directory=%d error=%v", free, err)
	}
}

func TestDirectorySizeCountsFilesAndRejectsLinks(t *testing.T) {
	dir := t.TempDir()
	if size, err := DirectorySize(filepath.Join(dir, "missing")); err != nil || size != 0 {
		t.Errorf("missing directory size=%d error=%v", size, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if size, err := DirectorySize(dir); err != nil || size != 5 {
		t.Errorf("directory size=%d error=%v", size, err)
	}
	if err := os.Symlink(filepath.Join(dir, "file"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := DirectorySize(dir); err == nil {
		t.Error("existing installation symlink accepted by space estimator")
	}
}

func TestEstimatePHPBuildSpaceIsConservativeAndOverflowSafe(t *testing.T) {
	smallCache, smallInstall := EstimatePHPBuildSpace(15 << 20)
	largeCache, largeInstall := EstimatePHPBuildSpace(80 << 20)
	if smallCache < 256<<20 || smallInstall < 128<<20 || largeCache <= smallCache || largeInstall <= smallInstall {
		t.Errorf("estimates do not reserve build/install space: small=%d/%d large=%d/%d", smallCache, smallInstall, largeCache, largeInstall)
	}
	if cache, install := EstimatePHPBuildSpace(math.MaxInt64); cache < largeCache || install < largeInstall {
		t.Errorf("large archive overflowed estimates: %d/%d", cache, install)
	}
}

func TestEstimateExtensionBuildSpaceCoversTempAndPublication(t *testing.T) {
	smallBuild, smallInstall := EstimateExtensionBuildSpace(1 << 20)
	largeBuild, largeInstall := EstimateExtensionBuildSpace(100 << 20)
	if smallBuild < 128<<20 || smallInstall < 64<<20 || largeBuild <= smallBuild || largeInstall <= smallInstall {
		t.Errorf("extension estimates do not scale: %d/%d then %d/%d", smallBuild, smallInstall, largeBuild, largeInstall)
	}
}

func TestRequireSpaceFailsBeforeMutationWithActionableNumbers(t *testing.T) {
	probe := func(path string) (uint64, error) {
		if path != "/managed/cache" {
			t.Errorf("wrong filesystem checked: %s", path)
		}
		return 100 << 20, nil
	}
	err := RequireSpace("/managed/cache", "source extraction", 300<<20, probe)
	if err == nil || !strings.Contains(err.Error(), "source extraction") || !strings.Contains(err.Error(), "300") || !strings.Contains(err.Error(), "100") {
		t.Errorf("low disk space was not diagnosed: %v", err)
	}
	if err := RequireSpace("/managed/cache", "source extraction", 50<<20, probe); err != nil {
		t.Errorf("sufficient disk space rejected: %v", err)
	}
	want := errors.New("probe unavailable")
	if err := RequireSpace("/managed/cache", "source extraction", 1, func(string) (uint64, error) { return 0, want }); !errors.Is(err, want) {
		t.Errorf("disk probe error hidden: %v", err)
	}
}
