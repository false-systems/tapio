//go:build !linux
// +build !linux

package systemd

// startEBPF stub for non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF not supported on this platform")
	return nil
}

// stopEBPF stub for non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op on non-Linux platforms
}

// processEvents stub for non-Linux platforms
func (c *Collector) processEvents() {
	// No-op on non-Linux platforms
	<-c.ctx.Done()
}
