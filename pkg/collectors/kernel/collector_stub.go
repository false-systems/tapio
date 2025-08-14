//go:build !linux
// +build !linux

package kernel

import (
	"fmt"
	"runtime"
)

// startEBPF returns an error on non-Linux platforms
func (c *ModularCollector) startEBPF() error {
	return fmt.Errorf("eBPF not supported on %s", runtime.GOOS)
}

// stopEBPF is a no-op on non-Linux platforms
func (c *ModularCollector) stopEBPF() {
	// No-op
}

// readEBPFEvents is a no-op on non-Linux platforms
func (c *ModularCollector) readEBPFEvents() {
	// No-op
}