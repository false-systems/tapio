//go:build !linux
// +build !linux

package dns

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// startEBPF is a no-op for non-Linux platforms
func (c *Observer) startEBPF() error {
	if c.config.EnableEBPF {
		c.logger.Warn("eBPF DNS monitoring not supported on this platform, running in mock mode")
		c.mockMode = true
		// Start mock DNS event generation for testing
		go c.generateMockEvents()
	}
	return nil
}

// stopEBPF is a no-op for non-Linux platforms
func (c *Observer) stopEBPF() {
	// Nothing to clean up on non-Linux platforms
	c.logger.Debug("DNS eBPF monitoring stopped (no-op for non-Linux)")
}

// readEBPFEvents generates mock events for non-Linux platforms
func (c *Observer) readEBPFEvents() {
	if c.mockMode {
		c.generateMockEvents()
	}
}

// generateMockEvents generates mock DNS events for testing on non-Linux platforms
func (c *Observer) generateMockEvents() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	mockQueries := []string{
		"google.com", "github.com", "stackoverflow.com",
		"kubernetes.io", "golang.org", "docker.com",
	}
	queryIndex := 0

	c.logger.Info("Starting mock DNS event generation for non-Linux testing")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			now := time.Now()
			queryName := mockQueries[queryIndex%len(mockQueries)]
			queryIndex++

			mockEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("dns-mock-%d", now.UnixNano()),
				Type:      domain.EventTypeDNS,
				Timestamp: now,
				Source:    c.config.Name,
				Severity:  domain.EventSeverityInfo,
				EventData: domain.EventDataContainer{
					DNS: &domain.DNSData{
						QueryName:    queryName,
						QueryType:    "A",
						ResponseCode: 0,
						Duration:     time.Duration(20+queryIndex%80) * time.Millisecond,
						ClientIP:     "127.0.0.1",
						ServerIP:     "8.8.8.8",
						ClientPort:   uint16(32000 + queryIndex%1000),
						ServerPort:   53,
						Error:        false,
					},
					Process: &domain.ProcessData{
						PID: int32(1000 + queryIndex%1000),
						TID: int32(1000 + queryIndex%1000),
						UID: 1000,
						GID: 1000,
					},
				},
				Metadata: domain.EventMetadata{
					Tags: []string{
						"protocol:UDP", "qtype:A", "rcode:0", "mock:true",
					},
					Labels: map[string]string{
						"observer":      c.config.Name,
						"query_name":    queryName,
						"query_type":    "A",
						"protocol":      "UDP",
						"mock_event":    "true",
						"response_code": "0",
						"platform":      "non_linux",
					},
				},
			}

			// Send mock event
			if c.EventChannelManager.SendEvent(mockEvent) {
				c.BaseObserver.RecordEvent()
			} else {
				c.BaseObserver.RecordDrop()
			}
			c.logger.Debug("Mock DNS event generated", zap.String("query", queryName))
		}
	}
}
