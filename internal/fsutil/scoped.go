package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OpenScopedDir opens a directory through an os.Root anchored at base.
// Existing symlinks cannot redirect operations outside the configured base.
func OpenScopedDir(base, path string, create bool) (*os.Root, error) {
	base, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("path is outside managed root")
	}
	if create {
		if err := os.MkdirAll(base, 0755); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	prefix := ""
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		prefix = filepath.Join(prefix, part)
		info, err := root.Lstat(prefix)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("managed directory cannot be a symlink: %s", prefix)
		}
	}
	if create {
		if err := root.MkdirAll(rel, 0755); err != nil {
			return nil, err
		}
	}
	return root.OpenRoot(rel)
}

// ValidateName validates a portable single path component.
func ValidateName(name string) error {
	if name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, "..") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("invalid name %q", name)
	}
	for _, ch := range name {
		allowed := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.'
		if !allowed {
			return fmt.Errorf("invalid name %q", name)
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
		return fmt.Errorf("reserved name %q", name)
	}
	return nil
}

// AtomicWriteRoot publishes a file using operations confined to root.
func AtomicWriteRoot(root *os.Root, name string, data []byte, perm os.FileMode) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("invalid relative path")
	}
	if err := root.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), ".tmp-"+RandomSuffix())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = root.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, name)
}

// CopyRootTree copies regular files and directories between confined roots.
// Symlinks are rejected instead of following arbitrary configuration targets.
func CopyRootTree(src, dst *os.Root) error {
	f, err := src.Open(".")
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("configuration symlink is not allowed: %s", entry.Name())
		}
		if entry.IsDir() {
			if err := dst.Mkdir(entry.Name(), 0755); err != nil && !os.IsExist(err) {
				return err
			}
			s, err := src.OpenRoot(entry.Name())
			if err != nil {
				return err
			}
			d, err := dst.OpenRoot(entry.Name())
			if err != nil {
				_ = s.Close()
				return err
			}
			err = CopyRootTree(s, d)
			_ = s.Close()
			_ = d.Close()
			if err != nil {
				return err
			}
			continue
		}
		in, err := src.Open(entry.Name())
		if err != nil {
			return err
		}
		info, err := in.Stat()
		if err != nil {
			_ = in.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			_ = in.Close()
			return fmt.Errorf("configuration file is not regular: %s", entry.Name())
		}
		data, err := io.ReadAll(in)
		_ = in.Close()
		if err != nil {
			return err
		}
		if err := AtomicWriteRoot(dst, entry.Name(), data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}
