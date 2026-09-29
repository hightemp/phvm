package ext

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
)

func TestExtensionChangesWaitForStateLockAndRetainBothMetadataUpdates(t *testing.T) {
	p := extensionFixture(t)
	meta := core.NewMetadata("8.3.30")
	for _, module := range []string{"redis", "http"} {
		writeExtensionFile(t, p, "20-"+module+".ini", "extension="+module+".so\n")
		meta.AddExtension(module, "1.0.0", true)
	}
	if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
		t.Fatal(err)
	}
	lock := fsutil.NewFileLock(p.LockFile())
	if ok, err := lock.TryLock(); !ok || err != nil {
		t.Fatalf("lock %v %v", ok, err)
	}
	defer lock.Unlock()
	done := make(chan error, 2)
	for _, module := range []string{"redis", "http"} {
		go func() { done <- Disable(p, "8.3.30", module) }()
	}
	early := 0
	select {
	case <-done:
		early++
		t.Error("extension mutation bypassed installation/cache state lock")
	case <-time.After(100 * time.Millisecond):
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	for n := early; n < 2; n++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"redis", "http"} {
		if meta.Extensions[module].Enabled {
			t.Errorf("lost metadata update for %s", module)
		}
		if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-"+module+".ini.disabled")); err != nil {
			t.Error(err)
		}
	}
}

func TestPHPRemovalWaitsForExtensionUninstallToFinish(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	marker, release := filepath.Join(t.TempDir(), "probe"), filepath.Join(t.TempDir(), "release")
	script := fmt.Sprintf("#!/bin/sh\ntouch %s\nwhile [ ! -f %s ]; do sleep 0.02; done\nprintf '[PHP Modules]\\nCore\\n'\n", shellLiteral(marker), shellLiteral(release))
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), core.PHPBinary()), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	extDone := make(chan error, 1)
	go func() { extDone <- NewInstaller(p, nil).Uninstall(context.Background(), "http", "8.3.30") }()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			_ = os.WriteFile(release, nil, 0600)
			<-extDone
			t.Fatal("probe not reached")
		case <-time.After(10 * time.Millisecond):
		}
	}
	removed := make(chan error, 1)
	go func() { removed <- core.NewInstalledManager(p).Remove("8.3.30") }()
	early := false
	select {
	case err := <-removed:
		early = true
		t.Errorf("PHP deleted during extension probe: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-extDone; err != nil {
		t.Fatal(err)
	}
	if !early {
		if err := <-removed; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(p.VersionDir("8.3.30")); !os.IsNotExist(err) {
		t.Error("version not removed after extension finished")
	}
}
