//go:build !linux
// +build !linux

package systemd

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// startEBPFMonitoring is a no-op on non-Linux systems
func (o *Observer) startEBPFMonitoring(ctx context.Context) error {
	o.logger.Info("eBPF monitoring not available on this platform")

	// Start simulation for testing purposes
	o.LifecycleManager.Start("simulation", func() {
		o.simulateSystemdEvents(o.LifecycleManager.Context())
	})

	return nil
}

// stopEBPFMonitoring is a no-op on non-Linux systems
func (o *Observer) stopEBPFMonitoring() {
	o.logger.Debug("No eBPF resources to cleanup on this platform")
}

// startJournalMonitoring is a no-op on non-Linux systems
func (o *Observer) startJournalMonitoring(ctx context.Context) error {
	o.logger.Info("Journal monitoring not available on this platform")
	return nil
}

// stopJournalMonitoring is a no-op on non-Linux systems
func (o *Observer) stopJournalMonitoring() {
	o.logger.Debug("No journal resources to cleanup on this platform")
}

// simulateSystemdEvents creates fake events for testing on non-Linux platforms
func (o *Observer) simulateSystemdEvents(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	o.logger.Info("Running systemd simulation (eBPF not available)")

	services := []string{
		"docker.service",
		"nginx.service",
		"postgresql.service",
		"redis.service",
		"ssh.service",
	}

	eventTypes := []uint8{
		EventTypeServiceStart,
		EventTypeServiceStop,
		EventTypeServiceRestart,
		EventTypeServiceFailed,
	}

	eventCounter := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate a simulated event
			serviceIdx := eventCounter % len(services)
			eventTypeIdx := (eventCounter / len(services)) % len(eventTypes)

			event := &SystemdEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       uint32(1000 + eventCounter),
				PPID:      1,
				UID:       0,
				GID:       0,
				CgroupID:  uint64(serviceIdx + 1),
				EventType: eventTypes[eventTypeIdx],
			}

			// Copy service name
			serviceName := services[serviceIdx]
			copy(event.ServiceName[:], serviceName)
			copy(event.Comm[:], "systemd")
			copy(event.CgroupPath[:], "/system.slice/"+serviceName)

			// Set exit code for failure events
			if event.EventType == EventTypeServiceFailed {
				event.ExitCode = 1
			}

			o.processSystemdEvent(ctx, event)

			o.logger.Debug("Simulated systemd event",
				zap.String("service", serviceName),
				zap.String("event_type", getEventTypeName(event.EventType)),
			)

			eventCounter++
		}
	}
}
