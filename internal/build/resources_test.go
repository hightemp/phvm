package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/remote"
	"github.com/hightemp/phvm/internal/resources"
)

func TestNewBuilderUsesAvailableMemoryForAutomaticJobs(t *testing.T) {
	p := core.NewPaths(t.TempDir())
	b := newBuilder(p, (*remote.Client)(nil), func() (uint64, error) { return 2 << 30, nil }, func(string) (uint64, error) { return 1 << 40, nil })
	if b.jobs != 1 {
		t.Errorf("low-memory automatic builder selected %d jobs", b.jobs)
	}
	b.SetJobs(7)
	if b.jobs != 7 {
		t.Errorf("explicit jobs lost to memory heuristic: %d", b.jobs)
	}
}

func TestPHPBuildChecksSpaceBeforeSourceExtraction(t *testing.T) {
	b, opts := transactionFixture(t, "", false)
	b.freeSpace = func(string) (uint64, error) { return 0, nil }
	err := b.Build(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "before source extraction") {
		t.Fatalf("low space did not stop extraction: %v", err)
	}
	if _, err := os.Stat(b.paths.SourcePath(opts.Version)); !os.IsNotExist(err) {
		t.Errorf("source extracted despite failed space check: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.paths.Root, "configure-args")); !os.IsNotExist(err) {
		t.Errorf("configure executed despite failed space check: %v", err)
	}
	if _, err := os.Stat(b.paths.VersionDir(opts.Version)); !os.IsNotExist(err) {
		t.Errorf("PHP published despite failed space check: %v", err)
	}
}

func TestPHPBuildRechecksSpaceBeforeInstallation(t *testing.T) {
	b, opts := transactionFixture(t, "", false)
	versionChecks := 0
	b.freeSpace = func(path string) (uint64, error) {
		if path == b.paths.Versions {
			versionChecks++
			if versionChecks == 2 {
				return 0, nil
			}
		}
		return 1 << 40, nil
	}
	err := b.Build(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "before PHP installation") || versionChecks != 2 {
		t.Fatalf("space loss after compile did not stop installation: checks=%d error=%v", versionChecks, err)
	}
	if _, err := os.Stat(filepath.Join(b.paths.Root, "configure-args")); err != nil {
		t.Errorf("fixture did not reach configure: %v", err)
	}
	if _, err := os.Stat(b.paths.VersionDir(opts.Version)); !os.IsNotExist(err) {
		t.Errorf("PHP published after installation space check failed: %v", err)
	}
}

func TestPHPReinstallAccountsForPreviousInstallationFootprint(t *testing.T) {
	b, opts := transactionFixture(t, "", true)
	archive, err := os.Stat(opts.TarballPath)
	if err != nil {
		t.Fatal(err)
	}
	_, baseInstallNeed := resources.EstimatePHPBuildSpace(archive.Size())
	b.freeSpace = func(path string) (uint64, error) {
		if path == b.paths.Versions {
			return baseInstallNeed, nil
		}
		return 1 << 40, nil
	}
	err = b.Build(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "before PHP installation") {
		t.Errorf("force reinstall ignored previous bytes: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(b.paths.VersionBin(opts.Version), "php"))
	if err != nil || !strings.Contains(string(data), "old working PHP") {
		t.Errorf("previous installation changed after preflight failure: %q %v", data, err)
	}
}
