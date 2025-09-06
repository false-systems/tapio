//go:build !linux
// +build !linux

package containerruntime

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Observer) startEBPF() error {
	c.logger.Warn("eBPF CRI monitoring not available on this platform (Linux-only)")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Observer) stopEBPF() {
	// No resources to clean up
}

// processEvents - No events on non-Linux platforms
func (c *Observer) processEvents() {
	c.logger.Debug("eBPF event processing skipped (Linux-only feature)")
}

// cleanup - No cleanup on non-Linux platforms
func (c *Observer) cleanup() {
	// No resources to clean up
}

// loadEBPFPrograms - No-op on non-Linux platforms
func (c *Observer) loadEBPFPrograms() error {
	c.logger.Debug("eBPF program loading skipped (Linux-only feature)")
	return nil
}

// attachPrograms - No-op on non-Linux platforms
func (c *Observer) attachPrograms() error {
	c.logger.Debug("eBPF program attachment skipped (Linux-only feature)")
	return nil
}

// handleRingBufferEvent - No-op on non-Linux platforms
func (c *Observer) handleRingBufferEvent(data []byte) {
	// No events to handle
}

// collectMetrics - No-op on non-Linux platforms
func (c *Observer) collectMetrics() {
	c.logger.Debug("Metrics collection skipped (Linux-only feature)")
}
