//go:build !linux
// +build !linux

package kernel

// startEBPF stub for non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF not supported on this platform")
	return nil
}

// stopEBPF stub for non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op on non-Linux platforms
}

// readEBPFEvents stub for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No-op on non-Linux platforms
	<-c.ctx.Done()
}
