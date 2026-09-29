package core

import (
	"os"
	"testing"
)

func TestInstalledRejectsIncompleteOrCorruptPublicationState(t *testing.T) {
	for _, metadata := range []string{`{"version":"8.3.30","installation_state":"staging"}`, `{"version":"8.3.30","installation_state":"failed"}`, `{"version":"8.2.30","installation_state":"ready"}`, `{broken`} {
		t.Run(metadata, func(t *testing.T) {
			p := resolverPaths(t)
			if err := os.WriteFile(p.VersionMetadata("8.3.30"), []byte(metadata), 0600); err != nil {
				t.Fatal(err)
			}
			if NewInstalledManager(p).IsInstalled("8.3.30") {
				t.Error("incomplete/corrupt publication accepted")
			}
			if versions, err := NewInstalledManager(p).List(); err != nil || len(versions) != 0 {
				t.Errorf("incomplete publication listed: %v %v", versions, err)
			}
		})
	}
}
