//go:build !linux
// +build !linux

package storageio

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startEBPF is a no-op on non-Linux platforms
func (o *Observer) startEBPF() error {
	o.logger.Warn("Storage I/O observer requires Linux with eBPF support, running in mock mode")
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (o *Observer) stopEBPF() {
	// Nothing to clean up
}

// processEventsImpl generates mock events on non-Linux platforms
func (o *Observer) processEventsImpl() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventCount := 0
	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			eventCount++

			// Generate a mock slow I/O event
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("mock-storage-io-%d", eventCount),
				Timestamp: time.Now(),
				Type:      domain.EventTypeStorageIO,
				Source:    o.name,
				Severity:  domain.EventSeverityWarning,
				EventData: domain.EventDataContainer{
					StorageIO: &domain.StorageIOData{
						Operation: "write",
						Path:      "/var/lib/kubelet/pods/mock-pod/volumes/kubernetes.io~configmap/config",
						Duration:  250 * time.Millisecond,
						Size:      4096,
						SlowIO:    true,
						BlockedIO: false,
						Device:    "253:0",
						Inode:     123456,
					},
					Process: &domain.ProcessData{
						PID:     5678,
						TID:     5678,
						Command: "kubelet",
					},
					Custom: map[string]string{
						"mock":     "true",
						"platform": "non-linux",
					},
				},
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "storage-io",
						"version":  "1.0.0",
						"slow":     "true",
						"blocking": "false",
						"k8s":      "true",
					},
				},
			}

			if o.EventChannelManager.SendEvent(event) {
				o.BaseObserver.RecordEvent()
				o.logger.Debug("Sent mock storage I/O event", zap.Int("count", eventCount))
			} else {
				o.BaseObserver.RecordDrop()
			}
		}
	}
}
