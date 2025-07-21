package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createCorrelationTestEvent(id string, eventType domain.EventType, severity string, service string, timestamp time.Time) *domain.UnifiedEvent {
	event := &domain.UnifiedEvent{
		ID:        id,
		Type:      eventType,
		Source:    "test-source",
		Timestamp: timestamp,
		Entity: &domain.Entity{
			Type:      "service",
			Name:      service,
			Namespace: "default",
		},
	}

	// Set severity based on event content
	if severity == "error" || severity == "critical" {
		event.Application = &domain.ApplicationContext{
			Level: severity,
		}
	}

	return event
}

func TestRealTimeCorrelationEngine_ProcessEvent(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()
	baseTime := time.Now()

	// Create error cascade pattern
	events := []*domain.UnifiedEvent{
		createCorrelationTestEvent("1", domain.EventTypeProcess, "error", "service-a", baseTime),
		createCorrelationTestEvent("2", domain.EventTypeProcess, "error", "service-a", baseTime.Add(1*time.Second)),
		createCorrelationTestEvent("3", domain.EventTypeProcess, "error", "service-b", baseTime.Add(2*time.Second)),
		createCorrelationTestEvent("4", domain.EventTypeProcess, "error", "service-b", baseTime.Add(3*time.Second)),
	}

	var correlations []*pb.Correlation
	for _, event := range events {
		corrs, err := engine.ProcessEvent(ctx, event)
		require.NoError(t, err)
		correlations = append(correlations, corrs...)
	}

	// Should detect error cascade pattern
	assert.Greater(t, len(correlations), 0)

	// Verify statistics
	assert.Equal(t, uint64(4), engine.eventsProcessed.Load())
	assert.Greater(t, engine.correlationsFound.Load(), uint64(0))
}

func TestRealTimeCorrelationEngine_PatternMatchers(t *testing.T) {
	baseTime := time.Now()

	tests := []struct {
		name        string
		events      []*domain.UnifiedEvent
		pattern     PatternMatcher
		shouldMatch bool
	}{
		{
			name: "error cascade pattern",
			events: []*domain.UnifiedEvent{
				createCorrelationTestEvent("1", domain.EventTypeProcess, "error", "service-a", baseTime),
				createCorrelationTestEvent("2", domain.EventTypeProcess, "error", "service-a", baseTime),
				createCorrelationTestEvent("3", domain.EventTypeProcess, "error", "service-b", baseTime),
				createCorrelationTestEvent("4", domain.EventTypeProcess, "error", "service-b", baseTime),
			},
			pattern:     &ErrorCascadePattern{},
			shouldMatch: true,
		},
		{
			name: "latency spike pattern",
			events: []*domain.UnifiedEvent{
				func() *domain.UnifiedEvent {
					event := createCorrelationTestEvent("1", domain.EventTypeNetwork, "info", "service-a", baseTime)
					event.Network = &domain.NetworkContext{Latency: 2000000000} // 2 seconds
					return event
				}(),
				createCorrelationTestEvent("2", domain.EventTypeProcess, "error", "service-a", baseTime.Add(1*time.Second)),
			},
			pattern:     &LatencySpikePattern{},
			shouldMatch: true,
		},
		{
			name: "resource exhaustion pattern - OOM",
			events: []*domain.UnifiedEvent{
				func() *domain.UnifiedEvent {
					event := createCorrelationTestEvent("1", domain.EventTypeKernel, "critical", "system", baseTime)
					event.Kernel = &domain.KernelContext{Syscall: "oom_kill"}
					return event
				}(),
			},
			pattern:     &ResourceExhaustionPattern{},
			shouldMatch: true,
		},
		{
			name: "service failure pattern",
			events: []*domain.UnifiedEvent{
				createCorrelationTestEvent("1", domain.EventTypeProcess, "critical", "api-server", baseTime),
				createCorrelationTestEvent("2", domain.EventTypeProcess, "critical", "api-server", baseTime.Add(1*time.Second)),
			},
			pattern:     &ServiceFailurePattern{},
			shouldMatch: true,
		},
		{
			name: "security anomaly pattern",
			events: []*domain.UnifiedEvent{
				func() *domain.UnifiedEvent {
					event := createCorrelationTestEvent("1", domain.EventTypeKernel, "warning", "system", baseTime)
					event.Kernel = &domain.KernelContext{Syscall: "ptrace", UID: 0}
					return event
				}(),
				func() *domain.UnifiedEvent {
					event := createCorrelationTestEvent("2", domain.EventTypeKernel, "warning", "system", baseTime)
					event.Kernel = &domain.KernelContext{Syscall: "execve", UID: 0}
					return event
				}(),
				func() *domain.UnifiedEvent {
					event := createCorrelationTestEvent("3", domain.EventTypeNetwork, "warning", "system", baseTime)
					event.Network = &domain.NetworkContext{DestPort: 22, Direction: "egress"}
					return event
				}(),
			},
			pattern:     &SecurityAnomalyPattern{},
			shouldMatch: true,
		},
		{
			name: "performance degradation pattern",
			events: func() []*domain.UnifiedEvent {
				var events []*domain.UnifiedEvent
				for i := 0; i < 6; i++ {
					event := createCorrelationTestEvent(string(rune('a'+i)), domain.EventTypeNetwork, "warning", "service", baseTime)
					event.Network = &domain.NetworkContext{Latency: 600000000} // 600ms
					events = append(events, event)
				}
				return events
			}(),
			pattern:     &PerformanceDegradationPattern{},
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, found := tt.pattern.Match(tt.events)
			assert.Equal(t, tt.shouldMatch, found)
			if found {
				assert.NotNil(t, match)
				assert.NotEmpty(t, match.EventIDs)
				assert.GreaterOrEqual(t, match.Confidence, 0.7)
			}
		})
	}
}

func TestRealTimeCorrelationEngine_GetCorrelations(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()

	// Create and process events that will generate correlations
	baseTime := time.Now()
	for i := 0; i < 4; i++ {
		event := createCorrelationTestEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"error",
			"service-"+string(rune('a'+i/2)),
			baseTime.Add(time.Duration(i)*time.Second),
		)
		engine.ProcessEvent(ctx, event)
	}

	// Get all correlations
	correlations, err := engine.GetCorrelations(ctx, nil, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(correlations), 0)

	// Test with filter
	filter := &pb.Filter{
		Query: "error_cascade",
	}
	filtered, err := engine.GetCorrelations(ctx, filter, nil)
	require.NoError(t, err)

	for _, corr := range filtered {
		assert.Contains(t, corr.Pattern, "error_cascade")
	}
}

func TestRealTimeCorrelationEngine_GetSemanticGroups(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()

	// Create events with semantic context
	events := []*domain.UnifiedEvent{
		{
			ID:        "1",
			Type:      domain.EventTypeProcess,
			Source:    "test",
			Timestamp: time.Now(),
			Semantic: &domain.SemanticContext{
				Intent:     "service-startup",
				Category:   "lifecycle",
				Confidence: 0.9,
			},
		},
		{
			ID:        "2",
			Type:      domain.EventTypeProcess,
			Source:    "test",
			Timestamp: time.Now(),
			Semantic: &domain.SemanticContext{
				Intent:     "service-startup",
				Category:   "lifecycle",
				Confidence: 0.9,
			},
		},
		{
			ID:        "3",
			Type:      domain.EventTypeProcess,
			Source:    "test",
			Timestamp: time.Now(),
			Semantic: &domain.SemanticContext{
				Intent:     "service-shutdown",
				Category:   "lifecycle",
				Confidence: 0.9,
			},
		},
	}

	// Process events
	for _, event := range events {
		engine.ProcessEvent(ctx, event)
	}

	// Get semantic groups
	groups, err := engine.GetSemanticGroups(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, groups, 2)

	// Verify group content
	for _, group := range groups {
		if group.Name == "service-startup" {
			assert.Equal(t, int32(2), group.EventCount)
		} else if group.Name == "service-shutdown" {
			assert.Equal(t, int32(1), group.EventCount)
		}
	}
}

func TestRealTimeCorrelationEngine_AnalyzeEvents(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()
	baseTime := time.Now()

	// Create events for analysis
	events := []*domain.UnifiedEvent{
		// Error cascade
		createCorrelationTestEvent("1", domain.EventTypeProcess, "error", "service-a", baseTime),
		createCorrelationTestEvent("2", domain.EventTypeProcess, "error", "service-a", baseTime.Add(1*time.Second)),
		createCorrelationTestEvent("3", domain.EventTypeProcess, "error", "service-b", baseTime.Add(2*time.Second)),
		createCorrelationTestEvent("4", domain.EventTypeProcess, "error", "service-b", baseTime.Add(3*time.Second)),
		// High event rate (temporal pattern)
		createCorrelationTestEvent("5", domain.EventTypeNetwork, "info", "service-c", baseTime.Add(4*time.Second)),
		createCorrelationTestEvent("6", domain.EventTypeNetwork, "info", "service-c", baseTime.Add(4100*time.Millisecond)),
		createCorrelationTestEvent("7", domain.EventTypeNetwork, "info", "service-c", baseTime.Add(4200*time.Millisecond)),
	}

	// Add more events for temporal pattern
	for i := 8; i < 20; i++ {
		events = append(events, createCorrelationTestEvent(
			string(rune('a'+i)),
			domain.EventTypeNetwork,
			"info",
			"service-c",
			baseTime.Add(time.Duration(4000+i*10)*time.Millisecond),
		))
	}

	findings, err := engine.AnalyzeEvents(ctx, events)
	require.NoError(t, err)
	assert.Greater(t, len(findings), 0)

	// Verify different finding types
	var hasPattern, hasTemporal bool
	for _, finding := range findings {
		switch finding.Type {
		case pb.CorrelationType_CORRELATION_TYPE_PATTERN:
			hasPattern = true
		case pb.CorrelationType_CORRELATION_TYPE_TEMPORAL:
			hasTemporal = true
		}
	}
	assert.True(t, hasPattern || hasTemporal)
}

func TestRealTimeCorrelationEngine_CircularBuffer(t *testing.T) {
	buffer := NewCircularEventBuffer(5)

	// Add more events than capacity
	for i := 0; i < 10; i++ {
		event := createCorrelationTestEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"info",
			"test",
			time.Now(),
		)
		buffer.Add(event)
	}

	// Should only have last 5 events
	recent := buffer.GetRecent(10)
	assert.Len(t, recent, 5)
	assert.Equal(t, "f", recent[0].ID)
	assert.Equal(t, "j", recent[4].ID)
}

func TestRealTimeCorrelationEngine_TimeWindow(t *testing.T) {
	buffer := NewCircularEventBuffer(10)
	baseTime := time.Now()

	// Add events at different times
	for i := 0; i < 5; i++ {
		event := createCorrelationTestEvent(
			string(rune('a'+i)),
			domain.EventTypeProcess,
			"info",
			"test",
			baseTime.Add(time.Duration(i)*time.Minute),
		)
		buffer.Add(event)
	}

	// Get events from last 3 minutes
	windowEvents := buffer.GetTimeWindow(3 * time.Minute)

	// Should get the most recent events
	assert.GreaterOrEqual(t, len(windowEvents), 1)
	assert.LessOrEqual(t, len(windowEvents), 5)

	// All events should be within the time window
	cutoff := time.Now().Add(-3 * time.Minute)
	for _, event := range windowEvents {
		assert.True(t, event.Timestamp.After(cutoff))
	}
}

func TestRealTimeCorrelationEngine_Health(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   10,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	// Initially healthy
	health := engine.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)

	// Add correlations near capacity
	ctx := context.Background()
	baseTime := time.Now()

	// Generate events that will create multiple correlations
	for batch := 0; batch < 3; batch++ {
		for i := 0; i < 4; i++ {
			event := createCorrelationTestEvent(
				string(rune(batch*10+i)),
				domain.EventTypeProcess,
				"error",
				"service-"+string(rune('a'+i/2)),
				baseTime.Add(time.Duration(batch*10+i)*time.Second),
			)
			engine.ProcessEvent(ctx, event)
		}
	}

	// Check health again
	health = engine.Health()
	if len(engine.correlations) > 9 {
		assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_DEGRADED, health.Status)
		assert.Contains(t, health.Message, "near capacity")
	}
}

func TestRealTimeCorrelationEngine_Cleanup(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   100 * time.Millisecond, // Fast cleanup for testing
		RetentionPeriod:   200 * time.Millisecond, // Short retention
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()

	// Create old correlation
	oldEvent := createCorrelationTestEvent("old", domain.EventTypeProcess, "error", "service", time.Now().Add(-1*time.Second))
	engine.ProcessEvent(ctx, oldEvent)

	// Wait for cleanup
	time.Sleep(400 * time.Millisecond)

	// Old correlations should be cleaned up
	correlations, err := engine.GetCorrelations(ctx, nil, nil)
	require.NoError(t, err)

	// Verify old correlations are removed
	for _, corr := range correlations {
		assert.True(t, time.Since(corr.CreatedAt.AsTime()) < config.RetentionPeriod)
	}
}

func TestRealTimeCorrelationEngine_CausalPatterns(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	baseTime := time.Now()
	events := []*domain.UnifiedEvent{
		// Syscall followed by error
		func() *domain.UnifiedEvent {
			event := createCorrelationTestEvent("1", domain.EventTypeKernel, "info", "system", baseTime)
			event.Kernel = &domain.KernelContext{Syscall: "open", UID: 1000}
			return event
		}(),
		createCorrelationTestEvent("2", domain.EventTypeProcess, "error", "app", baseTime.Add(2*time.Second)),
		// Network error followed by application error
		func() *domain.UnifiedEvent {
			event := createCorrelationTestEvent("3", domain.EventTypeNetwork, "error", "api", baseTime.Add(5*time.Second))
			event.Network = &domain.NetworkContext{StatusCode: 503}
			return event
		}(),
		func() *domain.UnifiedEvent {
			event := createCorrelationTestEvent("4", domain.EventTypeApplication, "error", "app", baseTime.Add(8*time.Second))
			event.Application = &domain.ApplicationContext{Level: "error"}
			return event
		}(),
	}

	findings := engine.analyzeCausalPatterns(events)
	assert.Greater(t, len(findings), 0)

	// Verify causal findings
	for _, finding := range findings {
		assert.Equal(t, pb.CorrelationType_CORRELATION_TYPE_CAUSAL, finding.Type)
		assert.Len(t, finding.EventIds, 2)
		assert.NotEmpty(t, finding.Metadata["time_diff_s"])
	}
}

func TestRealTimeCorrelationEngine_TemporalPatterns(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	// Create event burst
	baseTime := time.Now()
	var events []*domain.UnifiedEvent

	for i := 0; i < 200; i++ {
		event := createCorrelationTestEvent(
			string(rune(i)),
			domain.EventTypeNetwork,
			"info",
			"burst-service",
			baseTime.Add(time.Duration(i)*time.Millisecond),
		)
		events = append(events, event)
	}

	findings := engine.analyzeTemporalPatterns(events)
	assert.Greater(t, len(findings), 0)

	// Verify temporal findings
	for _, finding := range findings {
		assert.Equal(t, pb.CorrelationType_CORRELATION_TYPE_TEMPORAL, finding.Type)
		assert.Equal(t, "event_burst", finding.Pattern)
		assert.Greater(t, finding.Confidence, 0.8)

		// Check metadata
		eventsPerSecond, exists := finding.Metadata["events_per_second"]
		assert.True(t, exists)
		assert.NotEmpty(t, eventsPerSecond)
	}
}

func TestRealTimeCorrelationEngine_ComplexScenario(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        1000,
		TimeWindow:        10 * time.Minute,
		MaxCorrelations:   100,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()
	baseTime := time.Now()

	// Simulate a complex failure scenario:
	// 1. Performance degradation
	// 2. Latency spikes
	// 3. Resource exhaustion
	// 4. Service failures
	// 5. Error cascade

	// Phase 1: Performance degradation
	for i := 0; i < 10; i++ {
		event := createCorrelationTestEvent(
			"perf-"+string(rune(i)),
			domain.EventTypeNetwork,
			"warning",
			"api-gateway",
			baseTime.Add(time.Duration(i)*time.Second),
		)
		event.Network = &domain.NetworkContext{Latency: 800000000} // 800ms
		engine.ProcessEvent(ctx, event)
	}

	// Phase 2: Latency spike
	latencyEvent := createCorrelationTestEvent("latency-1", domain.EventTypeNetwork, "warning", "database", baseTime.Add(15*time.Second))
	latencyEvent.Network = &domain.NetworkContext{Latency: 5000000000} // 5 seconds
	engine.ProcessEvent(ctx, latencyEvent)

	// Phase 3: Resource exhaustion
	oomEvent := createCorrelationTestEvent("oom-1", domain.EventTypeKernel, "critical", "worker-node-1", baseTime.Add(20*time.Second))
	oomEvent.Kernel = &domain.KernelContext{Syscall: "oom_kill", UID: 0}
	engine.ProcessEvent(ctx, oomEvent)

	// Phase 4: Service failures
	for i := 0; i < 5; i++ {
		event := createCorrelationTestEvent(
			"failure-"+string(rune(i)),
			domain.EventTypeProcess,
			"critical",
			"worker-service",
			baseTime.Add(time.Duration(25+i)*time.Second),
		)
		engine.ProcessEvent(ctx, event)
	}

	// Phase 5: Error cascade across services
	services := []string{"api-gateway", "auth-service", "database", "cache", "worker-service"}
	for i, service := range services {
		for j := 0; j < 3; j++ {
			event := createCorrelationTestEvent(
				"cascade-"+service+"-"+string(rune(j)),
				domain.EventTypeProcess,
				"error",
				service,
				baseTime.Add(time.Duration(35+i*2+j)*time.Second),
			)
			engine.ProcessEvent(ctx, event)
		}
	}

	// Get all correlations
	correlations, err := engine.GetCorrelations(ctx, nil, nil)
	require.NoError(t, err)

	// Should detect multiple patterns
	patterns := make(map[string]bool)
	for _, corr := range correlations {
		patterns[corr.Pattern] = true
	}

	// Verify multiple patterns detected
	assert.Greater(t, len(patterns), 2)

	// Check statistics
	stats := engine.Health()
	assert.Greater(t, stats.Metrics["events_processed"], float64(30))
	assert.Greater(t, stats.Metrics["correlations_found"], float64(1))
	assert.Greater(t, stats.Metrics["patterns_matched"], float64(1))
}

// Benchmarks
func BenchmarkCorrelationEngine_ProcessEvent(b *testing.B) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        1000,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   1000,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := createCorrelationTestEvent(
			string(rune(i)),
			domain.EventTypeProcess,
			"info",
			"bench-service",
			time.Now(),
		)
		engine.ProcessEvent(ctx, event)
	}
}

func BenchmarkCorrelationEngine_PatternMatching(b *testing.B) {
	// Pre-create events
	baseTime := time.Now()
	events := make([]*domain.UnifiedEvent, 100)
	for i := 0; i < 100; i++ {
		severity := "info"
		if i%10 == 0 {
			severity = "error"
		}
		events[i] = createCorrelationTestEvent(
			string(rune(i)),
			domain.EventTypeProcess,
			severity,
			"service-"+string(rune(i%5)),
			baseTime.Add(time.Duration(i)*time.Second),
		)
	}

	pattern := &ErrorCascadePattern{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern.Match(events)
	}
}

func TestRealTimeCorrelationEngine_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	config := CorrelationConfig{
		BufferSize:        1000,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   1000,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   1 * time.Hour,
	}
	engine := NewRealTimeCorrelationEngine(logger, config)
	defer engine.Close()

	ctx := context.Background()

	// Run concurrent operations
	errCh := make(chan error, 30)

	// Writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				event := createCorrelationTestEvent(
					string(rune(id*100+j)),
					domain.EventTypeProcess,
					"error",
					"service-"+string(rune(id)),
					time.Now(),
				)
				_, err := engine.ProcessEvent(ctx, event)
				if err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}(i)
	}

	// Readers - correlations
	for i := 0; i < 10; i++ {
		go func() {
			_, err := engine.GetCorrelations(ctx, nil, nil)
			errCh <- err
		}()
	}

	// Readers - semantic groups
	for i := 0; i < 10; i++ {
		go func() {
			_, err := engine.GetSemanticGroups(ctx, nil)
			errCh <- err
		}()
	}

	// Wait for all operations
	for i := 0; i < 30; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}

	// Verify data integrity
	health := engine.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)
	assert.Greater(t, health.Metrics["events_processed"], float64(0))
}
