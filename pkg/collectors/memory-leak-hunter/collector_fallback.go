//go:build !linux
// +build !linux

package memory_leak_hunter

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF memory monitoring not available on this platform (Linux-only)")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	// No resources to clean up
}

// readEBPFEvents - No events on non-Linux platforms
func (c *Collector) readEBPFEvents() {
	c.logger.Debug("eBPF event reading skipped (Linux-only feature)")
}

// scanUnfreedAllocations - No scanning on non-Linux platforms
func (c *Collector) scanUnfreedAllocations() {
	c.logger.Debug("Allocation scanning skipped (Linux-only feature)")
}