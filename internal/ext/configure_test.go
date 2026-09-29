package ext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestExtensionMetadataRecordsActualConfigureArguments(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	i := NewInstaller(p, nil)
	archive, err := os.ReadFile(p.ExtensionCachePath("pecl_http", "4.3.0"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(archive)
	if err := i.Install(context.Background(), InstallOptions{Name: "pecl_http", Version: "4.3.0", PHPVersion: "8.3.30", SHA256: hex.EncodeToString(hash[:]), CustomFlags: []string{"--with-greeting=/a path,with comma", "CFLAGS=-O0 -g"}}); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	entry := meta.Extensions["pecl_http"]
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var trust struct {
		Hash   string `json:"source_sha256"`
		Method string `json:"source_verification"`
	}
	if err := json.Unmarshal(data, &trust); err != nil {
		t.Fatal(err)
	}
	if trust.Hash != hex.EncodeToString(hash[:]) || trust.Method != "sha256-pinned" {
		t.Errorf("source trust missing: %s", data)
	}
	want := []string{"--with-greeting=/a path,with comma", "CFLAGS=-O0 -g", "--with-php-config=" + filepath.Join(p.VersionBin("8.3.30"), "php-config")}
	if !reflect.DeepEqual(entry.ConfigureFlags, want) {
		t.Errorf("actual extension argv=%q want=%q", entry.ConfigureFlags, want)
	}
	if entry.BuildEnvironment["CC"] == "" {
		t.Error("extension build environment not recorded")
	}
	if entry.BuildEnvironment["CFLAGS"] != "-O0 -g" {
		t.Error("extension configure assignment not reflected in environment")
	}
}

func TestExtensionCannotOverrideSelectedSDK(t *testing.T) {
	p := extensionFixture(t)
	if err := NewInstaller(p, nil).Install(context.Background(), InstallOptions{Name: "redis", PHPVersion: "8.3.30", CustomFlags: []string{"--with-php-config=/other/sdk"}}); err == nil {
		t.Error("unrelated SDK override accepted")
	}
}
