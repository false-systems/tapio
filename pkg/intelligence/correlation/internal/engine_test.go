package internal
import (
	"context"
	"testing"
	"time"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation/core"
)
func TestNewCorrelationEngine(t *testing.T) {
	config := core.EngineConfig{
		Enabled:              true,
		EventBufferSize:      1000,
		OutputBufferSize:     100,
		DefaultTimeWindow:    5 * time.Minute,
		MinConfidenceScore:   0.7,
		MaxConcurrentEvents:  50,
		ProcessingTimeout:    30 * time.Second,
		CleanupInterval:      1 * time.Hour,
		EventRetentionTime:   24 * time.Hour,
		AlgorithmWeights:     make(map[string]float64),
	}
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create correlation engine: %v", err)
	}
	if engine == nil {
		t.Fatal("Engine should not be nil")
	}
	// Test invalid config
	invalidConfig := core.EngineConfig{}
	_, err = NewCorrelationEngine(invalidConfig)
	if err == nil {
		t.Error("Expected error for invalid config")
	}
}
func TestCorrelationEngine_StartStop(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	ctx := context.Background()
	// Test start
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	// Test double start
	err = engine.Start(ctx)
	if err == nil {
		t.Error("Expected error for double start")
	}
	// Test stop
	err = engine.Stop()
	if err != nil {
		t.Fatalf("Failed to stop engine: %v", err)
	}
	// Test double stop
	err = engine.Stop()
	if err != nil {
		t.Error("Should not error on double stop")
	}
}
func TestCorrelationEngine_ProcessEvent(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop()
	// Create test event
	event := createTestEvent()
	// Test process event
	err = engine.ProcessEvent(ctx, event)
	if err != nil {
		t.Errorf("Failed to process event: %v", err)
	}
	// Test invalid event
	invalidEvent := domain.Event{} // Missing required fields
	err = engine.ProcessEvent(ctx, invalidEvent)
	if err == nil {
		t.Error("Expected error for invalid event")
	}
}
func TestCorrelationEngine_ProcessEvents(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop()
	// Create test events
	events := []domain.Event{
		createTestEvent(),
		createTestMemoryEvent(),
		createTestNetworkEvent(),
	}
	// Test process events
	correlations, err := engine.ProcessEvents(ctx, events)
	if err != nil {
		t.Errorf("Failed to process events: %v", err)
	}
	// Should return empty correlations for this simple test
	if correlations == nil {
		t.Error("Correlations should not be nil")
	}
}
func TestCorrelationEngine_RegisterPattern(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	// Test register pattern
	pattern := &testPattern{id: "test_pattern"}
	err = engine.RegisterPattern(pattern)
	if err != nil {
		t.Errorf("Failed to register pattern: %v", err)
	}
	// Test register duplicate pattern
	err = engine.RegisterPattern(pattern)
	if err == nil {
		t.Error("Expected error for duplicate pattern")
	}
	// Test register nil pattern
	err = engine.RegisterPattern(nil)
	if err == nil {
		t.Error("Expected error for nil pattern")
	}
}
func TestCorrelationEngine_Health(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	// Test health before start
	health := engine.Health()
	if health.Status != core.HealthStatusUnknown {
		t.Errorf("Expected unknown status, got %v", health.Status)
	}
	// Start engine and test health
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop()
	health = engine.Health()
	if health.Status != core.HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %v", health.Status)
	}
}
func TestCorrelationEngine_Statistics(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	stats := engine.Statistics()
	if stats.StartTime.IsZero() {
		t.Error("Start time should be set")
	}
	if stats.PatternStatistics == nil {
		t.Error("Pattern statistics should not be nil")
	}
	if stats.AlgorithmMetrics == nil {
		t.Error("Algorithm metrics should not be nil")
	}
}
func TestCorrelationEngine_AnalyzeTimeWindow(t *testing.T) {
	config := createTestConfig()
	engine, err := NewCorrelationEngine(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	defer engine.Stop()
	// Add some events to the buffer
	events := []domain.Event{
		createTestEvent(),
		createTestMemoryEvent(),
	}
	for _, event := range events {
		_ = engine.ProcessEvent(ctx, event)
	}
	// Test analyze time window
	start := time.Now().Add(-10 * time.Minute)
	end := time.Now()
	correlations, err := engine.AnalyzeTimeWindow(ctx, start, end)
	if err != nil {
		t.Errorf("Failed to analyze time window: %v", err)
	}
	if correlations == nil {
		t.Error("Correlations should not be nil")
	}
	// Test invalid time range
	_, err = engine.AnalyzeTimeWindow(ctx, end, start)
	if err == nil {
		t.Error("Expected error for invalid time range")
	}
}
// Helper functions
func createTestConfig() core.EngineConfig {
	return core.EngineConfig{
		Enabled:              true,
		EventBufferSize:      1000,
		OutputBufferSize:     100,
		DefaultTimeWindow:    5 * time.Minute,
		MinConfidenceScore:   0.7,
		MaxConcurrentEvents:  50,
		ProcessingTimeout:    30 * time.Second,
		CleanupInterval:      1 * time.Hour,
		EventRetentionTime:   24 * time.Hour,
		AlgorithmWeights:     make(map[string]float64),
	}
}
func createTestEvent() domain.Event {
	return domain.Event{
		ID:          "test-event-1",
		Source:      domain.SourceEBPF,
		Type:        domain.EventTypeService,
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    domain.SeverityInfo,
		Description: "Test service event",
		Context: domain.EventContext{
			Host:   "test-host",
			Labels: map[string]string{"service": "test-service"},
			Tags:   []string{"test"},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "test-service",
			State:       "running",
			PID:         1234,
		},
		Metadata: domain.EventMetadata{
			Version:     "1.0",
			Annotations: map[string]string{"test": "true"},
		},
	}
}
func createTestMemoryEvent() domain.Event {
	return domain.Event{
		ID:          "test-memory-1",
		Source:      domain.SourceEBPF,
		Type:        domain.EventTypeMemory,
		Timestamp:   time.Now(),
		Confidence:  0.9,
		Severity:    domain.SeverityWarn,
		Description: "High memory usage detected",
		Context: domain.EventContext{
			Host:   "test-host",
			Labels: map[string]string{"process": "test-process"},
			Tags:   []string{"memory", "resource"},
		},
		Payload: domain.MemoryEventPayload{
			Usage:     85.5,
			Available: 1024 * 1024 * 1024, // 1GB
			Total:     8 * 1024 * 1024 * 1024, // 8GB
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func createTestNetworkEvent() domain.Event {
	return domain.Event{
		ID:          "test-network-1",
		Source:      domain.SourceEBPF,
		Type:        domain.EventTypeNetwork,
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    domain.SeverityError,
		Description: "Network connection failure",
		Context: domain.EventContext{
			Host:   "test-host",
			Labels: map[string]string{"protocol": "tcp"},
			Tags:   []string{"network", "failure"},
		},
		Payload: domain.NetworkEventPayload{
			Protocol:          "tcp",
			SourceIP:          "10.0.0.1",
			DestinationIP:     "10.0.0.2",
			SourcePort:        8080,
			DestinationPort:   3306,
			BytesSent:         1024,
			BytesReceived:     0,
			PacketsDropped:    5,
			ConnectionsFailed: 3,
			Errors:            2,
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
// Test pattern implementation
type testPattern struct {
	id string
}
func (p *testPattern) ID() string                                                                                { return p.id }
func (p *testPattern) Name() string                                                                             { return "Test Pattern" }
func (p *testPattern) Description() string                                                                      { return "Test pattern for unit tests" }
func (p *testPattern) Category() core.PatternCategory                                                           { return core.PatternCategoryGeneral }
func (p *testPattern) Priority() core.PatternPriority                                                           { return core.PatternPriorityLow }
func (p *testPattern) TimeWindow() time.Duration                                                                { return 5 * time.Minute }
func (p *testPattern) MinConfidence() float64                                                                   { return 0.5 }
func (p *testPattern) MaxEvents() int                                                                           { return 10 }
func (p *testPattern) RequiredSources() []domain.Source                                                         { return []domain.Source{domain.SourceEBPF} }
func (p *testPattern) Tags() []string                                                                           { return []string{"test"} }
func (p *testPattern) Match(ctx context.Context, events []domain.Event) ([]domain.Correlation, error)         { return nil, nil }
func (p *testPattern) CanMatch(event domain.Event) bool                                                         { return true }
func (p *testPattern) Configure(config map[string]interface{}) error                                            { return nil }
func (p *testPattern) Statistics() core.PatternStatistics                                                       { return core.PatternStatistics{} }
func (p *testPattern) Reset() error                                                                             { return nil }