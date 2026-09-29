package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/process"
	"github.com/hightemp/phvm/internal/redact"
)

// ErrExtensionNotFound distinguishes a missing exact identity from a command failure.
var ErrExtensionNotFound = errors.New("extension not found")

type loadingFile struct {
	name, physical, binary, module string
	enabled, zend                  bool
	modules                        int
}
type inventoryEntry struct {
	Extension
	packageKey string
	files      []loadingFile
}
type inventory struct {
	paths    *core.Paths
	version  string
	root     *os.Root
	metadata *core.Metadata
	entries  map[string]*inventoryEntry
}

func canonicalModule(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "zend opcache" {
		return "opcache"
	}
	return name
}

func binaryModule(binary string) string {
	binary = strings.ReplaceAll(binary, "\\", "/")
	name := strings.ToLower(filepath.Base(binary))
	for _, suffix := range []string{".so", ".dll", ".dylib"} {
		name = strings.TrimSuffix(name, suffix)
	}
	name = strings.TrimPrefix(name, "php_")
	return canonicalModule(name)
}

func loadingDirectives(data []byte) []loadingFile {
	var out []loadingFile
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "extension" && key != "zend_extension" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if value[0] == '\'' || value[0] == '"' {
			end := strings.IndexByte(value[1:], value[0])
			if end < 0 {
				continue
			}
			value = value[1 : end+1]
		} else {
			value = strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		}
		if value == "" {
			continue
		}
		out = append(out, loadingFile{binary: value, module: binaryModule(value), zend: key == "zend_extension"})
	}
	return out
}

func readInventory(paths *core.Paths, version string) (*inventory, error) {
	version, err := paths.CheckVersionPath(version)
	if err != nil {
		return nil, err
	}
	root, err := paths.OpenVersion(version, false)
	if err != nil {
		return nil, err
	}
	i := &inventory{paths: paths, version: version, root: root, entries: make(map[string]*inventoryEntry)}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	data, err := root.ReadFile(".phvm-metadata.json")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		i.metadata = core.NewMetadata(version)
		if err = json.Unmarshal(data, i.metadata); err != nil {
			return nil, fmt.Errorf("parse extension metadata: %w", err)
		}
		for pkg, meta := range i.metadata.Extensions {
			if err = fsutil.ValidateName(pkg); err != nil {
				return nil, err
			}
			module := canonicalModule(meta.Module)
			if module == "" {
				module = canonicalModule(pkg)
			}
			if err := fsutil.ValidateName(module); err != nil {
				return nil, fmt.Errorf("invalid metadata module: %w", err)
			}
			if existing := i.entries[module]; existing != nil {
				return nil, fmt.Errorf("ambiguous module %s owned by multiple packages", module)
			}
			i.entries[module] = &inventoryEntry{Extension: Extension{Name: pkg, Module: module, Version: meta.Version, Enabled: meta.Enabled, InstalledBy: "phvm", Binary: meta.Binary, IniFile: meta.IniFile}, packageKey: pkg}
		}
	}
	conf, confErr := root.OpenRoot(filepath.Join("etc", "conf.d"))
	if confErr != nil && !os.IsNotExist(confErr) {
		return nil, confErr
	}
	if conf == nil {
		success = true
		return i, nil
	}
	defer conf.Close()
	dir, err := conf.Open(".")
	if err != nil {
		return nil, err
	}
	files, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		physical := file.Name()
		name := strings.TrimSuffix(physical, ".disabled")
		if !strings.HasSuffix(name, ".ini") {
			continue
		}
		if err = fsutil.ValidateName(physical); err != nil {
			return nil, err
		}
		info, err := conf.Lstat(physical)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("extension ini must be a regular file: %s", physical)
		}
		data, err := conf.ReadFile(physical)
		if err != nil {
			return nil, err
		}
		directives := loadingDirectives(data)
		for _, directive := range directives {
			directive.name = name
			directive.physical = physical
			directive.enabled = physical == name
			directive.modules = len(directives)
			e := i.entries[directive.module]

			if e == nil {
				e = &inventoryEntry{Extension: Extension{Name: directive.module, Module: directive.module, InstalledBy: "external"}}
				i.entries[directive.module] = e
			}
			e.files = append(e.files, directive)
			e.IniFile = name
			e.Enabled = directive.enabled
			if e.Binary == "" {
				e.Binary = directive.binary
			}
		}
	}
	success = true
	return i, nil
}

func (i *inventory) resolve(name string) (*inventoryEntry, error) {
	if err := fsutil.ValidateName(name); err != nil {
		return nil, err
	}
	key := canonicalModule(name)
	var match *inventoryEntry
	for module, e := range i.entries {
		if module == key || canonicalModule(e.packageKey) == key {
			if match != nil && match != e {
				return nil, fmt.Errorf("ambiguous extension identity: %s", name)
			}
			match = e
		}
	}
	if match == nil {
		return nil, fmt.Errorf("%w: %s", ErrExtensionNotFound, name)
	}
	return match, nil
}

func validateLoadingFile(e *inventoryEntry) error {
	if len(e.files) == 0 {
		return fmt.Errorf("extension %s has no managed loading ini", e.Name)
	}
	if len(e.files) != 1 {
		return fmt.Errorf("extension %s has multiple loading ini files", e.Name)
	}
	if e.files[0].modules != 1 {
		return fmt.Errorf("ini %s loads multiple extensions; edit it explicitly", e.files[0].name)
	}
	return nil
}

func (i *inventory) saveMetadata() error {
	if i.metadata == nil {
		return nil
	}
	data, err := json.MarshalIndent(i.metadata, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteRoot(i.root, ".phvm-metadata.json", data, 0644)
}

func removeExact(ctx context.Context, paths *core.Paths, version, name string) error {
	i, err := readInventory(paths, version)
	if err != nil {
		return err
	}
	defer i.root.Close()
	e, err := i.resolve(name)
	if err != nil {
		return err
	}
	if len(e.files) > 0 {
		if err := validateLoadingFile(e); err != nil {
			return err
		}
	}
	php := filepath.Join(paths.VersionBin(i.version), core.PHPBinary())
	builtin, err := probeModules(ctx, php, "-n", "-m")
	if err != nil {
		return err
	}
	if builtin[e.Module] {
		return fmt.Errorf("cannot uninstall built-in module %s", e.Module)
	}
	changes := []fileChange{}
	if e.packageKey != "" {
		meta := i.metadata.Extensions[e.packageKey]
		if meta.BinarySHA256 != "" {
			if err := validateOwnedBinary(i, e); err != nil {
				return err
			}
			changes = append(changes, fileChange{path: meta.Binary, remove: true})
		}
	}
	if len(e.files) > 0 {
		changes = append(changes, fileChange{path: filepath.Join("etc", "conf.d", e.files[0].physical), remove: true})
	}
	if e.packageKey != "" {
		delete(i.metadata.Extensions, e.packageKey)
		data, err := json.MarshalIndent(i.metadata, "", "  ")
		if err != nil {
			return err
		}
		changes = append(changes, fileChange{path: ".phvm-metadata.json", data: data, mode: 0644})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return applyFileChanges(i.root, changes)
}

func setEnabled(paths *core.Paths, version, name string, enabled bool) error {
	i, err := readInventory(paths, version)
	if err != nil {
		return err
	}
	defer i.root.Close()
	e, err := i.resolve(name)
	if err != nil {
		return err
	}
	if err := validateLoadingFile(e); err != nil {
		return err
	}
	f := e.files[0]
	from := filepath.Join("etc", "conf.d", f.physical)
	to := filepath.Join("etc", "conf.d", f.name)
	if !enabled {
		to += ".disabled"
	}
	if from != to {
		if err := i.root.Rename(from, to); err != nil {
			return err
		}
	}
	if e.packageKey != "" {
		meta := i.metadata.Extensions[e.packageKey]
		meta.Enabled = enabled
		meta.Module = e.Module
		meta.IniFile = f.name
		if meta.Binary == "" {
			meta.Binary = e.Binary
		}
		meta.Zend = f.zend
		i.metadata.Extensions[e.packageKey] = meta
		if err := i.saveMetadata(); err != nil {
			if from != to {
				_ = i.root.Rename(to, from)
			}
			return err
		}
	}
	return nil
}

func phpQuery(ctx context.Context, php string, args ...string) ([]byte, []byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := process.CommandContext(probeCtx, php, args...)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if probeCtx.Err() != nil {
		return nil, stderr.Bytes(), probeCtx.Err()
	}
	return output, stderr.Bytes(), err
}

func phpOutput(ctx context.Context, php string, args ...string) ([]byte, error) {
	output, _, err := phpQuery(ctx, php, args...)
	if err != nil {
		return nil, fmt.Errorf("query PHP extensions: %w", redact.Error(err, ""))
	}
	return output, nil
}

func loadedModules(output []byte) map[string]bool {
	out := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		out[canonicalModule(line)] = true
	}
	return out
}

func listInventory(paths *core.Paths, version string) ([]Extension, error) {
	i, err := readInventory(paths, version)
	if err != nil {
		return nil, err
	}
	defer i.root.Close()
	php := filepath.Join(paths.VersionBin(i.version), core.PHPBinary())
	output, err := phpOutput(context.Background(), php, "-m")
	if err != nil {
		return nil, err
	}
	loaded := loadedModules(output)
	output, err = phpOutput(context.Background(), php, "-n", "-m")
	if err != nil {
		return nil, err
	}
	builtin := loadedModules(output)
	output, err = phpOutput(context.Background(), php, "-r", `echo ini_get('extension_dir');`)
	if err != nil {
		return nil, err
	}
	extDir := strings.TrimSpace(string(output))
	var binaries *os.Root
	if extDir != "" {
		if !filepath.IsAbs(extDir) {
			extDir = filepath.Join(paths.VersionDir(i.version), extDir)
		}
		rel, err := filepath.Rel(paths.VersionDir(i.version), extDir)
		if err != nil {
			return nil, err
		}
		if !filepath.IsLocal(rel) {
			// External extension directory queries are read-only. Mutations remain
			// confined to the registered installation's conf.d and metadata.
			binaries, err = os.OpenRoot(extDir)
		} else {
			binaries, err = i.root.OpenRoot(rel)
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if binaries != nil {
			defer binaries.Close()
			dir, err := binaries.Open(".")
			if err != nil {
				return nil, err
			}
			files, err := dir.ReadDir(-1)
			_ = dir.Close()
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if !strings.HasSuffix(file.Name(), ".so") && !strings.HasSuffix(file.Name(), ".dll") && !strings.HasSuffix(file.Name(), ".dylib") {
					continue
				}
				module := binaryModule(file.Name())
				e := i.entries[module]
				if e == nil {
					e = &inventoryEntry{Extension: Extension{Name: module, Module: module, InstalledBy: "external"}}
					i.entries[module] = e
				}
				if e.Binary == "" {
					e.Binary = filepath.Join(extDir, file.Name())
				}
			}
		}
	}
	for module := range loaded {
		if i.entries[module] == nil {
			i.entries[module] = &inventoryEntry{Extension: Extension{Name: module, Module: module, InstalledBy: "external"}}
		}
	}
	var result []Extension
	for module, e := range i.entries {
		e.Loaded = loaded[module]
		if builtin[module] {
			e.InstalledBy = "builtin"
			e.Enabled = true
			e.State = "builtin"
		} else {
			exists := false
			if e.Binary != "" {
				path := e.Binary
				if filepath.IsAbs(path) {
					info, statErr := os.Lstat(path)
					exists = statErr == nil && info.Mode().IsRegular()
					path = ""
				}
				var info os.FileInfo
				if path != "" {
					if filepath.Base(path) == path && binaries != nil {
						info, err = binaries.Lstat(path)
					} else {
						info, err = i.root.Lstat(path)
					}
					exists = err == nil && info.Mode().IsRegular()
				}
			}
			switch {
			case len(e.files) > 1 || len(e.files) == 1 && e.files[0].modules > 1:
				e.State = "broken"
				e.Problem = "ambiguous loading configuration"
			case !exists && !e.Loaded:
				e.State = "missing"
				e.Problem = "extension binary missing"
			case len(e.files) > 0 && e.Enabled && !e.Loaded:
				e.State = "broken"
				e.Problem = "configured extension is not loaded"
			case len(e.files) > 0 && !e.Enabled && e.Loaded:
				e.State = "broken"
				e.Problem = "disabled extension is still loaded"
			case e.Loaded:
				e.State = "enabled"
				e.Enabled = true
			default:
				e.State = "disabled"
				e.Enabled = false
			}
		}
		result = append(result, e.Extension)
	}
	sort.Slice(result, func(a, b int) bool { return strings.ToLower(result[a].Name) < strings.ToLower(result[b].Name) })
	return result, nil
}
