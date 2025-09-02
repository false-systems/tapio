//go:build !linux
// +build !linux

package helmcorrelator

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF Helm tracking not available on this platform (Linux-only)")
	c.logger.Info("Helm correlator running in K8s-only mode")

	// Still functional without eBPF - we can track via K8s secrets
	// Just won't have process-level visibility
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	c.logger.Debug("No eBPF resources to clean up (non-Linux platform)")
}

// readEBPFEvents - No eBPF events on non-Linux platforms
func (c *Collector) readEBPFEvents() {
	c.logger.Debug("eBPF event processing skipped (Linux-only feature)")

	// Keep the goroutine alive to maintain expected behavior
	ctx := c.LifecycleManager.Context()
	<-ctx.Done()
	c.logger.Debug("Event processing stopped (fallback mode)")
}
