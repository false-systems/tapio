//go:build !linux

package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// startEBPF is a no-op for non-Linux platforms
func (c *Collector) startEBPF() error {
	ctx := context.Background()

	if c.config.EnableEBPF {
		c.logger.Warn("eBPF DNS monitoring not supported on this platform, running in mock mode")

		// Start mock DNS event generation for testing
		go c.generateMockDNSEvents()

		// Record that eBPF is not supported
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_not_supported"),
				attribute.String("platform", "non_linux"),
			))
		}
	}

	return nil
}

// stopEBPF is a no-op for non-Linux platforms
func (c *Collector) stopEBPF() {
	// Nothing to clean up on non-Linux platforms
	c.logger.Debug("DNS eBPF monitoring stopped (no-op for non-Linux)")
}

// readEBPFEvents is a no-op for non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No eBPF events to read on non-Linux platforms
	c.logger.Debug("DNS eBPF event reading not available on non-Linux platforms")
}

// processDNSEvent processes mock DNS events for non-Linux platforms
func (c *Collector) processDNSEvent(data []byte) error {
	// On non-Linux platforms, this should not be called
	return fmt.Errorf("DNS event processing not supported on non-Linux platforms")
}

// generateMockDNSEvents generates mock DNS events for testing on non-Linux platforms
func (c *Collector) generateMockDNSEvents() {
	if !c.config.EnableEBPF {
		return
	}

	ctx := c.ctx
	if ctx == nil {
		// If collector hasn't been started yet, don't generate events
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	mockQueries := []string{
		"google.com",
		"github.com",
		"stackoverflow.com",
		"kubernetes.io",
		"golang.org",
		"docker.com",
	}

	queryIndex := 0

	c.logger.Info("Starting mock DNS event generation for non-Linux testing",
		zap.String("collector", c.name))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate a mock DNS event
			now := time.Now()
			queryName := mockQueries[queryIndex%len(mockQueries)]
			queryIndex++

			mockEvent := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("mock-dns-%d", now.UnixNano()),
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
						Answers:      []string{fmt.Sprintf("192.168.1.%d", queryIndex%254+1)},
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
						"protocol:UDP",
						"rcode:NOERROR",
						"qtype:A",
						"mock:true",
					},
					Attributes: map[string]string{
						"query_name":    queryName,
						"query_type":    "A",
						"response_code": "NOERROR",
						"protocol":      "UDP",
						"client_ip":     "127.0.0.1",
						"server_ip":     "8.8.8.8",
						"mock_event":    "true",
						"platform":      "non_linux",
						"description":   fmt.Sprintf("Mock DNS A query for %s (NOERROR)", queryName),
					},
					TraceID: fmt.Sprintf("mock-%d", queryIndex),
				},
			}

			// Add configured labels
			if c.config.Labels != nil {
				for k, v := range c.config.Labels {
					mockEvent.Metadata.Attributes[k] = v
				}
			}

			// Send mock event to channel
			select {
			case c.events <- mockEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
						attribute.String("query_type", "A"),
						attribute.String("protocol", "UDP"),
						attribute.String("response_code", "NOERROR"),
						attribute.Bool("mock_event", true),
					))
				}

				c.logger.Debug("Mock DNS event generated",
					zap.String("query", queryName),
					zap.String("event_id", mockEvent.EventID))

			case <-ctx.Done():
				return
			default:
				// Buffer full, drop event
				if c.droppedEvents != nil {
					c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
						attribute.String("reason", "buffer_full"),
						attribute.Bool("mock_event", true),
					))
				}
				c.logger.Warn("Mock DNS event channel full, dropping event")
			}
		}
	}
}

// processBPFEvent is not available on non-Linux platforms
func (c *Collector) processBPFEvent(ctx context.Context, data []byte) error {
	return fmt.Errorf("BPF event processing not supported on non-Linux platforms")
}
