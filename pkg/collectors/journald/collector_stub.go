//go:build !linux
// +build !linux

package journald

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/unified"
	"github.com/yairfalse/tapio/pkg/logging"
)

// StubCollector provides a no-op implementation of the journald collector for non-Linux platforms
type StubCollector struct {
	config    unified.CollectorConfig
	logger    *logging.Logger
	eventChan chan *unified.Event
	platform  string
}

// NewCollector creates a new stub journald collector for non-Linux platforms
func NewCollector(config unified.CollectorConfig) (*StubCollector, error) {
	logger := logging.Development.WithComponent("journald-collector-stub")

	collector := &StubCollector{
		config:    config,
		logger:    logger,
		eventChan: make(chan *unified.Event, config.EventBufferSize),
		platform:  runtime.GOOS,
	}

	logger.Info("Journald collector initialized in stub mode",
		"platform", collector.platform,
		"reason", "journald is only available on Linux")

	return collector, nil
}

// Name returns the collector name
func (c *StubCollector) Name() string {
	return "journald-stub"
}

// Type returns the collector type
func (c *StubCollector) Type() string {
	return "journald"
}

// Start starts the stub collector (no-op)
func (c *StubCollector) Start(ctx context.Context) error {
	c.logger.Info("Starting journald collector in stub mode")

	// Generate mock events for development
	go c.generateMockEvents(ctx)

	return nil
}

// Stop stops the stub collector
func (c *StubCollector) Stop() error {
	c.logger.Info("Stopping journald collector stub")
	close(c.eventChan)
	return nil
}

// Events returns the event channel
func (c *StubCollector) Events() <-chan *unified.Event {
	return c.eventChan
}

// Health returns the collector health status
func (c *StubCollector) Health() *unified.Health {
	return &unified.Health{
		Status:  unified.HealthStatusHealthy,
		Message: fmt.Sprintf("Journald collector running in stub mode on %s", c.platform),
		Metrics: map[string]interface{}{
			"platform":    c.platform,
			"mode":        "stub",
			"supported":   false,
			"mock_events": true,
		},
	}
}

// GetStats returns stub statistics
func (c *StubCollector) GetStats() *unified.Stats {
	return &unified.Stats{
		EventsCollected: 0,
		EventsDropped:   0,
		EventsFiltered:  0,
		ErrorCount:      0,
		StartTime:       time.Now(),
		LastEventTime:   time.Now(),
		Custom: map[string]interface{}{
			"platform":           c.platform,
			"mode":               "stub",
			"journald_available": false,
		},
	}
}

// Configure updates the collector configuration
func (c *StubCollector) Configure(config unified.CollectorConfig) error {
	c.config = config
	c.logger.Info("Journald collector configuration updated (stub mode)")
	return nil
}

// IsEnabled returns whether the collector is enabled
func (c *StubCollector) IsEnabled() bool {
	return c.config.Enabled
}

// generateMockEvents generates mock events for development purposes
func (c *StubCollector) generateMockEvents(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	eventCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			eventCount++

			// Generate a mock journald event
			event := &unified.Event{
				ID:        fmt.Sprintf("mock-journald-%d", eventCount),
				Type:      "log",
				Category:  unified.CategorySystem,
				Severity:  unified.SeverityInfo,
				Timestamp: time.Now(),
				Source: unified.EventSource{
					Collector: c.Name(),
					Component: "mock",
					Node:      "localhost",
					Version:   "1.0.0",
				},
				Message: fmt.Sprintf("Mock journald event #%d generated on %s", eventCount, c.platform),
				Data: map[string]interface{}{
					"message":  fmt.Sprintf("Mock journald event #%d generated on %s", eventCount, c.platform),
					"unit":     "mock.service",
					"priority": 6,
					"mock":     true,
				},
				Attributes: map[string]interface{}{
					"platform":     c.platform,
					"systemd_unit": "mock.service",
					"mock_event":   true,
				},
				Labels: map[string]string{
					"source":   "journald-stub",
					"platform": c.platform,
					"mode":     "development",
				},
				Context: &unified.EventContext{
					Node: "localhost",
				},
				Metadata: unified.EventMetadata{
					CollectedAt:  time.Now(),
					ProcessedAt:  time.Now(),
					ProcessingMS: 0,
					Tags:         c.config.Tags,
				},
			}

			// Occasionally generate a mock OOM event
			if eventCount%10 == 0 {
				event.Type = "oom"
				event.Category = unified.CategoryMemory
				event.Severity = unified.SeverityCritical
				event.Data["message"] = "Mock OOM kill event for development"
				event.Data["victim_name"] = "mock-process"
				event.Data["victim_pid"] = 12345
				event.Data["memory_usage"] = 1024 * 1024 * 100 // 100MB
			}

			select {
			case c.eventChan <- event:
			default:
				// Drop if channel is full
			}
		}
	}
}

// Platform-specific stubs for components that don't exist on non-Linux
func NewParser() *Parser {
	return &Parser{} // Empty struct for compatibility
}

func NewOOMDetector() *OOMDetector {
	return &OOMDetector{} // Empty struct for compatibility
}

func NewContainerEventParser() *ContainerEventParser {
	return &ContainerEventParser{} // Empty struct for compatibility
}

func NewSmartFilter(config *JournaldConfig) *SmartFilter {
	return &SmartFilter{} // Empty struct for compatibility
}

func NewSemanticEnricher() *SemanticEnricher {
	return &SemanticEnricher{} // Empty struct for compatibility
}

// Stub implementations for the components
type Parser struct{}
type OOMDetector struct{}
type ContainerEventParser struct{}
type SmartFilter struct{}
type SemanticEnricher struct{}

// No-op methods for compatibility
func (p *Parser) ParseCritical(entry *JournalEntry) *unified.Event           { return nil }
func (o *OOMDetector) Detect(entry *JournalEntry) *unified.Event             { return nil }
func (o *OOMDetector) Reset()                                                {}
func (c *ContainerEventParser) Parse(entry *JournalEntry) *unified.Event     { return nil }
func (s *SmartFilter) ShouldProcess(entry *JournalEntry) bool                { return false }
func (s *SmartFilter) GetStatistics() map[string]interface{}                 { return map[string]interface{}{} }
func (e *SemanticEnricher) Enrich(event *unified.Event, entry *JournalEntry) {}
