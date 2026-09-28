package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolverPaths(t *testing.T) *Paths {
	t.Helper()
	p := NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.VersionBin("8.3.30"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.VersionBin("8.3.30"), PHPBinary()), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstalledResolverRejectsCorruptNumericAlias(t *testing.T) {
	for _, tt := range []struct{ name, contents, want string }{
		{"empty", "", "empty alias"}, {"invalid target", "../../outside", "invalid name"}, {"cycle", "8.3", "alias cycle"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := resolverPaths(t)
			if err := os.WriteFile(p.AliasFile("8.3"), []byte(tt.contents), 0600); err != nil {
				t.Fatal(err)
			}
			if version, err := NewInstalledManager(p).Resolve("8.3"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("version=%q error=%v, want %q", version, err, tt.want)
			}
		})
	}
}

func TestInstalledResolverChainLimit(t *testing.T) {
	for _, length := range []int{10, 11} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			p := resolverPaths(t)
			aliases := NewAliasManager(p)
			for i := 0; i < length; i++ {
				target := "8.3"
				if i+1 < length {
					target = fmt.Sprintf("alias%d", i+1)
				}
				if err := aliases.Set(fmt.Sprintf("alias%d", i), target); err != nil {
					t.Fatal(err)
				}
			}
			version, err := NewInstalledManager(p).Resolve("alias0")
			if length == 10 && (err != nil || version != "8.3.30") {
				t.Errorf("valid chain: %s %v", version, err)
			}
			if length == 11 && (err == nil || !strings.Contains(err.Error(), "10 links")) {
				t.Errorf("long chain was accepted: %s %v", version, err)
			}
		})
	}
}

func TestInstalledResolverEmptyAndMissingAliases(t *testing.T) {
	p := NewPaths(t.TempDir())
	for _, input := range []string{"latest", "stable", "8.3"} {
		if version, err := NewInstalledManager(p).Resolve(input); err == nil {
			t.Errorf("empty installation resolved %s to %s", input, version)
		}
	}
	p = resolverPaths(t)
	if err := os.Remove(p.Alias); err != nil {
		t.Fatal(err)
	}
	if version, err := NewInstalledManager(p).Resolve("v8.3"); err != nil || version != "8.3.30" {
		t.Errorf("missing alias directory: %s %v", version, err)
	}
}

func TestInstalledListIgnoresNoncanonicalDirectories(t *testing.T) {
	p := resolverPaths(t)
	for _, version := range []string{"v8.3.30", "08.3.30"} {
		if err := os.Mkdir(p.VersionDir(version), 0755); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := NewInstalledManager(p).List()
	if err != nil || len(versions) != 1 || versions[0] != "8.3.30" {
		t.Errorf("listed aliases as installations: %v %v", versions, err)
	}
}
