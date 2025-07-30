//go:build !linux
// +build !linux

package cni

// initEBPF is a no-op on non-Linux systems
func (c *Collector) initEBPF() error {
	// eBPF is only available on Linux
	return nil
}

// readEBPFEvents is a no-op on non-Linux systems
func (c *Collector) readEBPFEvents() {
	// No-op
}

// cleanupEBPF is a no-op on non-Linux systems
func (c *Collector) cleanupEBPF() {
	// No-op
}
