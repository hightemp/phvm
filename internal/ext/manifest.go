package ext

import (
	"fmt"
	"os"
	"runtime"

	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/remote"
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
	provided, err := remote.PECLPackageModule(data, pkg)
	if err != nil {
		return "", fmt.Errorf("parse PECL package.xml: %w", err)
	}
	module := canonicalModule(provided)
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
