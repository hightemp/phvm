//go:build linux

package resources

import "testing"

func TestParseMemAvailableUsesReclaimableMemory(t *testing.T) {
	value, err := parseMemAvailable("MemFree: 123 kB\nMemAvailable: 4096 kB\n")
	if err != nil || value != 4<<20 {
		t.Errorf("available memory=%d error=%v", value, err)
	}
	if _, err := parseMemAvailable("MemFree: 4096 kB\n"); err == nil {
		t.Error("missing MemAvailable was treated as plentiful memory")
	}
}
