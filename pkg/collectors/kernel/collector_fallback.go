//go:build !linux
// +build !linux

package kernel

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF kernel monitoring not available on this platform (Linux-only)")
	c.logger.Info("Kernel collector running in fallback mode - limited events will be generated")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	c.logger.Debug("No eBPF resources to clean up (non-Linux platform)")
}

// processEvents - No eBPF events on non-Linux platforms
func (c *Collector) processEvents() {
	c.logger.Debug("eBPF event processing skipped (Linux-only feature)")
	
	// Keep the goroutine alive to maintain expected behavior
	// but don't actually process any eBPF events
	<-c.ctx.Done()
	c.logger.Debug("Event processing stopped (fallback mode)")
}