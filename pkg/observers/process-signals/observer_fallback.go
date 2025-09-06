//go:build !linux
// +build !linux

package processsignals

import (
	"context"
	"time"
)

// Stub implementation for non-Linux systems

// initializeEBPF is a no-op on non-Linux systems
func (o *Observer) initializeEBPF(ctx context.Context) error {
	o.logger.Info("eBPF not supported on this platform, running in limited mode")
	return nil
}

// cleanupEBPF is a no-op on non-Linux systems
func (o *Observer) cleanupEBPF() {
	// Nothing to cleanup
}

// processEvents runs in fallback mode without eBPF
func (o *Observer) processEvents(ctx context.Context) {
	o.logger.Info("Running in fallback mode without eBPF support")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			// In fallback mode, we can still provide some value through other means
			// For example, reading /proc, using audit logs, etc.
			o.logger.Debug("Heartbeat from fallback runtime observer")
		}
	}
}

// Stub types for non-Linux builds
type runtimeMonitorObjects struct{}

func loadRuntimeMonitorObjects(obj interface{}, opts interface{}) error {
	return nil
}
