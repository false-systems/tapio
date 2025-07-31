//go:build !linux
// +build !linux

package cni

import "fmt"

// startEBPF is a no-op on non-Linux platforms
func (c *Collector) startEBPF() error {
	return fmt.Errorf("eBPF monitoring not supported on this platform")
}

// stopEBPF is a no-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op
}

// readEBPFEvents is not used on non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No-op
}
