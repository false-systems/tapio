package correlation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewCircularBuffer(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		wantErr  bool
	}{
		{
			name:     "valid capacity",
			capacity: 100,
			wantErr:  false,
		},
		{
			name:     "zero capacity",
			capacity: 0,
			wantErr:  true,
		},
		{
			name:     "negative capacity",
			capacity: -1,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := NewCircularBuffer(tt.capacity)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, buf)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, buf)
				assert.Equal(t, tt.capacity, buf.capacity)
				assert.Equal(t, 0, buf.size)
			}
		})
	}
}

func TestCircularBuffer_Operations(t *testing.T) {
	buf, err := NewCircularBuffer(3)
	require.NoError(t, err)

	// Test Add
	event1 := &domain.UnifiedEvent{
		ID:        "event1",
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
	}
	buf.Add(event1)
	assert.Equal(t, 1, buf.Size())

	// Test Get
	events := buf.Get(10)
	assert.Len(t, events, 1)
	assert.Equal(t, event1.ID, events[0].Event.ID)

	// Test overflow
	event2 := &domain.UnifiedEvent{ID: "event2", Timestamp: time.Now()}
	event3 := &domain.UnifiedEvent{ID: "event3", Timestamp: time.Now()}
	event4 := &domain.UnifiedEvent{ID: "event4", Timestamp: time.Now()}

	buf.Add(event2)
	buf.Add(event3)
	buf.Add(event4) // This should overwrite event1

	events = buf.Get(10)
	assert.Len(t, events, 3)
	assert.Equal(t, "event2", events[0].Event.ID)
	assert.Equal(t, "event3", events[1].Event.ID)
	assert.Equal(t, "event4", events[2].Event.ID)

	// Test GetByTimeWindow
	now := time.Now()
	oldEvent := &domain.UnifiedEvent{
		ID:        "old",
		Timestamp: now.Add(-10 * time.Minute),
	}
	buf.Add(oldEvent)

	recentEvents := buf.GetByTimeWindow(5 * time.Minute)
	// Should not include the old event
	found := false
	for _, e := range recentEvents {
		if e.Event.ID == "old" {
			found = true
			break
		}
	}
	assert.False(t, found)

	// Test Clear
	buf.Clear()
	assert.Equal(t, 0, buf.Size())
	assert.Empty(t, buf.Get(10))
}

func TestCircularBuffer_GetByPattern(t *testing.T) {
	buf, err := NewCircularBuffer(10)
	require.NoError(t, err)

	// Add events with different types
	errorEvent := &domain.UnifiedEvent{
		ID:   "error1",
		Type: domain.EventTypeLog,
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "api",
		},
		Semantic: &domain.SemanticContext{
			Category: "error",
		},
	}
	warnEvent := &domain.UnifiedEvent{
		ID:   "warn1",
		Type: domain.EventTypeLog,
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "api",
		},
		Semantic: &domain.SemanticContext{
			Category: "warning",
		},
	}
	crashEvent := &domain.UnifiedEvent{
		ID:   "crash1",
		Type: domain.EventTypeSystem,
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "db",
		},
		Semantic: &domain.SemanticContext{
			Category: "crash",
		},
	}

	buf.Add(errorEvent)
	buf.Add(warnEvent)
	buf.Add(crashEvent)

	// Test pattern matching
	pattern := func(e *BufferedEvent) bool {
		return e.Event.Semantic != nil && e.Event.Semantic.Category == "error"
	}
	matched := buf.GetByPattern(pattern)
	assert.Len(t, matched, 1)
	assert.Equal(t, "error1", matched[0].Event.ID)

	// Test service pattern
	servicePattern := func(e *BufferedEvent) bool {
		return e.Event.Entity != nil && e.Event.Entity.Name == "api"
	}
	matched = buf.GetByPattern(servicePattern)
	assert.Len(t, matched, 2)
}

func TestCircularBuffer_Concurrent(t *testing.T) {
	buf, err := NewCircularBuffer(1000)
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &domain.UnifiedEvent{
					ID:        string(rune(id*1000 + j)),
					Timestamp: time.Now(),
				}
				buf.Add(event)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				_ = buf.Get(10)
				_ = buf.GetByTimeWindow(1 * time.Minute)
				_ = buf.Size()
			}
		}()
	}

	wg.Wait()
	// Buffer should not crash and size should be reasonable
	size := buf.Size()
	assert.True(t, size > 0 && size <= 1000)
}

func TestNewRealTimeProcessor(t *testing.T) {
	config := &ProcessorConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		CorrelationWindow: 10 * time.Minute,
	}

	proc, err := NewRealTimeProcessor(config)
	assert.NoError(t, err)
	assert.NotNil(t, proc)
	assert.NotNil(t, proc.buffer)
	assert.NotNil(t, proc.patterns)
	assert.NotNil(t, proc.activePatterns)
	assert.Len(t, proc.patternMatchers, 4)
}

func TestRealTimeProcessor_ProcessEvent(t *testing.T) {
	config := &ProcessorConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		CorrelationWindow: 10 * time.Minute,
	}

	proc, err := NewRealTimeProcessor(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Process a single event
	event := &domain.UnifiedEvent{
		ID:        "test1",
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "api",
			Namespace: "prod",
		},
		Semantic: &domain.SemanticContext{
			Category: "error",
			Intent:   "connection-failed",
		},
	}

	result := proc.ProcessEvent(ctx, event)
	assert.NotNil(t, result)
	assert.Equal(t, event.ID, result.TriggerEvent.ID)
	assert.NotEmpty(t, result.ID)
	assert.True(t, result.Score >= 0 && result.Score <= 1)

	// Verify event was buffered
	assert.Equal(t, 1, proc.buffer.Size())
}

func TestRealTimeProcessor_RegisterPattern(t *testing.T) {
	proc, err := NewRealTimeProcessor(&ProcessorConfig{
		BufferSize: 100,
	})
	require.NoError(t, err)

	pattern := &EventPattern{
		ID:             "test-pattern",
		Name:           "Test Pattern",
		Description:    "A test pattern",
		Severity:       "high",
		EventTypes:     []domain.EventType{domain.EventTypeLog},
		TimeWindow:     5 * time.Minute,
		MinOccurrences: 3,
	}

	err = proc.RegisterPattern(pattern)
	assert.NoError(t, err)

	// Try to register duplicate
	err = proc.RegisterPattern(pattern)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Register pattern with no ID
	badPattern := &EventPattern{
		Name: "Bad Pattern",
	}
	err = proc.RegisterPattern(badPattern)
	assert.Error(t, err)
}

func TestRealTimeProcessor_GetActiveCorrelations(t *testing.T) {
	proc, err := NewRealTimeProcessor(&ProcessorConfig{
		BufferSize: 100,
	})
	require.NoError(t, err)

	// Add some active patterns
	result1 := &CorrelationResult{
		ID:    "corr1",
		Score: 0.8,
	}
	result2 := &CorrelationResult{
		ID:    "corr2",
		Score: 0.6,
	}

	proc.mu.Lock()
	proc.activePatterns["pattern1"] = result1
	proc.activePatterns["pattern2"] = result2
	proc.mu.Unlock()

	active := proc.GetActiveCorrelations()
	assert.Len(t, active, 2)
}

func TestRealTimeProcessor_PatternMatchers(t *testing.T) {
	config := &ProcessorConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		CorrelationWindow: 10 * time.Minute,
	}

	proc, err := NewRealTimeProcessor(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Sequence Pattern - multiple errors from same service
	for i := 0; i < 5; i++ {
		event := &domain.UnifiedEvent{
			ID:        "seq" + string(rune(i)),
			Type:      domain.EventTypeLog,
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type: "service",
				Name: "api",
			},
			Semantic: &domain.SemanticContext{
				Category: "error",
			},
		}
		result := proc.ProcessEvent(ctx, event)
		if i >= 2 { // After 3 errors
			assert.Contains(t, result.PatternType, "sequence")
		}
	}

	// Test Temporal Pattern - burst of events
	baseTime := time.Now()
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			ID:        "temporal" + string(rune(i)),
			Type:      domain.EventTypeLog,
			Timestamp: baseTime.Add(time.Duration(i) * time.Second),
			Entity: &domain.EntityContext{
				Type: "service",
				Name: "cache",
			},
			Semantic: &domain.SemanticContext{
				Category: "warning",
			},
		}
		result := proc.ProcessEvent(ctx, event)
		if i >= 4 { // After 5 events in short time
			assert.Contains(t, result.PatternType, "temporal")
		}
	}

	// Test Anomaly Pattern - unusual event type
	// Clear buffer first to avoid temporal pattern interference
	proc.buffer.Clear()
	
	anomalyEvent := &domain.UnifiedEvent{
		ID:        "anomaly1",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "api",
			Namespace: "prod",
		},
		Semantic: &domain.SemanticContext{
			Category: "crash",
			Intent:   "system-crash",
		},
		Impact: &domain.ImpactContext{
			Severity: "critical",
		},
	}
	result := proc.ProcessEvent(ctx, anomalyEvent)
	assert.Contains(t, result.PatternType, "anomaly")
	assert.True(t, result.Score > 0.7)

	// Test Escalation Pattern - warning then error then crash
	// Clear buffer for clean test
	proc.buffer.Clear()
	
	escalationEvents := []struct {
		eventType domain.EventType
		category  string
		delay     time.Duration
	}{
		{domain.EventTypeLog, "warning", 0},
		{domain.EventTypeLog, "error", 30 * time.Second},
		{domain.EventTypeSystem, "crash", 60 * time.Second},
	}

	baseTime = time.Now()
	for i, e := range escalationEvents {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("escalation-%d", i),
			Type:      e.eventType,
			Timestamp: baseTime.Add(e.delay),
			Entity: &domain.EntityContext{
				Type: "service",
				Name: "db",
			},
			Semantic: &domain.SemanticContext{
				Category: e.category,
			},
		}
		result = proc.ProcessEvent(ctx, event)
		if i == 2 { // After crash
			assert.Contains(t, result.PatternType, "escalation")
			assert.True(t, result.Score > 0.8)
		}
	}
}

func TestRealTimeProcessor_EdgeCases(t *testing.T) {
	proc, err := NewRealTimeProcessor(&ProcessorConfig{
		BufferSize: 10,
		TimeWindow: 1 * time.Minute,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Test nil event
	result := proc.ProcessEvent(ctx, nil)
	assert.Nil(t, result)

	// Test event with nil context
	event := &domain.UnifiedEvent{
		ID:        "no-context",
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
	}
	result = proc.ProcessEvent(ctx, event)
	assert.NotNil(t, result)

	// Test event with empty ID
	event = &domain.UnifiedEvent{
		ID:        "",
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
	}
	result = proc.ProcessEvent(ctx, event)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.ID) // Should generate ID

	// Test with cancelled context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	result = proc.ProcessEvent(cancelCtx, event)
	assert.NotNil(t, result) // Should still process

	// Test buffer overflow
	for i := 0; i < 20; i++ {
		event := &domain.UnifiedEvent{
			ID:        "overflow" + string(rune(i)),
			Type:      domain.EventTypeLog,
			Timestamp: time.Now(),
		}
		proc.ProcessEvent(ctx, event)
	}
	assert.Equal(t, 10, proc.buffer.Size()) // Should be capped at buffer size
}

func TestRealTimeProcessor_ComplexScenarios(t *testing.T) {
	proc, err := NewRealTimeProcessor(&ProcessorConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		CorrelationWindow: 10 * time.Minute,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Scenario 1: Service degradation across multiple services
	services := []string{"api", "db", "cache"}
	for _, svc := range services {
		for i := 0; i < 3; i++ {
			event := &domain.UnifiedEvent{
				ID:        svc + "-error-" + string(rune(i)),
				Type:      domain.EventTypeLog,
				Timestamp: time.Now(),
				Entity: &domain.EntityContext{
					Type:      "service",
					Name:      svc,
					Namespace: "prod",
				},
				Semantic: &domain.SemanticContext{
					Category:  "error",
					Intent:    "connection-timeout",
					Narrative: "Connection timeout",
				},
			}
			result := proc.ProcessEvent(ctx, event)
			_ = result
		}
	}

	// Should have detected cross-service issues
	correlations := proc.GetActiveCorrelations()
	assert.True(t, len(correlations) > 0)

	// Scenario 2: Cascading failure
	// First: Database issues
	dbEvent := &domain.UnifiedEvent{
		ID:        "db-crash",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "database",
			Namespace: "prod",
		},
		Semantic: &domain.SemanticContext{
			Category: "crash",
			Intent:   "system-crash",
		},
	}
	proc.ProcessEvent(ctx, dbEvent)

	// Then: API errors due to DB
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 5; i++ {
		apiEvent := &domain.UnifiedEvent{
			ID:        "api-error-cascade-" + string(rune(i)),
			Type:      domain.EventTypeLog,
			Timestamp: time.Now(),
			Entity: &domain.EntityContext{
				Type:      "service",
				Name:      "api",
				Namespace: "prod",
			},
			Semantic: &domain.SemanticContext{
				Category:  "error",
				Intent:    "database-connection-failed",
				Narrative: "Database connection failed",
			},
		}
		result := proc.ProcessEvent(ctx, apiEvent)
		assert.True(t, result.Score > 0.5)
	}
}

func TestPatternMatcher_Implementations(t *testing.T) {
	// Test SequencePatternMatcher
	t.Run("SequencePatternMatcher", func(t *testing.T) {
		matcher := &SequencePatternMatcher{}

		// Create events from same service
		events := []*BufferedEvent{
			{Event: &domain.UnifiedEvent{Type: domain.EventTypeLog, Entity: &domain.EntityContext{Type: "service", Name: "api"}, Semantic: &domain.SemanticContext{Category: "error"}}},
			{Event: &domain.UnifiedEvent{Type: domain.EventTypeLog, Entity: &domain.EntityContext{Type: "service", Name: "api"}, Semantic: &domain.SemanticContext{Category: "error"}}},
			{Event: &domain.UnifiedEvent{Type: domain.EventTypeLog, Entity: &domain.EntityContext{Type: "service", Name: "api"}, Semantic: &domain.SemanticContext{Category: "error"}}},
		}

		pattern, score := matcher.Match(events)
		assert.True(t, pattern)
		assert.True(t, score > 0.5)

		// Test with different services
		mixedEvents := []*BufferedEvent{
			{Event: &domain.UnifiedEvent{Type: domain.EventTypeLog, Entity: &domain.EntityContext{Type: "service", Name: "api"}, Semantic: &domain.SemanticContext{Category: "error"}}},
			{Event: &domain.UnifiedEvent{Type: domain.EventTypeLog, Entity: &domain.EntityContext{Type: "service", Name: "db"}, Semantic: &domain.SemanticContext{Category: "error"}}},
		}
		pattern, score = matcher.Match(mixedEvents)
		assert.False(t, pattern)
		assert.Equal(t, 0.0, score)
	})

	// Test TemporalPatternMatcher
	t.Run("TemporalPatternMatcher", func(t *testing.T) {
		matcher := &TemporalPatternMatcher{}

		// Create burst of events
		now := time.Now()
		events := make([]*BufferedEvent, 10)
		for i := 0; i < 10; i++ {
			events[i] = &BufferedEvent{
				Event: &domain.UnifiedEvent{
					Type:      domain.EventTypeLog,
					Timestamp: now.Add(time.Duration(i) * time.Second),
					Semantic: &domain.SemanticContext{
						Category: "warning",
					},
				},
			}
		}

		pattern, score := matcher.Match(events)
		assert.True(t, pattern)
		assert.True(t, score > 0.6)
	})
}
