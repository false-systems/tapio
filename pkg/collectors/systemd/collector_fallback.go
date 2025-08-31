//go:build !linux
// +build !linux

package systemd

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startMonitoring - eBPF systemd monitoring is Linux-only, logs warning on other platforms
func (c *Collector) startMonitoring() error {
	c.logger.Warn("eBPF systemd monitoring not available on this platform (Linux-only)")
	c.logger.Info("Systemd collector running in fallback mode - no events will be generated")
	
	// Set collector as healthy even though it won't produce events
	// This allows development and testing of other components on non-Linux systems
	c.healthy = true
	
	return nil
}

// stopMonitoring - No-op on non-Linux platforms
func (c *Collector) stopMonitoring() {
	c.logger.Debug("No eBPF resources to clean up (non-Linux platform)")
}

// processEvents - No events on non-Linux platforms
func (c *Collector) processEvents() {
	c.logger.Debug("eBPF event processing skipped (Linux-only feature)")
	
	// Keep the goroutine alive to maintain expected behavior
	// but don't actually process any events
	<-c.ctx.Done()
	c.logger.Debug("Event processing stopped (fallback mode)")
}