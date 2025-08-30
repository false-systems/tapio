//go:build !linux
// +build !linux

package memory_leak_hunter

// startEBPF is a stub for non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF not supported on this platform")
	return nil
}

// stopEBPF is a stub for non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op on non-Linux
}

// readEBPFEvents is a stub for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	c.logger.Info("eBPF event reading not available on this platform")
}

// scanUnfreedAllocations is a stub for non-Linux platforms
func (c *Collector) scanUnfreedAllocations() {
	c.logger.Debug("Unfreed allocation scanning not available on this platform")
}
