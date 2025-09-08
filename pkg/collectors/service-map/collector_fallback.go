//go:build !linux

package servicemap

import (
	"context"
	"time"
)

// Platform fallback for non-Linux systems (development only)
// Production deployment is Linux-only (Kubernetes)

// startEBPF - eBPF is Linux-only, logs warning on other platforms
func (c *Collector) startEBPF() error {
	c.logger.Warn("eBPF connection tracking not available on this platform (Linux-only)")
	c.logger.Info("Service map collector will work with Kubernetes service discovery only")

	// On non-Linux platforms, we can still do service discovery via K8s API
	// but won't have real-time connection tracking
	return nil
}

// stopEBPF - No-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	// No resources to clean up
}

// processEBPFEvents - No-op on non-Linux platforms
func (c *Collector) processEBPFEvents(ctx context.Context) {
	c.logger.Debug("eBPF event processing skipped on non-Linux platform")

	// Instead of eBPF events, we could potentially:
	// 1. Parse /proc/net/tcp and /proc/net/udp on Linux-like systems
	// 2. Use netstat output parsing
	// 3. Mock some connections for development

	// For now, just sleep to prevent busy loop
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.StopChannel():
			return
		case <-ticker.C:
			c.logger.Debug("Service map running in K8s-only mode (no eBPF)")
		}
	}
}
