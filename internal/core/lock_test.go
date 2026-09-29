package core

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestStateLockCancellationNestingAndLeaseLifetime(t *testing.T) {
	p := NewPaths(t.TempDir())
	if err := p.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	ctx, release, err := p.LockState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WithStateLock(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := p.WithStateLock(waitCtx, func(context.Context) error { t.Error("waiter entered holder's state"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error=%v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// Reusing a released context must acquire a fresh lock, not silently bypass it.
	_, otherRelease, err := p.LockState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer otherRelease()
	stale, cancelStale := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelStale()
	if err := p.WithStateLock(stale, func(context.Context) error { t.Error("stale lease bypassed new holder"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := otherRelease(); err != nil {
		t.Fatal(err)
	}
	if err := p.WithStateLock(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LockFile()); err != nil {
		t.Error("persistent lock deleted")
	}
}
