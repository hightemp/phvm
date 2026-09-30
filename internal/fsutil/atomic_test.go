package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type faultingAtomicTemp struct {
	*os.File
	stage string
	fault error
}

func (f *faultingAtomicTemp) Write(p []byte) (int, error) {
	if f.stage == "write" {
		n, _ := f.File.Write(p[:1])
		return n, f.fault
	}
	return f.File.Write(p)
}

func (f *faultingAtomicTemp) Chmod(mode os.FileMode) error {
	if f.stage == "chmod" {
		return f.fault
	}
	return f.File.Chmod(mode)
}

func (f *faultingAtomicTemp) Sync() error {
	if f.stage == "sync" {
		return f.fault
	}
	return f.File.Sync()
}

func (f *faultingAtomicTemp) Close() error {
	err := f.File.Close()
	if err == nil && f.stage == "close" {
		return f.fault
	}
	return err
}

func TestAtomicReplacementCleansEveryFailureStage(t *testing.T) {
	for _, stage := range []string{"write", "chmod", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("old data"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(dir, ".tmp-preserve")
			if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			fault := errors.New("injected " + stage + " failure")
			create := func(dir string) (atomicTempFile, error) {
				file, err := os.CreateTemp(dir, ".tmp-*")
				if err != nil {
					return nil, err
				}
				return &faultingAtomicTemp{File: file, stage: stage, fault: fault}, nil
			}
			write := func(file atomicTempFile) error {
				if _, err := file.Write([]byte("new data")); err != nil {
					return fmt.Errorf("write temp file: %w", err)
				}
				return nil
			}
			rename := os.Rename
			if stage == "rename" {
				rename = func(_, _ string) error { return fault }
			}
			if err := atomicReplace(target, 0644, write, create, rename); !errors.Is(err, fault) {
				t.Fatalf("want stage error, got %v", err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old data" {
				t.Errorf("prior destination changed: %q, %v", data, err)
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != before.Mode().Perm() {
				t.Errorf("prior destination mode changed: %v, %v", info, err)
			}
			files, err := filepath.Glob(filepath.Join(dir, ".tmp-*"))
			if err != nil || len(files) != 1 || files[0] != unrelated {
				t.Errorf("temp cleanup touched another file or left staging: %v, %v", files, err)
			}
		})
	}
}

func TestAtomicFilesRemoveOnlyTheirOwnTempOnRenameFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(string, string) error
	}{
		{"write", func(src, dst string) error { return AtomicWriteFile(dst, []byte("new data"), 0600) }},
		{"copy", func(src, dst string) error { return AtomicCopyFile(src, dst, 0600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "existing-directory")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(target, "original")
			if err := os.WriteFile(original, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(dir, ".tmp-preserve")
			if err := os.WriteFile(unrelated, []byte("unrelated"), 0600); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(dir, "source")
			if err := os.WriteFile(source, []byte("new data"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(source, target); err == nil || !strings.Contains(err.Error(), "rename temp file") {
				t.Fatalf("want rename error, got %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".tmp-") && entry.Name() != filepath.Base(unrelated) {
					t.Errorf("failed operation left temp %q", entry.Name())
				}
			}
			for path, want := range map[string]string{original: "keep", unrelated: "unrelated", source: "new data"} {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Errorf("%s = %q, %v; want %q", path, got, err, want)
				}
			}
		})
	}
}

func TestAtomicFilesReplaceExistingFileWithoutTemp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(string, string) error
	}{
		{"write", func(src, dst string) error { return AtomicWriteFile(dst, []byte("replacement"), 0600) }},
		{"copy", func(src, dst string) error { return AtomicCopyFile(src, dst, 0600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(dir, "source")
			if err := os.WriteFile(source, []byte("replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(source, target); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "replacement" {
				t.Fatalf("published file = %q, %v", data, err)
			}
			if temps, err := filepath.Glob(filepath.Join(dir, ".tmp-*")); err != nil || len(temps) != 0 {
				t.Errorf("temporary files after success: %v, %v", temps, err)
			}
		})
	}
}

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
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0200 == 0 {
			t.Errorf("File is not writable on Windows: %o", info.Mode().Perm())
		}
	} else if info.Mode().Perm() != 0644 {
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
