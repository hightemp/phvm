package ext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestExtensionBuildJobsRespectMemoryAndExplicitValue(t *testing.T) {
	i := newInstaller(core.NewPaths(t.TempDir()), nil, func() (uint64, error) { return 2 << 30, nil })
	if i.jobs != 1 {
		t.Errorf("low-memory PECL builder selected %d jobs", i.jobs)
	}
	i.SetJobs(6)
	if i.jobs != 6 {
		t.Errorf("explicit extension jobs were capped: %d", i.jobs)
	}
}

func TestExtensionInstallChecksSpaceBeforeExtractionAndPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PECL build fixture")
	}
	for _, phase := range []string{"extraction", "publication"} {
		t.Run(phase, func(t *testing.T) {
			p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
			archive, err := os.ReadFile(p.ExtensionCachePath("pecl_http", "4.3.0"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(archive)
			sentinel := filepath.Join(p.Root, "phpize-reached")
			phpize := "#!/bin/sh\ntouch " + shellLiteral(sentinel) + "\nexit 0\n"
			if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), "phpize"), []byte(phpize), 0755); err != nil {
				t.Fatal(err)
			}
			i := NewInstaller(p, nil)
			versionChecks := 0
			i.freeSpace = func(path string) (uint64, error) {
				if phase == "extraction" && path == os.TempDir() {
					return 0, nil
				}
				if phase == "publication" && path == p.VersionDir("8.3.30") {
					versionChecks++
					if versionChecks == 2 {
						return 0, nil
					}
				}
				return 1 << 40, nil
			}
			err = i.Install(context.Background(), InstallOptions{Name: "pecl_http", Version: "4.3.0", PHPVersion: "8.3.30", SHA256: hex.EncodeToString(hash[:])})
			if err == nil || !strings.Contains(err.Error(), "insufficient free disk space") {
				t.Fatalf("%s space shortage did not stop extension install: %v", phase, err)
			}
			_, statErr := os.Stat(sentinel)
			if phase == "extraction" && !os.IsNotExist(statErr) {
				t.Errorf("phpize ran before space preflight: %v", statErr)
			}
			if phase == "publication" && (statErr != nil || versionChecks != 2) {
				t.Errorf("publication check did not run after build: phpize=%v checks=%d", statErr, versionChecks)
			}
			if _, err := os.Stat(p.VersionMetadata("8.3.30")); !os.IsNotExist(err) {
				t.Errorf("extension metadata published after space failure: %v", err)
			}
		})
	}
}
