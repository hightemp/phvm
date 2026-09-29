package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverInterruptedPHPPublication(t *testing.T) {
	for _, phase := range []string{"building", "old moved", "candidate moved", "committed", "new interrupted", "foreign owner"} {
		t.Run(phase, func(t *testing.T) {
			p := resolverPaths(t)
			if err := NewCurrentManager(p).Set("8.3.30"); err != nil {
				t.Fatal(err)
			}
			const id = "0123456789abcdef"
			name := filepath.Join(p.Versions, ".php-install-"+id)
			if err := os.MkdirAll(name, 0700); err != nil {
				t.Fatal(err)
			}
			tx := InstallTransaction{ID: id, Version: "8.3.30", Phase: "publishing", HadPrevious: true}
			if phase == "building" {
				tx.Phase = "building"
			}
			if phase == "committed" {
				tx.Phase = "committed"
			}
			if phase != "building" && phase != "new interrupted" {
				if err := os.Rename(p.VersionDir("8.3.30"), filepath.Join(name, "previous")); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "new interrupted" {
				tx.HadPrevious = false
				if err := os.RemoveAll(p.VersionDir("8.3.30")); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "candidate moved" || phase == "committed" || phase == "new interrupted" || phase == "foreign owner" {
				if err := os.MkdirAll(p.VersionBin("8.3.30"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), PHPBinary()), []byte("new candidate"), 0755); err != nil {
					t.Fatal(err)
				}
				meta := NewMetadata("8.3.30")
				meta.InstallationID = id
				meta.InstallationState = "ready"
				if phase == "foreign owner" {
					meta.InstallationID = "fedcba9876543210"
				}
				if err := meta.Save(p.VersionMetadata("8.3.30")); err != nil {
					t.Fatal(err)
				}
			}
			data, err := json.Marshal(tx)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(name, "transaction.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			err = p.WithStateLock(context.Background(), func(context.Context) error { return nil })
			if phase == "foreign owner" {
				if err == nil {
					t.Error("foreign publication ownership accepted")
				}
				if data, err := os.ReadFile(filepath.Join(p.VersionBin("8.3.30"), PHPBinary())); err != nil || string(data) != "new candidate" {
					t.Error("foreign installation was removed")
				}
				if _, err := os.Stat(filepath.Join(name, "previous")); err != nil {
					t.Error("backup was removed after refused recovery")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if phase == "new interrupted" {
				if _, err := os.Stat(p.VersionDir("8.3.30")); !os.IsNotExist(err) {
					t.Error("uncommitted new installation survived")
				}
			} else {
				want := "fixture"
				if phase == "committed" {
					want = "new candidate"
				}
				if data, err := os.ReadFile(filepath.Join(p.VersionBin("8.3.30"), PHPBinary())); err != nil || string(data) != want {
					t.Errorf("recovered PHP=%q want=%q error=%v", data, want, err)
				}
			}
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Error("recovered transaction not cleaned")
			}
			if err := p.WithStateLock(context.Background(), func(context.Context) error { return nil }); err != nil {
				t.Errorf("repeated recovery failed: %v", err)
			}
		})
	}
}
