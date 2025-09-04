//go:build !linux
// +build !linux

package syscallerrors

import "context"

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF syscall error monitoring not available on this platform (Linux-only)")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	// No resources to clean up
}

// readEvents - No events on non-Linux platforms
func (c *Collector) readEvents() {
	c.logger.Debug("eBPF event reading skipped (Linux-only feature)")
	// Keep goroutine alive but idle
	<-c.LifecycleManager.Context().Done()
}

// processRawEvent - No-op on non-Linux platforms
func (c *Collector) processRawEvent(data []byte) error {
	c.logger.Debug("Event processing skipped (Linux-only feature)")
	return nil
}

// isCategoryEnabled - Always returns true on non-Linux
func (c *Collector) isCategoryEnabled(category uint8) bool {
	return true
}

// getStatsImpl - Returns empty stats on non-Linux platforms
func (c *Collector) getStatsImpl() (*CollectorStats, error) {
	return &CollectorStats{
		TotalErrors:       0,
		ENOSPCCount:       0,
		ENOMEMCount:       0,
		ECONNREFUSEDCount: 0,
		EIOCount:          0,
		EventsSent:        0,
		EventsDropped:     0,
	}, nil
}

// updateErrorMetrics - No-op on non-Linux platforms
func (c *Collector) updateErrorMetrics(ctx context.Context, errorCode int32) {
	// No metrics to update
}
