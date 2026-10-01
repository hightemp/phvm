//go:build windows

package resources

// AvailableMemory returns unknown when the Windows memory probe is unavailable.
// ResolveJobs then uses one safe automatic job.
func AvailableMemory() (uint64, error) { return 0, nil }
