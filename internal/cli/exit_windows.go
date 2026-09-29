//go:build windows

package cli

import "os"

func interruptionSignals() []os.Signal { return []os.Signal{os.Interrupt} }
func interruptionCode(_ os.Signal) int { return 130 }
