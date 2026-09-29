package ext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/redact"
)

type fileChange struct {
	path   string
	data   []byte
	mode   os.FileMode
	remove bool
}

// regularPath rejects symlinks in every component, including links inside the
// installation. os.Root additionally confines all reads and mutations.
func regularPath(root *os.Root, path string) error {
	if !filepath.IsLocal(path) || filepath.Clean(path) != path || path == "." {
		return fmt.Errorf("invalid extension file path: %s", path)
	}
	parts := strings.Split(path, string(filepath.Separator))
	for n := range parts {
		info, err := root.Lstat(filepath.Join(parts[:n+1]...))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if n < len(parts)-1 && !info.IsDir() || n == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("extension path is not a regular file: %s", path)
		}
	}
	return nil
}

func fileSnapshot(root *os.Root, path string) (fileChange, error) {
	c := fileChange{path: path}
	if err := regularPath(root, path); err != nil {
		return c, err
	}
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		c.remove = true
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.mode = info.Mode().Perm()
	c.data, err = root.ReadFile(path)
	return c, err
}

func writeChange(root *os.Root, c fileChange) error {
	if c.remove {
		err := root.Remove(c.path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return fsutil.AtomicWriteRoot(root, c.path, c.data, c.mode)
}

// Each file is published atomically. On a returned write error the previous
// bytes/modes are restored; this does not promise recovery after process death.
func applyFileChanges(root *os.Root, changes []fileChange) error {
	snapshots := make([]fileChange, len(changes))
	seen := make(map[string]bool)
	for n, c := range changes {
		if seen[c.path] {
			return fmt.Errorf("duplicate extension transaction path: %s", c.path)
		}
		seen[c.path] = true
		var err error
		snapshots[n], err = fileSnapshot(root, c.path)
		if err != nil {
			return err
		}
	}
	for n, c := range changes {
		if err := writeChange(root, c); err != nil {
			failure := fmt.Errorf("write extension file %s: %w", c.path, err)
			for k := n - 1; k >= 0; k-- {
				if restoreErr := writeChange(root, snapshots[k]); restoreErr != nil {
					failure = errors.Join(failure, fmt.Errorf("restore %s: %w", snapshots[k].path, restoreErr))
				}
			}
			return failure
		}
	}
	return nil
}

func binaryHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func validateOwnedBinary(i *inventory, e *inventoryEntry) error {
	meta := i.metadata.Extensions[e.packageKey]
	if meta.BinarySHA256 == "" || filepath.Base(meta.Binary) != extensionBinary(e.Module) {
		return fmt.Errorf("binary ownership is not established for %s", e.Name)
	}
	if err := regularPath(i.root, meta.Binary); err != nil {
		return err
	}
	for _, other := range i.entries {
		if other == e {
			continue
		}
		if other.Binary == meta.Binary {
			return fmt.Errorf("extension binary is shared with %s", other.Name)
		}
		for _, loader := range other.files {
			if binaryModule(loader.binary) == e.Module {
				return fmt.Errorf("extension binary has another loader")
			}
		}
	}
	if len(e.files) > 0 && meta.IniFile != e.files[0].name {
		return fmt.Errorf("loading ini ownership changed for %s", e.Name)
	}
	if len(e.files) > 0 {
		loader := e.files[0].binary
		if filepath.IsAbs(loader) {
			var err error
			loader, err = filepath.Rel(i.paths.VersionDir(i.version), loader)
			if err != nil {
				return err
			}
		} else if filepath.Base(loader) == loader {
			loader = filepath.Join(filepath.Dir(meta.Binary), loader)
		}
		if loader != meta.Binary {
			return fmt.Errorf("loading ini points to a different binary for %s", e.Name)
		}
	}
	data, err := i.root.ReadFile(meta.Binary)
	if os.IsNotExist(err) {
		return nil // A missing owned file can be cleaned from metadata/config.
	}
	if err != nil {
		return err
	}
	if binaryHash(data) != meta.BinarySHA256 {
		return fmt.Errorf("owned binary changed for %s; refusing to modify it", e.Name)
	}
	return nil
}

func probeModules(ctx context.Context, php string, args ...string) (map[string]bool, error) {
	stdout, stderr, err := phpQuery(ctx, php, args...)
	if err != nil {
		return nil, fmt.Errorf("load extension with selected PHP: %w: %s", redact.Error(err, ""), redact.Text(string(stderr)))
	}
	if len(stderr) > 0 || strings.Contains(string(stdout), "Warning:") || strings.Contains(string(stdout), "Fatal error:") {
		return nil, fmt.Errorf("PHP extension startup failed: %s", redact.Text(string(stderr)+string(stdout)))
	}
	return loadedModules(stdout), nil
}

// Only loading directives are copied into the probe. -n excludes both php.ini
// and the scan directory; unrelated settings and the previous candidate loader
// cannot make a broken new binary appear loaded.
func validationArgs(i *inventory, module, artifact, extDir string) ([]string, error) {
	args := []string{"-n", "-d", "extension_dir=" + extDir, "-d", "display_startup_errors=1", "-d", "display_errors=stderr", "-d", "log_errors=0"}
	appendLoaders := func(data []byte) {
		for _, f := range loadingDirectives(data) {
			if f.module == module {
				continue
			}
			key := "extension="
			if f.zend {
				key = "zend_extension="
			}
			args = append(args, "-d", key+f.binary)
		}
	}
	ini := filepath.Join("etc", "php.ini")
	if err := regularPath(i.root, ini); err != nil {
		return nil, err
	}
	data, err := i.root.ReadFile(ini)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	appendLoaders(data)
	var files []loadingFile
	for _, e := range i.entries {
		for _, f := range e.files {
			if f.enabled {
				files = append(files, f)
			}
		}
	}
	sort.Slice(files, func(a, b int) bool { return files[a].physical < files[b].physical })
	seen := make(map[string]bool)
	for _, f := range files {
		if seen[f.physical] {
			continue
		}
		seen[f.physical] = true
		data, err := i.root.ReadFile(filepath.Join("etc", "conf.d", f.physical))
		if err != nil {
			return nil, err
		}
		appendLoaders(data)
	}
	key := "extension="
	if isZendExtension(module) {
		key = "zend_extension="
	}
	return append(args, "-d", key+artifact, "-m"), nil
}

type configurationRecord struct {
	Flags       []string
	Environment map[string]string
}

func publishExtension(ctx context.Context, paths *core.Paths, phpVersion, pkg, module, version, srcDir, extDir, source string, records ...configurationRecord) error {
	i, err := readInventory(paths, phpVersion)
	if err != nil {
		return err
	}
	defer i.root.Close()
	extDir, err = filepath.Abs(extDir)
	if err != nil {
		return err
	}
	dir, err := filepath.Rel(paths.VersionDir(i.version), extDir)
	if err != nil || !filepath.IsLocal(dir) || dir == "." {
		return fmt.Errorf("extension directory must be inside selected PHP installation")
	}
	binary := filepath.Join(dir, extensionBinary(module))
	if err := regularPath(i.root, binary); err != nil {
		return err
	}
	old := i.entries[module]
	for _, e := range i.entries {
		if strings.EqualFold(e.packageKey, pkg) && e.Module != module {
			return fmt.Errorf("package %s already owns module %s", pkg, e.Module)
		}
	}
	if old != nil {
		if !strings.EqualFold(old.packageKey, pkg) {
			return fmt.Errorf("module %s already belongs to another owner", module)
		}
		if len(old.files) > 0 {
			if err := validateLoadingFile(old); err != nil {
				return err
			}
		}
		if err := validateOwnedBinary(i, old); err != nil {
			return err
		}
		if i.metadata.Extensions[old.packageKey].Binary != binary {
			return fmt.Errorf("owned binary path changed")
		}
		pkg = old.packageKey
	} else if _, err := i.root.Lstat(binary); err == nil {
		return fmt.Errorf("refusing to overwrite an unowned extension binary")
	} else if !os.IsNotExist(err) {
		return err
	}
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer src.Close()
	artifact := filepath.Join("modules", extensionBinary(module))
	if err := regularPath(src, artifact); err != nil {
		return err
	}
	data, err := src.ReadFile(artifact)
	if err != nil {
		return fmt.Errorf("required built binary %s: %w", artifact, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("built extension binary is empty")
	}
	php := filepath.Join(paths.VersionBin(i.version), core.PHPBinary())
	builtin, err := probeModules(ctx, php, "-n", "-m")
	if err != nil {
		return err
	}
	if builtin[module] {
		return fmt.Errorf("cannot replace built-in module %s", module)
	}
	// Probe an immutable copy of the exact bytes that will be published.
	stage, err := os.MkdirTemp("", "phvm-ext-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, extensionBinary(module))
	stageRoot, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer stageRoot.Close()
	if err := fsutil.AtomicWriteRoot(stageRoot, extensionBinary(module), data, 0600); err != nil {
		return err
	}
	args, err := validationArgs(i, module, staged, extDir)
	if err != nil {
		return err
	}
	loaded, err := probeModules(ctx, php, args...)
	if err != nil {
		return err
	}
	if !loaded[module] {
		return fmt.Errorf("built module %s did not load with PHP %s (check ABI and dependencies)", module, i.version)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if i.metadata == nil {
		i.metadata = core.NewMetadata(i.version)
	}
	if i.metadata.Extensions == nil {
		i.metadata.Extensions = make(map[string]core.ExtMetadata)
	}
	iniName := "20-" + pkg + ".ini"
	if old != nil && len(old.files) > 0 {
		iniName = old.files[0].name
	}
	iniPath := filepath.Join("etc", "conf.d", iniName)
	if old == nil || len(old.files) == 0 {
		for _, path := range []string{iniPath, iniPath + ".disabled"} {
			if _, err := i.root.Lstat(path); err == nil {
				return fmt.Errorf("refusing to overwrite an unowned ini")
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	directive := "extension"
	if isZendExtension(module) {
		directive = "zend_extension"
	}
	absoluteBinary, err := filepath.Abs(filepath.Join(paths.VersionDir(i.version), binary))
	if err != nil {
		return err
	}
	if strings.ContainsAny(absoluteBinary, "\r\n\"") {
		return fmt.Errorf("extension binary path cannot be represented in ini")
	}
	content := []byte(fmt.Sprintf("; Extension %s\n%s=\"%s\"\n", pkg, directive, filepath.ToSlash(absoluteBinary)))
	i.metadata.AddExtension(pkg, version, true)
	entry := i.metadata.Extensions[pkg]
	entry.Module, entry.Binary, entry.IniFile = module, binary, iniName
	entry.BinarySHA256, entry.SourceURL, entry.Zend = binaryHash(data), source, isZendExtension(module)
	if len(records) > 0 {
		entry.ConfigureFlags = records[0].Flags
		entry.BuildEnvironment = records[0].Environment
	}
	i.metadata.Extensions[pkg] = entry
	metadata, err := json.MarshalIndent(i.metadata, "", "  ")
	if err != nil {
		return err
	}
	changes := []fileChange{{path: binary, data: data, mode: 0644}, {path: iniPath, data: content, mode: 0644}}
	if old != nil && len(old.files) > 0 && !old.files[0].enabled {
		changes = append(changes, fileChange{path: iniPath + ".disabled", remove: true})
	}
	changes = append(changes, fileChange{path: ".phvm-metadata.json", data: metadata, mode: 0644})
	return applyFileChanges(i.root, changes)
}
