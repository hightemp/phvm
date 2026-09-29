package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/fsutil"
	"github.com/hightemp/phvm/internal/log"
	"github.com/hightemp/phvm/internal/redact"
)

type phpTransaction struct {
	root                                       *os.Root
	name, candidateRel, candidate, installRoot string
	journal                                    core.InstallTransaction
	previousMetadata                           *core.Metadata
	keep                                       bool
}

func newPHPTransaction(paths *core.Paths, version, final string) (*phpTransaction, error) {
	root, err := paths.OpenDataDir(paths.Versions, true)
	if err != nil {
		return nil, err
	}
	versionsDir, err := filepath.Abs(paths.Versions)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	id := fsutil.RandomSuffix()
	name := ".php-install-" + id
	if err := root.Mkdir(name, 0700); err != nil {
		_ = root.Close()
		return nil, err
	}
	relPrefix := strings.TrimLeft(strings.TrimPrefix(final, filepath.VolumeName(final)), string(filepath.Separator))
	tx := &phpTransaction{root: root, name: name, candidateRel: filepath.Join(name, "root", relPrefix), installRoot: filepath.Join(versionsDir, name, "root"), journal: core.InstallTransaction{Version: version, ID: id, Phase: "building"}}
	tx.candidate = filepath.Join(versionsDir, tx.candidateRel)
	if _, err := root.Lstat(version); err == nil {
		tx.journal.HadPrevious = true
	} else if !os.IsNotExist(err) {
		tx.close()
		return nil, err
	}
	if err := tx.writeJournal(); err != nil {
		tx.close()
		return nil, err
	}
	return tx, nil
}

func (tx *phpTransaction) writeJournal() error {
	data, err := json.Marshal(tx.journal)
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteRoot(tx.root, filepath.Join(tx.name, "transaction.json"), data, 0600)
}

func (tx *phpTransaction) close() {
	if !tx.keep {
		if err := tx.root.RemoveAll(tx.name); err != nil {
			log.Warn("Could not clean installation transaction: %v", err)
		}
	}
	_ = tx.root.Close()
}

func (tx *phpTransaction) preservePrevious() error {
	if !tx.journal.HadPrevious {
		return nil
	}
	previous, err := tx.root.OpenRoot(tx.journal.Version)
	if err != nil {
		return err
	}
	defer previous.Close()
	data, err := previous.ReadFile(".phvm-metadata.json")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		tx.previousMetadata = core.NewMetadata(tx.journal.Version)
		if err := json.Unmarshal(data, tx.previousMetadata); err != nil {
			return err
		}
	}
	candidate, err := tx.root.OpenRoot(tx.candidateRel)
	if err != nil {
		return err
	}
	defer candidate.Close()
	if err := copyPreviousFiles(previous, candidate, "."); err != nil {
		return err
	}
	if tx.previousMetadata != nil {
		for _, entry := range tx.previousMetadata.Extensions {
			if entry.BinarySHA256 == "" {
				continue
			}
			if !filepath.IsLocal(entry.Binary) || !strings.HasSuffix(entry.Binary, ".so") && !strings.HasSuffix(entry.Binary, ".dll") && !strings.HasSuffix(entry.Binary, ".dylib") {
				return fmt.Errorf("invalid owned extension binary path")
			}
			info, err := previous.Lstat(entry.Binary)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("owned extension binary must be regular")
			}
			data, err := previous.ReadFile(entry.Binary)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != entry.BinarySHA256 {
				return fmt.Errorf("owned extension binary changed")
			}
			if err := fsutil.AtomicWriteRoot(candidate, entry.Binary, data, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyPreviousFiles(previous, candidate *os.Root, path string) error {
	dir, err := previous.Open(path)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := filepath.Join(path, entry.Name())
		if name == ".phvm-metadata.json" {
			continue
		}
		info, err := previous.Lstat(name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := candidate.MkdirAll(name, 0755); err != nil {
				return err
			}
			if err := copyPreviousFiles(previous, candidate, name); err != nil {
				return err
			}
			if name == "etc" || strings.HasPrefix(name, "etc"+string(filepath.Separator)) {
				if err := candidate.Chmod(name, info.Mode().Perm()); err != nil {
					return err
				}
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("previous installation contains a nonregular file: %s", name)
		}
		_, err = candidate.Lstat(name)
		if err == nil && !strings.HasPrefix(name, "etc"+string(filepath.Separator)) {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		data, err := previous.ReadFile(name)
		if err != nil {
			return err
		}
		if err := fsutil.AtomicWriteRoot(candidate, name, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) candidateMetadata(opts BuildOptions, duration time.Duration, previous *core.Metadata) (*core.Metadata, error) {
	metadata := core.NewMetadata(opts.Version)
	metadata.SourceURL, metadata.SHA256 = redact.URL(opts.SourceURL), opts.SHA256
	if opts.Verification != nil {
		result := opts.Verification
		if !result.SHA256Verified {
			return nil, fmt.Errorf("missing successful SHA256 verification")
		}
		metadata.SHA256Verified, metadata.GPGVerified, metadata.GPGSkipped = result.SHA256Verified, result.GPGVerified, result.GPGSkipped
		metadata.GPGSkipReason, metadata.GPGFingerprint = result.GPGSkipReason, result.GPGFingerprint
	} else if opts.SourceURL != "" || opts.SHA256 != "" {
		return nil, fmt.Errorf("source metadata requires successful verification")
	}
	metadata.ConfigureFlags = b.configureArguments(opts.Version, b.paths.VersionDir(opts.Version))
	metadata.BuildProfile, metadata.BuildDuration = b.profile.Name, int64(duration.Seconds())
	if previous != nil {
		metadata.Extensions = previous.Extensions
	}
	return metadata, nil
}

func (tx *phpTransaction) writeMetadata(metadata *core.Metadata) error {
	metadata.InstallationID = tx.journal.ID
	root, err := tx.root.OpenRoot(tx.candidateRel)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Lstat(".phvm-metadata.json"); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("candidate metadata must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteRoot(root, ".phvm-metadata.json", data, 0644)
}

func (tx *phpTransaction) publish(ctx context.Context, validate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.journal.Phase = "publishing"
	if err := tx.writeJournal(); err != nil {
		return err
	}
	backup := filepath.Join(tx.name, "previous")
	if tx.journal.HadPrevious {
		if err := tx.root.Rename(tx.journal.Version, backup); err != nil {
			return err
		}
	}
	if err := tx.root.Rename(tx.candidateRel, tx.journal.Version); err != nil {
		if tx.journal.HadPrevious {
			if restore := tx.root.Rename(backup, tx.journal.Version); restore != nil {
				tx.keep = true
				return errors.Join(err, restore)
			}
		}
		return err
	}
	rollback := func(cause error) error {
		if err := tx.root.RemoveAll(tx.journal.Version); err != nil {
			tx.keep = true
			return errors.Join(cause, fmt.Errorf("remove failed candidate: %w", err))
		}
		if tx.journal.HadPrevious {
			if err := tx.root.Rename(backup, tx.journal.Version); err != nil {
				tx.keep = true
				return errors.Join(cause, fmt.Errorf("restore previous PHP: %w", err))
			}
		}
		return cause
	}
	if err := validate(); err != nil {
		return rollback(fmt.Errorf("validate published PHP: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	tx.journal.Phase = "committed"
	if err := tx.writeJournal(); err != nil {
		return rollback(err)
	}
	return nil
}
