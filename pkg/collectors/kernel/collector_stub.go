//go:build !linux
// +build !linux

package kernel

// startEBPF is a stub for non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF monitoring not supported on this platform")
	return nil
}

// stopEBPF is a stub for non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op on non-Linux platforms
}

// processEvents is a stub for non-Linux platforms
func (c *Collector) processEvents() {
	c.logger.Warn("eBPF event processing not supported on this platform")
	<-c.ctx.Done()
}

// readEBPFEvents is a stub for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	c.logger.Debug("eBPF event reading not supported on this platform")
	<-c.ctx.Done()
}
