//go:build !linux
// +build !linux

package journald

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/types"
	"github.com/yairfalse/tapio/pkg/logging"
)

// StubCollector provides a no-op implementation of the journald collector for non-Linux platforms
type StubCollector struct {
	config    types.CollectorConfig
	logger    *logging.Logger
	eventChan chan *types.Event
	platform  string
}

// NewCollector creates a new stub journald collector for non-Linux platforms
func NewCollector(config types.CollectorConfig) (*StubCollector, error) {
	logger := logging.WithComponent("journald-collector-stub")
	
	collector := &StubCollector{
		config:    config,
		logger:    logger,
		eventChan: make(chan *types.Event, config.EventBufferSize),
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
func (c *StubCollector) Events() <-chan *types.Event {
	return c.eventChan
}

// Health returns the collector health status
func (c *StubCollector) Health() *types.Health {
	return &types.Health{
		Status:  types.HealthStatusHealthy,
		Message: fmt.Sprintf("Journald collector running in stub mode on %s", c.platform),
		Metrics: map[string]interface{}{
			"platform":   c.platform,
			"mode":       "stub",
			"supported":  false,
			"mock_events": true,
		},
	}
}

// GetStats returns stub statistics
func (c *StubCollector) GetStats() *types.Stats {
	return &types.Stats{
		EventsCollected: 0,
		EventsDropped:   0,
		EventsFiltered:  0,
		ErrorCount:      0,
		StartTime:       time.Now(),
		LastEventTime:   time.Now(),
		Custom: map[string]interface{}{
			"platform":      c.platform,
			"mode":          "stub",
			"journald_available": false,
		},
	}
}

// Configure updates the collector configuration
func (c *StubCollector) Configure(config types.CollectorConfig) error {
	c.config = config
	c.logger.Info("Journald collector configuration updated (stub mode)")
	return nil
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
			event := &types.Event{
				ID:        fmt.Sprintf("mock-journald-%d", eventCount),
				Type:      types.EventTypeLog,
				Category:  types.CategorySystem,
				Severity:  types.SeverityInfo,
				Timestamp: time.Now(),
				Source: types.EventSource{
					Collector: c.Name(),
					Component: "mock",
					Node:      "localhost",
				},
				Data: map[string]interface{}{
					"message": fmt.Sprintf("Mock journald event #%d generated on %s", eventCount, c.platform),
					"unit":    "mock.service",
					"priority": 6,
					"mock":    true,
				},
				Attributes: map[string]interface{}{
					"platform":    c.platform,
					"systemd_unit": "mock.service",
					"mock_event":  true,
				},
				Labels: map[string]string{
					"source":   "journald-stub",
					"platform": c.platform,
					"mode":     "development",
				},
				Context: &types.EventContext{
					Hostname: "localhost",
				},
			}
			
			// Occasionally generate a mock OOM event
			if eventCount%10 == 0 {
				event.Type = types.EventTypeOOM
				event.Category = types.CategoryCapacity
				event.Severity = types.SeverityCritical
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
func (p *Parser) ParseCritical(entry *JournalEntry) *types.Event { return nil }
func (o *OOMDetector) Detect(entry *JournalEntry) *types.Event { return nil }
func (o *OOMDetector) Reset() {}
func (c *ContainerEventParser) Parse(entry *JournalEntry) *types.Event { return nil }
func (s *SmartFilter) ShouldProcess(entry *JournalEntry) bool { return false }
func (s *SmartFilter) GetStatistics() map[string]interface{} { return map[string]interface{}{} }
func (e *SemanticEnricher) Enrich(event *types.Event, entry *JournalEntry) {}