//go:build !linux
// +build !linux

package dns

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// startEBPF is a no-op for non-Linux platforms
func (c *Collector) startEBPF() error {
	if c.config.EnableEBPF {
		c.logger.Warn("eBPF DNS monitoring not supported on this platform, running in mock mode")
		c.mockMode = true
		// Start mock DNS event generation for testing
		go c.generateMockEvents()
	}
	return nil
}

// stopEBPF is a no-op for non-Linux platforms
func (c *Collector) stopEBPF() {
	// Nothing to clean up on non-Linux platforms
	c.logger.Debug("DNS eBPF monitoring stopped (no-op for non-Linux)")
}

// readEBPFEvents generates mock events for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	if c.mockMode {
		c.generateMockEvents()
	}
}

// generateMockEvents generates mock DNS events for testing on non-Linux platforms
func (c *Collector) generateMockEvents() {
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
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			queryName := mockQueries[queryIndex%len(mockQueries)]
			queryIndex++

			mockEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("dns-mock-%d", now.UnixNano()),
				Type:      domain.EventTypeDNS,
				Timestamp: now,
				Source:    c.name,
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
						"collector":     c.name,
						"query_name":    queryName,
						"query_type":    "A",
						"protocol":      "UDP",
						"mock_event":    "true",
						"response_code": "0",
						"platform":      "non_linux",
					},
				},
			}

			// Send mock event to channel
			select {
			case c.events <- mockEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("query_type", "A"),
						attribute.String("protocol", "UDP"),
						attribute.Bool("mock_event", true),
					))
				}
				c.logger.Debug("Mock DNS event generated")
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop event
				if c.droppedEvents != nil {
					c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("reason", "buffer_full"),
						attribute.Bool("mock_event", true),
					))
				}
			}
		}
	}
}