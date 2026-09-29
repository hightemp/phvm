package ext

import (
	"encoding/xml"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
)

func packageModule(buildDir, pkg string) (string, error) {
	root, err := os.OpenRoot(buildDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	data, err := root.ReadFile("package.xml")
	if os.IsNotExist(err) {
		return canonicalModule(pkg), nil
	}
	if err != nil {
		return "", err
	}
	var manifest struct {
		Name   string `xml:"name"`
		Module string `xml:"providesextension"`
	}
	if err := xml.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse PECL package.xml: %w", err)
	}
	if manifest.Name != "" && !strings.EqualFold(strings.TrimSpace(manifest.Name), pkg) {
		return "", fmt.Errorf("PECL manifest package does not match requested package")
	}
	module := canonicalModule(manifest.Module)
	if module == "" {
		module = canonicalModule(pkg)
	}
	if err := fsutil.ValidateName(module); err != nil {
		return "", fmt.Errorf("invalid PECL provided module: %w", err)
	}
	return module, nil
}

func extensionBinary(module string) string {
	if runtime.GOOS == "windows" {
		return "php_" + module + ".dll"
	}
	return module + ".so"
}
