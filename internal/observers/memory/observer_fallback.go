//go:build !linux
// +build !linux

package memory

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (o *Observer) startEBPF() error {
	o.logger.Warn("eBPF memory monitoring not available on this platform (Linux-only)")
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (o *Observer) stopEBPF() {
	// No resources to clean up
}

// readEBPFEvents - No events on non-Linux platforms
func (o *Observer) readEBPFEvents() {
	o.logger.Debug("eBPF event reading skipped (Linux-only feature)")
}

// scanUnfreedAllocations - No scanning on non-Linux platforms
func (o *Observer) scanUnfreedAllocations() {
	o.logger.Debug("Allocation scanning skipped (Linux-only feature)")
}
