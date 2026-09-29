package ext

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hightemp/phvm/internal/core"
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

func recordExtension(paths *core.Paths, phpVersion, pkg, module, version, binary, source string) error {
	for _, name := range []string{pkg, module} {
		if err := fsutil.ValidateName(name); err != nil {
			return err
		}
	}
	root, err := paths.OpenVersion(phpVersion, false)
	if err != nil {
		return err
	}
	defer root.Close()
	metadata := core.NewMetadata(phpVersion)
	data, err := root.ReadFile(".phvm-metadata.json")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, metadata); err != nil {
			return fmt.Errorf("parse metadata: %w", err)
		}
	}
	if metadata.Extensions == nil {
		metadata.Extensions = make(map[string]core.ExtMetadata)
	}
	iniName := "20-" + pkg + ".ini"
	directive := "extension"
	zend := isZendExtension(module)
	if zend {
		directive = "zend_extension"
	}
	content := fmt.Sprintf("; Extension %s\n%s=%s\n", pkg, directive, extensionBinary(module))
	if err := fsutil.AtomicWriteRoot(root, filepath.Join("etc", "conf.d", iniName), []byte(content), 0644); err != nil {
		return err
	}
	metadata.AddExtension(pkg, version, true)
	entry := metadata.Extensions[pkg]
	entry.Module = module
	entry.Binary = binary
	entry.IniFile = iniName
	entry.Zend = zend
	entry.SourceURL = source
	metadata.Extensions[pkg] = entry
	data, err = json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteRoot(root, ".phvm-metadata.json", data, 0644)
}
