package ini

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfDManager(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewConfDManager(tmpDir)

	// Test Create
	t.Run("Create", func(t *testing.T) {
		err := mgr.Create("opcache.ini", "; opcache config\nopcache.enable=1\n", 10)
		if err != nil {
			t.Errorf("Create() error = %v", err)
		}

		// Verify file exists with priority prefix
		path := filepath.Join(tmpDir, "10-opcache.ini")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("File not created at expected path: %s", path)
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		files, err := mgr.List()
		if err != nil {
			t.Errorf("List() error = %v", err)
		}
		if len(files) != 1 {
			t.Errorf("List() returned %d files, want 1", len(files))
		}
		if files[0].Name != "10-opcache.ini" {
			t.Errorf("List()[0].Name = %s, want 10-opcache.ini", files[0].Name)
		}
		if !files[0].Enabled {
			t.Errorf("List()[0].Enabled = false, want true")
		}
	})

	// Test Disable
	t.Run("Disable", func(t *testing.T) {
		err := mgr.Disable("10-opcache.ini")
		if err != nil {
			t.Errorf("Disable() error = %v", err)
		}

		files, _ := mgr.List()
		if len(files) != 1 {
			t.Fatalf("Expected 1 file after disable")
		}
		if files[0].Enabled {
			t.Errorf("File should be disabled after Disable()")
		}
	})

	// Test Enable
	t.Run("Enable", func(t *testing.T) {
		err := mgr.Enable("10-opcache.ini")
		if err != nil {
			t.Errorf("Enable() error = %v", err)
		}

		files, _ := mgr.List()
		if len(files) != 1 {
			t.Fatalf("Expected 1 file after enable")
		}
		if !files[0].Enabled {
			t.Errorf("File should be enabled after Enable()")
		}
	})

	// Test IsEnabled
	t.Run("IsEnabled", func(t *testing.T) {
		if !mgr.IsEnabled("10-opcache.ini") {
			t.Error("IsEnabled() = false, want true")
		}
	})

	// Test Remove
	t.Run("Remove", func(t *testing.T) {
		err := mgr.Remove("10-opcache.ini")
		if err != nil {
			t.Errorf("Remove() error = %v", err)
		}

		files, _ := mgr.List()
		if len(files) != 0 {
			t.Errorf("List() returned %d files after remove, want 0", len(files))
		}
	})
}

func TestExtractPriority(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"10-opcache.ini", 10},
		{"20-xdebug.ini", 20},
		{"99-custom.ini", 99},
		{"opcache.ini", 99}, // No priority prefix
		{"5-short.ini", 99}, // Invalid prefix (single digit)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPriority(tt.name); got != tt.expected {
				t.Errorf("extractPriority(%q) = %d, want %d", tt.name, got, tt.expected)
			}
		})
	}
}

func TestNormalizeIniName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"opcache", "opcache.ini"},
		{"opcache.ini", "opcache.ini"},
		{"opcache.ini.disabled", "opcache.ini"},
		{"/path/to/opcache.ini", "opcache.ini"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeIniName(tt.input); got != tt.expected {
				t.Errorf("normalizeIniName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
