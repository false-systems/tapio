//go:build !linux

package cni

// Stub implementations for non-Linux systems (macOS with Colima, etc)

func (c *Collector) initEBPF() error {
	c.ebpfEnabled = false
	return nil
}

func (c *Collector) readEBPFEvents() {
	// No-op on non-Linux
	c.wg.Done()
}

func (c *Collector) cleanupEBPF() {
	// No-op on non-Linux
}