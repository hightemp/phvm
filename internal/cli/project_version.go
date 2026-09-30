package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const projectVersionFile = ".php-version"
const maxProjectVersionBytes = 4096

func findProjectPHPVersion() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("get current directory: %w", err)
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		value, found, err := readProjectVersionAt(dir)
		if err != nil {
			return "", "", err
		}
		if found {
			return value, filepath.Join(dir, projectVersionFile), nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", "", fmt.Errorf("no %s found from %s; specify a version with phvm use <version>", projectVersionFile, cwd)
}

func readProjectVersionAt(dir string) (string, bool, error) {
	path := filepath.Join(dir, projectVersionFile)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", false, fmt.Errorf("open project directory %s: %w", dir, err)
	}
	defer root.Close()
	info, err := root.Lstat(projectVersionFile)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", true, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxProjectVersionBytes {
		return "", true, fmt.Errorf("%s is too large", path)
	}
	file, err := root.Open(projectVersionFile)
	if err != nil {
		return "", true, fmt.Errorf("open %s: %w", path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxProjectVersionBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", true, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxProjectVersionBytes {
		return "", true, fmt.Errorf("%s is too large", path)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", true, fmt.Errorf("%s is empty", path)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", true, fmt.Errorf("%s must contain one version or alias", path)
	}
	return value, true, nil
}
