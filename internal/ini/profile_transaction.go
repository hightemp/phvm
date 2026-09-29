package ini

import (
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
	"github.com/hightemp/phvm/internal/ext"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
)

type profileManifest struct {
	Format int    `json:"format"`
	Mode   string `json:"mode"`
}
type backupManifest struct {
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func copyDirectoryModes(src, dst *os.Root, path string) error {
	dir, err := src.Open(path)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := copyDirectoryModes(src, dst, filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	info, err := src.Stat(path)
	if err != nil {
		return err
	}
	return dst.Chmod(path, info.Mode().Perm())
}

func validateProfileTree(root *os.Root, snapshot bool) error {
	info, err := root.Lstat("php.ini")
	if err != nil {
		return fmt.Errorf("profile php.ini: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("profile php.ini must be regular")
	}
	info, err = root.Lstat("conf.d")
	if os.IsNotExist(err) && !snapshot {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("profile conf.d must be a directory")
	}
	conf, err := root.OpenRoot("conf.d")
	if err != nil {
		return err
	}
	defer conf.Close()
	dir, err := conf.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	seen := make(map[string]string)
	for _, entry := range entries {
		if err := fsutil.ValidateName(entry.Name()); err != nil {
			return err
		}
		info, err := conf.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("conf.d entries must be regular files: %s", entry.Name())
		}
		base := strings.TrimSuffix(strings.ToLower(entry.Name()), ".disabled")
		if strings.HasSuffix(base, ".ini") {
			if other, ok := seen[base]; ok {
				return fmt.Errorf("ambiguous enabled/disabled ini pair: %s and %s", other, entry.Name())
			}
			seen[base] = entry.Name()
		}
	}
	return nil
}

func copyIniFile(src, dst *os.Root) error {
	info, err := src.Lstat("php.ini")
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("php.ini must be regular")
	}
	data, err := src.ReadFile("php.ini")
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWriteRoot(dst, "php.ini", data, info.Mode().Perm()); err != nil {
		return err
	}
	return nil
}

func copyConfiguration(src, dst *os.Root) error {
	if err := copyIniFile(src, dst); err != nil {
		return err
	}
	if err := dst.Mkdir("conf.d", 0755); err != nil && !os.IsExist(err) {
		return err
	}
	conf, err := src.OpenRoot("conf.d")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer conf.Close()
	to, err := dst.OpenRoot("conf.d")
	if err != nil {
		return err
	}
	defer to.Close()
	if err := fsutil.CopyRootTree(conf, to); err != nil {
		return err
	}
	return copyDirectoryModes(conf, to, ".")
}

func (m *ProfileManager) save(ctx context.Context, name, version string) error {
	if err := fsutil.ValidateName(name); err != nil {
		return err
	}
	version, err := m.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	source, err := m.paths.OpenDataDir(m.paths.VersionEtc(version), false)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := validateProfileTree(source, false); err != nil {
		return err
	}
	root, err := m.paths.OpenDataDir(m.paths.Profiles, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if existing, err := m.paths.OpenDataDir(m.paths.ProfileDir(name), false); err == nil {
		_ = existing.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	stage := ".profile-stage-" + fsutil.RandomSuffix()
	if err := root.Mkdir(stage, 0700); err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = root.RemoveAll(stage)
		}
	}()
	if err := root.Mkdir(filepath.Join(stage, "candidate"), 0755); err != nil {
		return err
	}
	candidate, err := root.OpenRoot(filepath.Join(stage, "candidate"))
	if err != nil {
		return err
	}
	err = copyConfiguration(source, candidate)
	if err == nil {
		data, _ := json.Marshal(profileManifest{Format: 1, Mode: "snapshot"})
		err = fsutil.AtomicWriteRoot(candidate, "profile.json", data, 0644)
	}
	if err == nil {
		err = validateProfileTree(candidate, true)
	}
	_ = candidate.Close()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, oldErr := root.Lstat(name)
	hadPrevious := oldErr == nil
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return oldErr
	}
	if hadPrevious {
		if err := root.Rename(name, filepath.Join(stage, "previous")); err != nil {
			return err
		}
	}
	if err := root.Rename(filepath.Join(stage, "candidate"), name); err != nil {
		if hadPrevious {
			if restoreErr := root.Rename(filepath.Join(stage, "previous"), name); restoreErr != nil {
				keep = true
				return errors.Join(err, restoreErr)
			}
		}
		return err
	}
	return nil
}

func (m *ProfileManager) apply(ctx context.Context, name, version string, backup bool) error {
	version, err := m.paths.CheckVersionPath(version)
	if err != nil {
		return err
	}
	profile, err := m.Get(name)
	if err != nil {
		return err
	}
	source, err := m.paths.OpenDataDir(profile.Path, false)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := validateProfileTree(source, profile.Mode == "snapshot"); err != nil {
		return err
	}
	root, err := m.paths.OpenVersion(version, false)
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := root.OpenRoot("etc")
	if err != nil {
		return err
	}
	defer current.Close()
	stage := ".ini-profile-" + fsutil.RandomSuffix()
	if err := root.Mkdir(stage, 0700); err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = root.RemoveAll(stage)
		}
	}()
	if err := root.Mkdir(filepath.Join(stage, "candidate"), 0755); err != nil {
		return err
	}
	candidate, err := root.OpenRoot(filepath.Join(stage, "candidate"))
	if err != nil {
		return err
	}
	err = fsutil.CopyRootTree(current, candidate)
	if err == nil {
		err = candidate.Remove("php.ini")
	}
	if err == nil && profile.Mode == "snapshot" {
		err = candidate.RemoveAll("conf.d")
	}
	if err == nil {
		if profile.Mode == "snapshot" {
			err = copyConfiguration(source, candidate)
		} else {
			err = copyIniFile(source, candidate)
			if err == nil {
				if mkdirErr := candidate.Mkdir("conf.d", 0755); mkdirErr != nil && !os.IsExist(mkdirErr) {
					err = mkdirErr
				}
			}
			if err == nil {
				from, openErr := current.OpenRoot("conf.d")
				if openErr == nil {
					to, toErr := candidate.OpenRoot("conf.d")
					if toErr == nil {
						err = copyDirectoryModes(from, to, ".")
						_ = to.Close()
					} else {
						err = toErr
					}
					_ = from.Close()
				} else if !os.IsNotExist(openErr) {
					err = openErr
				}
			}
		}
	}
	if err == nil {
		err = copyUnchangedDirectoryModes(current, candidate, ".")
	}
	if err == nil {
		err = validateProfileTree(candidate, true)
	}
	_ = candidate.Close()
	if err != nil {
		return err
	}
	candidatePath := filepath.Join(m.paths.VersionDir(version), stage, "candidate")
	php := filepath.Join(m.paths.VersionBin(version), core.PHPBinary())
	if err := validateConfiguration(ctx, php, candidatePath); err != nil {
		return err
	}
	oldMetadata, metadata, err := profileMetadata(root, filepath.Join(stage, "candidate"))
	if err != nil {
		return err
	}
	backupName := ""
	if backup {
		backupName = "etc.backup-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + fsutil.RandomSuffix()
		if err := root.Mkdir(filepath.Join(stage, "backup"), 0700); err != nil {
			return err
		}
		to, err := root.OpenRoot(filepath.Join(stage, "backup"))
		if err != nil {
			return err
		}
		err = fsutil.CopyRootTree(current, to)
		if err == nil {
			data, _ := json.Marshal(backupManifest{Version: version, Kind: "phvm-ini-backup"})
			err = fsutil.AtomicWriteRoot(to, ".phvm-ini-backup.json", data, 0600)
		}
		if err == nil && oldMetadata != nil {
			err = fsutil.AtomicWriteRoot(to, ".phvm-version-metadata.json", oldMetadata, 0600)
		}
		_ = to.Close()
		if err != nil {
			return err
		}
	}
	_ = current.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Rename("etc", filepath.Join(stage, "previous")); err != nil {
		return err
	}
	if err := root.Rename(filepath.Join(stage, "candidate"), "etc"); err != nil {
		if restoreErr := root.Rename(filepath.Join(stage, "previous"), "etc"); restoreErr != nil {
			keep = true
			return errors.Join(err, restoreErr)
		}
		return err
	}
	metadataWritten := false
	metadataMode := os.FileMode(0644)
	if info, err := root.Lstat(".phvm-metadata.json"); err == nil {
		metadataMode = info.Mode().Perm()
	}
	rollback := func(cause error) error {
		if metadataWritten {
			if err := fsutil.AtomicWriteRoot(root, ".phvm-metadata.json", oldMetadata, metadataMode); err != nil {
				keep = true
				cause = errors.Join(cause, err)
			}
		}
		if err := root.RemoveAll("etc"); err != nil {
			keep = true
			return errors.Join(cause, err)
		}
		if err := root.Rename(filepath.Join(stage, "previous"), "etc"); err != nil {
			keep = true
			return errors.Join(cause, err)
		}
		return cause
	}
	if err := validateConfiguration(ctx, php, m.paths.VersionEtc(version)); err != nil {
		return rollback(err)
	}
	if metadata != nil {
		if err := fsutil.AtomicWriteRoot(root, ".phvm-metadata.json", metadata, metadataMode); err != nil {
			return rollback(err)
		}
		metadataWritten = true
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if backup {
		if err := root.Rename(filepath.Join(stage, "backup"), backupName); err != nil {
			return rollback(err)
		}
		if err := pruneProfileBackups(root, version, m.backupLimit); err != nil {
			log.Warn("Could not prune ini backup history: %v", err)
		}
	}
	return nil
}

func copyUnchangedDirectoryModes(src, dst *os.Root, path string) error {
	dir, err := src.Open(path)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			name := filepath.Join(path, entry.Name())
			if name == "conf.d" {
				continue
			}
			if err := copyUnchangedDirectoryModes(src, dst, name); err != nil {
				return err
			}
		}
	}
	info, err := src.Stat(path)
	if err != nil {
		return err
	}
	return dst.Chmod(path, info.Mode().Perm())
}

func profileMetadata(root *os.Root, candidate string) ([]byte, []byte, error) {
	data, err := root.ReadFile(".phvm-metadata.json")
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var metadata core.Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, nil, fmt.Errorf("read extension metadata: %w", err)
	}
	for pkg, entry := range metadata.Extensions {
		if entry.IniFile == "" {
			continue
		}
		if err := fsutil.ValidateName(entry.IniFile); err != nil {
			return nil, nil, err
		}
		info, err := root.Lstat(filepath.Join(candidate, "conf.d", entry.IniFile))
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, err
		}
		entry.Enabled = false
		if err == nil && info.Mode().IsRegular() {
			content, err := root.ReadFile(filepath.Join(candidate, "conf.d", entry.IniFile))
			if err != nil {
				return nil, nil, err
			}
			module := entry.Module
			if module == "" {
				module = pkg
			}
			entry.Enabled = ext.ConfigurationLoadsModule(content, module)
		}
		metadata.Extensions[pkg] = entry
	}
	updated, err := json.MarshalIndent(metadata, "", "  ")
	return data, updated, err
}

func pruneProfileBackups(root *os.Root, version string, limit int) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "etc.backup-") {
			continue
		}
		data, err := root.ReadFile(filepath.Join(entry.Name(), ".phvm-ini-backup.json"))
		if err != nil {
			continue
		}
		var manifest backupManifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Kind != "phvm-ini-backup" || manifest.Version != version {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for len(names) > limit {
		if err := root.RemoveAll(names[0]); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}
