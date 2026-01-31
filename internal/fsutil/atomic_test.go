package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world")

	err = AtomicWriteFile(path, content, 0644)
	if err != nil {
		t.Errorf("AtomicWriteFile() error = %v", err)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("ReadFile() error = %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("File content = %q, want %q", string(data), string(content))
	}

	// Verify permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("File permissions = %o, want 0644", info.Mode().Perm())
	}
}

func TestAtomicWriteFileCreatesDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Path with non-existent directory
	path := filepath.Join(tmpDir, "subdir", "nested", "test.txt")
	content := []byte("test")

	err = AtomicWriteFile(path, content, 0644)
	if err != nil {
		t.Errorf("AtomicWriteFile() error = %v", err)
	}

	if !Exists(path) {
		t.Error("File was not created")
	}
}

func TestExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test existing file
	existingFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(existingFile, []byte("test"), 0644)

	if !Exists(existingFile) {
		t.Error("Exists() = false for existing file")
	}

	// Test non-existing file
	if Exists(filepath.Join(tmpDir, "notexists.txt")) {
		t.Error("Exists() = true for non-existing file")
	}
}

func TestIsDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if !IsDir(tmpDir) {
		t.Error("IsDir() = false for directory")
	}

	file := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(file, []byte("test"), 0644)

	if IsDir(file) {
		t.Error("IsDir() = true for file")
	}
}

func TestIsFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	file := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(file, []byte("test"), 0644)

	if !IsFile(file) {
		t.Error("IsFile() = false for file")
	}

	if IsFile(tmpDir) {
		t.Error("IsFile() = true for directory")
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	newDir := filepath.Join(tmpDir, "new", "nested", "dir")

	err = EnsureDir(newDir)
	if err != nil {
		t.Errorf("EnsureDir() error = %v", err)
	}

	if !IsDir(newDir) {
		t.Error("Directory was not created")
	}

	// Should not error on existing directory
	err = EnsureDir(newDir)
	if err != nil {
		t.Errorf("EnsureDir() on existing dir error = %v", err)
	}
}

func TestRemoveIfExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with existing file
	file := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(file, []byte("test"), 0644)

	err = RemoveIfExists(file)
	if err != nil {
		t.Errorf("RemoveIfExists() error = %v", err)
	}
	if Exists(file) {
		t.Error("File was not removed")
	}

	// Test with non-existing file (should not error)
	err = RemoveIfExists(filepath.Join(tmpDir, "notexists.txt"))
	if err != nil {
		t.Errorf("RemoveIfExists() on non-existing file error = %v", err)
	}
}

func TestCopyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "phvm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory structure
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("file1"), 0644)
	os.WriteFile(filepath.Join(srcDir, "subdir", "file2.txt"), []byte("file2"), 0644)

	// Copy
	dstDir := filepath.Join(tmpDir, "dst")
	err = CopyDir(srcDir, dstDir)
	if err != nil {
		t.Errorf("CopyDir() error = %v", err)
	}

	// Verify
	if !IsDir(dstDir) {
		t.Error("Destination directory not created")
	}
	if !IsFile(filepath.Join(dstDir, "file1.txt")) {
		t.Error("file1.txt not copied")
	}
	if !IsFile(filepath.Join(dstDir, "subdir", "file2.txt")) {
		t.Error("subdir/file2.txt not copied")
	}

	// Verify content
	data, _ := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if string(data) != "file1" {
		t.Errorf("file1.txt content = %q, want 'file1'", string(data))
	}
}
