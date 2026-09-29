package ext

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hightemp/phvm/internal/core"
)

func TestPackageManifestDefinesExactModule(t *testing.T) {
	for _, tt := range []struct {
		name, xml, want string
		fail            bool
	}{
		{name: "different package and module", xml: `<package xmlns="http://pear.php.net/dtd/package-2.0"><name>pecl_http</name><providesextension>http</providesextension></package>`, want: "http"},
		{name: "dependency provides is not package module", xml: `<package><name>pecl_http</name><dependencies><required><package><providesextension>raphf</providesextension></package></required></dependencies><providesextension>http</providesextension></package>`, want: "http"},
		{name: "legacy missing module falls back exactly", xml: `<package><name>pecl_http</name></package>`, want: "pecl_http"},
		{name: "unsafe module", xml: `<package><name>pecl_http</name><providesextension>../../redis</providesextension></package>`, fail: true},
		{name: "bad XML", xml: `<package>`, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.xml"), []byte(tt.xml), 0600); err != nil {
				t.Fatal(err)
			}
			module, err := packageModule(dir, "pecl_http")
			if (err != nil) != tt.fail || !tt.fail && module != tt.want {
				t.Errorf("module=%s error=%v expected=%s fail=%v", module, err, tt.want, tt.fail)
			}
		})
	}
}

func TestInstallationPersistsPackageModuleBinaryIniMapping(t *testing.T) {
	p := installationFixture(t, "printf fixture > modules/http.so", "printf '[PHP Modules]\\nhttp\\n'")
	if err := installHTTP(context.Background(), t, p); err != nil {
		t.Fatal(err)
	}
	meta, err := core.LoadMetadata(p.VersionMetadata("8.3.30"))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := meta.GetExtension("pecl_http")
	if !ok || entry.Module != "http" || entry.Binary != "lib/extensions/http.so" || entry.IniFile != "20-pecl_http.ini" {
		t.Errorf("incomplete identity persisted: %+v", entry)
	}
	if err := Disable(p, "8.3.30", "http"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.VersionConfD("8.3.30"), "20-pecl_http.ini.disabled")); err != nil {
		t.Fatal(err)
	}
}
