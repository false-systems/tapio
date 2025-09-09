//go:build !linux
// +build !linux

package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// initializeEBPF is a no-op on non-Linux systems
func (o *Observer) initializeEBPF(ctx context.Context) error {
	o.logger.Info("eBPF connection tracking not available on this platform")

	// Start simulated connection discovery for testing
	o.LifecycleManager.Start("connection-simulator", func() {
		o.simulateConnections(ctx)
	})

	return nil
}

// cleanupEBPF is a no-op on non-Linux systems
func (o *Observer) cleanupEBPF() {
	// Nothing to cleanup on non-Linux
	o.logger.Debug("No eBPF resources to cleanup on this platform")
}

// simulateConnections creates fake connections for testing on non-Linux platforms
func (o *Observer) simulateConnections(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	o.logger.Info("Running connection simulation (eBPF not available)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.generateSimulatedConnections(ctx)
		}
	}
}

// generateSimulatedConnections creates test connections between services
func (o *Observer) generateSimulatedConnections(ctx context.Context) {
	o.mu.RLock()
	serviceList := make([]*Service, 0, len(o.services))
	for _, svc := range o.services {
		serviceList = append(serviceList, svc)
	}
	o.mu.RUnlock()

	// Create some sample connections
	for i := 0; i < len(serviceList)-1; i++ {
		src := serviceList[i]
		dst := serviceList[i+1]

		// Skip if no endpoints
		if len(src.Endpoints) == 0 || len(dst.Endpoints) == 0 {
			continue
		}

		// Create dependency
		o.mu.Lock()
		if src.Dependencies == nil {
			src.Dependencies = make(map[string]*Dependency)
		}

		depKey := fmt.Sprintf("%s/%s", dst.Namespace, dst.Name)
		if _, exists := src.Dependencies[depKey]; !exists {
			src.Dependencies[depKey] = &Dependency{
				Target:    depKey,
				CallRate:  10.0,
				ErrorRate: 0.01,
				Latency: LatencyStats{
					P50: 10.0,
					P95: 50.0,
					P99: 100.0,
					Max: 200.0,
				},
				Protocol:  "HTTP",
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
			}

			// Also update dependents
			if dst.Dependents == nil {
				dst.Dependents = make(map[string]*Dependent)
			}
			srcKey := fmt.Sprintf("%s/%s", src.Namespace, src.Name)
			dst.Dependents[srcKey] = &Dependent{
				Source:    srcKey,
				CallRate:  10.0,
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
			}

			o.logger.Debug("Simulated dependency",
				zap.String("source", src.Name),
				zap.String("target", dst.Name))
		}
		o.mu.Unlock()
	}
}
