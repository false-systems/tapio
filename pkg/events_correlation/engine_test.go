package events_correlation

import (
	"context"
	"testing"
	"time"
)

// MockEventStore is a simple in-memory event store for testing
type MockEventStore struct {
	events []Event
}

func (m *MockEventStore) GetEvents(ctx context.Context, filter Filter) ([]Event, error) {
	var result []Event
	for _, event := range m.events {
		if filter.Matches(event) {
			result = append(result, event)
		}
	}
	return result, nil
}

func (m *MockEventStore) GetEventsInWindow(ctx context.Context, window TimeWindow, filter Filter) ([]Event, error) {
	var result []Event
	for _, event := range m.events {
		if window.Contains(event.Timestamp) && filter.Matches(event) {
			result = append(result, event)
		}
	}
	return result, nil
}

func (m *MockEventStore) StoreEvent(ctx context.Context, event Event) error {
	m.events = append(m.events, event)
	return nil
}

func (m *MockEventStore) StoreBatch(ctx context.Context, events []Event) error {
	m.events = append(m.events, events...)
	return nil
}

func (m *MockEventStore) GetMetrics(ctx context.Context, name string, window TimeWindow) (MetricSeries, error) {
	return MetricSeries{Name: name}, nil
}

func (m *MockEventStore) Cleanup(ctx context.Context, before time.Time) error {
	return nil
}

func (m *MockEventStore) Stats(ctx context.Context) (EventStoreStats, error) {
	return EventStoreStats{}, nil
}

func TestCorrelationEngine(t *testing.T) {
	// Create mock event store
	eventStore := &MockEventStore{}

	// Create engine
	engine := NewEngine(eventStore)

	// Test rule registration
	testRule := &Rule{
		ID:              "test-rule",
		Name:            "Test Rule",
		MinConfidence:   0.5,
		RequiredSources: []EventSource{SourceEBPF},
		Evaluate: func(ctx *Context) *Result {
			events := ctx.GetEvents(Filter{Source: SourceEBPF})
			if len(events) > 0 {
				return &Result{
					Confidence:  0.8,
					Severity:    SeverityMedium,
					Title:       "Test correlation found",
					Description: "Test rule found matching events",
				}
			}
			return nil
		},
	}

	err := engine.RegisterRule(testRule)
	if err != nil {
		t.Fatalf("Failed to register rule: %v", err)
	}

	// Test rule retrieval
	retrievedRule, exists := engine.GetRule("test-rule")
	if !exists {
		t.Error("Expected to find registered rule")
	}
	if retrievedRule.ID != "test-rule" {
		t.Errorf("Expected rule ID 'test-rule', got '%s'", retrievedRule.ID)
	}

	// Test processing events
	events := []Event{
		{
			ID:        "event1",
			Timestamp: time.Now(),
			Source:    SourceEBPF,
			Type:      "test_event",
			Entity:    Entity{Name: "test-pod"},
		},
	}

	ctx := context.Background()
	results, err := engine.ProcessEvents(ctx, events)
	if err != nil {
		t.Fatalf("Failed to process events: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if results[0].Title != "Test correlation found" {
		t.Errorf("Expected 'Test correlation found', got '%s'", results[0].Title)
	}
}

func TestRuleValidation(t *testing.T) {
	eventStore := &MockEventStore{}
	engine := NewEngine(eventStore)

	// Test invalid rule - missing ID
	invalidRule := &Rule{
		Name: "Invalid Rule",
		Evaluate: func(ctx *Context) *Result {
			return nil
		},
	}

	err := engine.RegisterRule(invalidRule)
	if err == nil {
		t.Error("Expected error for rule without ID")
	}

	// Test invalid rule - missing evaluate function
	invalidRule2 := &Rule{
		ID:   "invalid-rule-2",
		Name: "Invalid Rule 2",
	}

	err = engine.RegisterRule(invalidRule2)
	if err == nil {
		t.Error("Expected error for rule without evaluate function")
	}
}

func TestEngineStats(t *testing.T) {
	eventStore := &MockEventStore{}
	engine := NewEngine(eventStore)

	stats := engine.GetStats()
	if stats.RulesRegistered != 0 {
		t.Errorf("Expected 0 rules registered, got %d", stats.RulesRegistered)
	}

	// Register a rule
	testRule := &Rule{
		ID:              "stats-test-rule",
		Name:            "Stats Test Rule",
		RequiredSources: []EventSource{SourceEBPF},
		Evaluate: func(ctx *Context) *Result {
			return nil
		},
	}

	engine.RegisterRule(testRule)

	stats = engine.GetStats()
	if stats.RulesRegistered != 1 {
		t.Errorf("Expected 1 rule registered, got %d", stats.RulesRegistered)
	}
}

func TestRuleEnableDisable(t *testing.T) {
	eventStore := &MockEventStore{}
	engine := NewEngine(eventStore)

	testRule := &Rule{
		ID:              "enable-disable-test",
		Name:            "Enable Disable Test",
		RequiredSources: []EventSource{SourceEBPF},
		Evaluate: func(ctx *Context) *Result {
			return nil
		},
	}

	engine.RegisterRule(testRule)

	// Test disable
	err := engine.DisableRule("enable-disable-test")
	if err != nil {
		t.Errorf("Failed to disable rule: %v", err)
	}

	rule, _ := engine.GetRule("enable-disable-test")
	if rule.Enabled {
		t.Error("Expected rule to be disabled")
	}

	// Test enable
	err = engine.EnableRule("enable-disable-test")
	if err != nil {
		t.Errorf("Failed to enable rule: %v", err)
	}

	rule, _ = engine.GetRule("enable-disable-test")
	if !rule.Enabled {
		t.Error("Expected rule to be enabled")
	}
}
