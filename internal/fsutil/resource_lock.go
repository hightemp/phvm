package fsutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// AcquireRootLock holds a persistent regular lock file until release. The file
// must never be unlinked during normal operation, even after releasing it.
func AcquireRootLock(ctx context.Context, root *os.Root, name string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateName(strings.TrimPrefix(name, ".")); err != nil {
		return nil, err
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("lock must be a regular file: %s", name)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		locked, err := tryResourceLock(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if locked {
			var released atomic.Bool
			return func() error {
				if !released.CompareAndSwap(false, true) {
					return nil
				}
				return errors.Join(unlockResource(f), f.Close())
			}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// WithDirectoryLock serializes cache writers and cleanup. Lock files live in a
// sibling directory so clearing the cache cannot replace their locked inode.
func WithDirectoryLock(ctx context.Context, dir string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	dir = filepath.Join(parent, filepath.Base(dir))
	if canonical, err := filepath.EvalSymlinks(dir); err == nil {
		dir = canonical
		parent = filepath.Dir(dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	lockDir := filepath.Join(parent, ".phvm-locks")
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer parentRoot.Close()
	if info, err := parentRoot.Lstat(".phvm-locks"); err == nil && !info.IsDir() {
		return fmt.Errorf("lock directory must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := parentRoot.MkdirAll(".phvm-locks", 0700); err != nil {
		return err
	}
	root, err := parentRoot.OpenRoot(filepath.Base(lockDir))
	if err != nil {
		return err
	}
	defer root.Close()
	hash := sha256.Sum256([]byte(dir))
	release, err := AcquireRootLock(ctx, root, hex.EncodeToString(hash[:])+".lock")
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
