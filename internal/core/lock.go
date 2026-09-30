package core

import (
	"context"
	"path/filepath"
	"sync/atomic"

	"github.com/hightemp/phvm/internal/fsutil"
)

type stateLockKey struct{}
type stateLease struct {
	root   string
	active atomic.Bool
}

// LockState coordinates all managed state outside the disposable cache. The
// returned context permits sequential nested operations in the same lease;
// callers must not share it with concurrent mutations.
func (p *Paths) LockState(ctx context.Context) (context.Context, func() error, error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	root, err := p.OpenDataDir(p.Root, true)
	if err != nil {
		return ctx, nil, err
	}
	defer root.Close()
	// Make the root absolute before resolving symlinks. With a relative PHVM_DIR,
	// a symlinked working-directory parent otherwise gives nested locks different keys.
	key, err := filepath.Abs(p.Root)
	if err != nil {
		return ctx, nil, err
	}
	key, err = filepath.EvalSymlinks(key)
	if err != nil {
		return ctx, nil, err
	}
	if lease, ok := ctx.Value(stateLockKey{}).(*stateLease); ok && lease.root == key && lease.active.Load() {
		return ctx, func() error { return nil }, nil
	}
	release, err := fsutil.AcquireRootLock(ctx, root, ".lock")
	if err != nil {
		return ctx, nil, err
	}
	if err := p.RecoverInstallTransactions(ctx); err != nil {
		_ = release()
		return ctx, nil, err
	}
	lease := &stateLease{root: key}
	lease.active.Store(true)
	return context.WithValue(ctx, stateLockKey{}, lease), func() error {
		if !lease.active.CompareAndSwap(true, false) {
			return nil
		}
		return release()
	}, nil
}

// WithStateLock runs one state operation and its sequential nested operations.
func (p *Paths) WithStateLock(ctx context.Context, fn func(context.Context) error) error {
	locked, release, err := p.LockState(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(locked)
}
