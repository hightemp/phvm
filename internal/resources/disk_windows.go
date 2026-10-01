//go:build windows

package resources

import "golang.org/x/sys/windows"

// FreeDiskSpace returns bytes available to the current user on path's volume.
func FreeDiskSpace(path string) (uint64, error) {
	path, err := existingDiskPath(path)
	if err != nil {
		return 0, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(name, &available, &total, &totalFree); err != nil {
		return 0, err
	}
	return available, nil
}
