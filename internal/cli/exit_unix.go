//go:build !windows

package cli

import (
	"os"
	"syscall"
)

func interruptionSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
func interruptionCode(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 143
	}
	return 130
}
