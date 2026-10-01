//go:build linux || darwin

package resources

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// FreeDiskSpace returns bytes available to the current user on path's volume.
func FreeDiskSpace(path string) (uint64, error) {
	path, err := existingDiskPath(path)
	if err != nil {
		return 0, err
	}
	var info unix.Statfs_t
	if err := unix.Statfs(path, &info); err != nil {
		return 0, fmt.Errorf("stat filesystem %s: %w", path, err)
	}
	if info.Bsize <= 0 {
		return 0, fmt.Errorf("invalid filesystem block size")
	}
	return saturatingMultiplyAdd(info.Bavail, uint64(info.Bsize), 0), nil
}
