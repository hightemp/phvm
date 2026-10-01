//go:build linux

package resources

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// AvailableMemory reports reclaimable host memory, bounded by a cgroup limit.
func AvailableMemory() (uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	available, err := parseMemAvailable(string(data))
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]string{
		{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory.current"},
		{"/sys/fs/cgroup/memory/memory.limit_in_bytes", "/sys/fs/cgroup/memory/memory.usage_in_bytes"},
	} {
		limitBytes, limitErr := os.ReadFile(pair[0])
		usageBytes, usageErr := os.ReadFile(pair[1])
		if limitErr != nil || usageErr != nil || strings.TrimSpace(string(limitBytes)) == "max" {
			continue
		}
		limit, limitErr := strconv.ParseUint(strings.TrimSpace(string(limitBytes)), 10, 64)
		usage, usageErr := strconv.ParseUint(strings.TrimSpace(string(usageBytes)), 10, 64)
		if limitErr != nil || usageErr != nil || limit == 0 {
			continue
		}
		remaining := uint64(0)
		if usage < limit {
			remaining = limit - usage
		}
		if remaining < available {
			available = remaining
		}
	}
	return available, nil
}

func parseMemAvailable(meminfo string) (uint64, error) {
	for _, line := range strings.Split(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "MemAvailable:" || fields[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > math.MaxUint64/1024 {
			return 0, fmt.Errorf("invalid MemAvailable value")
		}
		return value * 1024, nil
	}
	return 0, fmt.Errorf("MemAvailable is absent from /proc/meminfo")
}
