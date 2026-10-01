//go:build darwin

package resources

import "testing"

func TestParseVMStatCountsOnlyReclaimablePages(t *testing.T) {
	output := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
		"Pages free: 100.\nPages inactive: 200.\nPages speculative: 50.\nPages wired down: 9999.\n"
	got, err := parseVMStat(output)
	if err != nil || got != 350*16384 {
		t.Errorf("available memory=%d error=%v", got, err)
	}
}
