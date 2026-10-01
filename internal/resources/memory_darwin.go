//go:build darwin

package resources

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var pageSizePattern = regexp.MustCompile(`page size of ([0-9]+) bytes`)

// AvailableMemory estimates reclaimable physical pages from vm_stat.
func AvailableMemory() (uint64, error) {
	output, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, err
	}
	return parseVMStat(string(output))
}

func parseVMStat(output string) (uint64, error) {
	match := pageSizePattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0, fmt.Errorf("vm_stat page size unavailable")
	}
	pageSize, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil || pageSize == 0 {
		return 0, fmt.Errorf("invalid vm_stat page size")
	}
	var pages uint64
	for _, line := range strings.Split(output, "\n") {
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name != "Pages free" && name != "Pages inactive" && name != "Pages speculative" {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "."))
		count, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid vm_stat page count: %w", err)
		}
		pages = saturatingMultiplyAdd(count, 1, pages)
	}
	if pages == 0 {
		return 0, fmt.Errorf("vm_stat free/inactive pages unavailable")
	}
	return saturatingMultiplyAdd(pages, pageSize, 0), nil
}
