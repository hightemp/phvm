package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallTransaction records the recoverable directory publication protocol.
type InstallTransaction struct {
	Version     string `json:"version"`
	ID          string `json:"id"`
	Phase       string `json:"phase"`
	HadPrevious bool   `json:"had_previous"`
}

// RecoverInstallTransactions rolls back interrupted publication or cleans up
// completed transactions. The caller must hold the shared state lock.
func (p *Paths) RecoverInstallTransactions(ctx context.Context) error {
	root, err := p.OpenDataDir(p.Versions, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, ok := strings.CutPrefix(entry.Name(), ".php-install-")
		if !ok || len(id) != 16 {
			continue
		}
		if _, err := hex.DecodeString(id); err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			return fmt.Errorf("installation transaction must be a directory")
		}
		txRoot, err := root.OpenRoot(entry.Name())
		if err != nil {
			return err
		}
		data, err := txRoot.ReadFile("transaction.json")
		_ = txRoot.Close()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var tx InstallTransaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return fmt.Errorf("read installation transaction: %w", err)
		}
		version, err := NormalizeInstalledVersion(tx.Version)
		if err != nil || version != tx.Version || tx.ID != id {
			return fmt.Errorf("invalid installation transaction identity")
		}
		if err := recoverInstallation(root, entry.Name(), tx); err != nil {
			return err
		}
	}
	return nil
}

func recoverInstallation(root *os.Root, name string, tx InstallTransaction) error {
	backup := filepath.Join(name, "previous")
	backupInfo, backupErr := root.Lstat(backup)
	if backupErr != nil && !os.IsNotExist(backupErr) {
		return backupErr
	}
	if backupErr == nil && !backupInfo.IsDir() {
		return fmt.Errorf("previous installation backup must be a directory")
	}
	finalInfo, finalErr := root.Lstat(tx.Version)
	if finalErr != nil && !os.IsNotExist(finalErr) {
		return finalErr
	}
	if finalErr == nil && !finalInfo.IsDir() {
		return fmt.Errorf("published installation must be a directory")
	}
	switch tx.Phase {
	case "building":
		if backupErr == nil {
			return fmt.Errorf("unexpected backup in building transaction")
		}
	case "publishing":
		if backupErr == nil && !tx.HadPrevious {
			return fmt.Errorf("unexpected previous installation")
		}
		if finalErr == nil && (backupErr == nil || !tx.HadPrevious) {
			if err := checkInstallationOwner(root, tx.Version, tx.ID); err != nil {
				return err
			}
			if err := root.RemoveAll(tx.Version); err != nil {
				return err
			}
		}
		if backupErr == nil {
			if err := root.Rename(backup, tx.Version); err != nil {
				return fmt.Errorf("restore interrupted PHP installation: %w", err)
			}
		} else if tx.HadPrevious && os.IsNotExist(finalErr) {
			return fmt.Errorf("previous PHP installation and backup are both missing")
		}
	case "committed":
		if finalErr != nil {
			return fmt.Errorf("committed PHP installation is missing; preserving backup")
		}
		if err := checkInstallationOwner(root, tx.Version, tx.ID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown installation transaction phase")
	}
	return root.RemoveAll(name)
}

func checkInstallationOwner(root *os.Root, version, id string) error {
	data, err := root.ReadFile(filepath.Join(version, ".phvm-metadata.json"))
	if err != nil {
		return err
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	if metadata.InstallationID != id || metadata.Version != version {
		return fmt.Errorf("installation ownership changed; preserving transaction")
	}
	return nil
}
