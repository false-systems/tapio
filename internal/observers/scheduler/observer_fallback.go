//go:build !linux
// +build !linux

package scheduler

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startEBPF is a no-op on non-Linux platforms
func (o *Observer) startEBPF() error {
	o.logger.Warn("Scheduler observer requires Linux with eBPF support, running in mock mode")
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (o *Observer) stopEBPF() {
	// Nothing to clean up
}

// processEvents generates mock events on non-Linux platforms
func (o *Observer) processEvents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventCount := 0
	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			eventCount++

			// Generate a mock scheduling delay event
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("mock-scheduler-%d", eventCount),
				Timestamp: time.Now(),
				Type:      domain.EventTypeScheduler,
				Source:    o.name,
				Severity:  domain.EventSeverityWarning,
				EventData: domain.EventDataContainer{
					Scheduler: &domain.SchedulerData{
						EventType:   "scheduling_delay",
						PID:         1234,
						TID:         1234,
						CPU:         0,
						Command:     "mock-process",
						DelayMs:     25.5,
						WaitRatio:   0.15,
						Priority:    120,
						Nice:        0,
						CgroupID:    1000,
						ContainerID: "mock-container-123",
					},
					Custom: map[string]string{
						"mock":     "true",
						"platform": "non-linux",
					},
				},
				Metadata: domain.EventMetadata{
					Priority: domain.PriorityNormal,
					Labels: map[string]string{
						"cpu":      "0",
						"delay_ms": "25.5",
					},
				},
			}

			if o.EventChannelManager.SendEvent(event) {
				o.BaseObserver.RecordEvent()
				o.logger.Debug("Sent mock scheduler event", zap.Int("count", eventCount))
			} else {
				o.BaseObserver.RecordDrop()
			}
		}
	}
}
