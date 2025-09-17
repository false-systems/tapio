//go:build !linux
// +build !linux

package dns

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Observer) startEBPF() error {
	c.logger.Warn("eBPF DNS monitoring not available on this platform (Linux-only)")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Observer) stopEBPF() {
	// No resources to clean up
}

// readEBPFEvents - No events on non-Linux platforms
func (c *Observer) readEBPFEvents() {
	c.logger.Debug("eBPF event reading skipped (Linux-only feature)")
}
