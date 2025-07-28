package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// mockCollector implements collectors.CollectorInterface for testing
type mockCollector struct {
	eventChan chan domain.UnifiedEvent
	running   bool
}

func newMockCollector() *mockCollector {
	return &mockCollector{
		eventChan: make(chan domain.UnifiedEvent, 100),
	}
}

func (m *mockCollector) Start(ctx context.Context) error {
	m.running = true
	return nil
}

func (m *mockCollector) Stop() error {
	m.running = false
	close(m.eventChan)
	return nil
}

func (m *mockCollector) Events() <-chan domain.UnifiedEvent {
	return m.eventChan
}

func (m *mockCollector) Health() collectors.CollectorHealth {
	return collectors.CollectorHealth{
		Status:  collectors.HealthStatusHealthy,
		Message: "Mock collector healthy",
	}
}

func (m *mockCollector) Statistics() collectors.CollectorStatistics {
	return collectors.CollectorStatistics{
		StartTime:       time.Now(),
		EventsCollected: 0,
	}
}

func (m *mockCollector) Name() string {
	return "mock"
}

func (m *mockCollector) Type() string {
	return "mock"
}

func (m *mockCollector) SendEvent(event domain.UnifiedEvent) {
	if m.running {
		m.eventChan <- event
	}
}

func TestCollectorIntelligencePipeline_Lifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create manager and pipeline
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	config := DefaultConfig()
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, config)
	require.NoError(t, err)
	require.NotNil(t, pipeline)

	// Register mock collector
	mock := newMockCollector()
	err = manager.Register("mock", mock)
	require.NoError(t, err)

	// Start manager
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)

	// Start pipeline
	err = pipeline.Start()
	require.NoError(t, err)

	// Give it time to initialize
	time.Sleep(100 * time.Millisecond)

	// Stop pipeline
	err = pipeline.Stop()
	require.NoError(t, err)

	// Stop manager
	err = manager.Stop()
	require.NoError(t, err)
}

func TestCollectorIntelligencePipeline_EventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create manager and pipeline
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	config := DefaultConfig()
	config.BatchSize = 1 // Process immediately
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, config)
	require.NoError(t, err)

	// Register mock collector
	mock := newMockCollector()
	err = manager.Register("mock", mock)
	require.NoError(t, err)

	// Start everything
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	err = pipeline.Start()
	require.NoError(t, err)
	defer pipeline.Stop()

	// Send test event
	testEvent := domain.UnifiedEvent{
		ID:        "test-event-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "test",
		Message:   "Test pod created",
		Category:  "test",
		Severity:  "info",
		Kubernetes: &domain.KubernetesData{
			EventType:  "pod_created",
			ObjectKind: "Pod",
			Object:     "test-pod",
		},
	}

	mock.SendEvent(testEvent)

	// Give time to process
	time.Sleep(500 * time.Millisecond)

	// Check statistics
	stats := pipeline.GetStatistics()
	assert.NotNil(t, stats)
	assert.Equal(t, uint64(1), stats["processed_events"])
	assert.Equal(t, uint64(0), stats["correlation_errors"])
}

func TestCollectorIntelligencePipeline_Enrichment(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create manager and pipeline with enrichment enabled
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	config := DefaultConfig()
	config.EnrichmentEnabled = true
	config.BatchSize = 1
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, config)
	require.NoError(t, err)

	// Register mock collector
	mock := newMockCollector()
	err = manager.Register("mock", mock)
	require.NoError(t, err)

	// Start everything
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	err = pipeline.Start()
	require.NoError(t, err)
	defer pipeline.Stop()

	// Test various event types for enrichment
	testCases := []struct {
		name  string
		event domain.UnifiedEvent
	}{
		{
			name: "kubernetes_event",
			event: domain.UnifiedEvent{
				ID:        "k8s-test-1",
				Type:      domain.EventTypeKubernetes,
				Timestamp: time.Now(),
				Source:    "test",
				Message:   "Pod OOM killed",
				Kubernetes: &domain.KubernetesData{
					EventType:  "pod_oom_killed",
					ObjectKind: "Pod",
					Object:     "memory-hog",
				},
			},
		},
		{
			name: "log_event",
			event: domain.UnifiedEvent{
				ID:        "log-test-1",
				Type:      domain.EventTypeLog,
				Timestamp: time.Now(),
				Source:    "test",
				Message:   "Application error",
				Application: &domain.ApplicationData{
					Message: "Application error",
					Level:   "error",
					Logger:  "main",
				},
			},
		},
		{
			name: "network_event",
			event: domain.UnifiedEvent{
				ID:        "net-test-1",
				Type:      domain.EventTypeNetwork,
				Timestamp: time.Now(),
				Source:    "test",
				Message:   "Connection timeout",
				Network: &domain.NetworkData{
					SourceIP:   "10.0.0.1",
					SourcePort: 12345,
					DestIP:     "10.0.0.2",
					DestPort:   80,
					Protocol:   "tcp",
				},
			},
		},
		{
			name: "system_event",
			event: domain.UnifiedEvent{
				ID:        "sys-test-1",
				Type:      domain.EventTypeSystem,
				Timestamp: time.Now(),
				Source:    "test",
				Message:   "High CPU usage",
				Kernel: &domain.KernelData{
					Comm: "system-monitor",
					PID:  1234,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mock.SendEvent(tc.event)
			time.Sleep(200 * time.Millisecond)

			// Verify event was processed
			stats := pipeline.GetStatistics()
			processedCount := stats["processed_events"].(uint64)
			assert.Greater(t, processedCount, uint64(0), "Event should be processed")
		})
	}
}

func TestCollectorIntelligencePipeline_BatchProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create pipeline with batch processing
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	config := DefaultConfig()
	config.BatchSize = 5
	config.BatchTimeout = 1 * time.Second
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, config)
	require.NoError(t, err)

	// Register mock collector
	mock := newMockCollector()
	err = manager.Register("mock", mock)
	require.NoError(t, err)

	// Start everything
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	err = pipeline.Start()
	require.NoError(t, err)
	defer pipeline.Stop()

	// Send batch of events
	for i := 0; i < 10; i++ {
		event := domain.UnifiedEvent{
			ID:        fmt.Sprintf("batch-test-%d", i),
			Type:      domain.EventTypeLog,
			Timestamp: time.Now(),
			Source:    "test",
			Message:   fmt.Sprintf("Batch event %d", i),
		}
		mock.SendEvent(event)
	}

	// Wait for batch processing
	time.Sleep(2 * time.Second)

	// Verify all events were processed
	stats := pipeline.GetStatistics()
	assert.Equal(t, uint64(10), stats["processed_events"])
}

func TestCollectorIntelligencePipeline_CorrelationIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create pipeline
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	config := DefaultConfig()
	config.EnrichmentEnabled = true
	config.BatchSize = 1
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, config)
	require.NoError(t, err)

	// Register mock collector
	mock := newMockCollector()
	err = manager.Register("mock", mock)
	require.NoError(t, err)

	// Start everything
	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	err = pipeline.Start()
	require.NoError(t, err)
	defer pipeline.Stop()

	// Send correlated events with same trace ID
	traceID := "test-trace-123"

	// Deployment event
	mock.SendEvent(domain.UnifiedEvent{
		ID:        "corr-1",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "k8s",
		Message:   "Deployment started",
		Kubernetes: &domain.KubernetesData{
			EventType:  "deployment_started",
			ObjectKind: "Deployment",
			Object:     "test-app",
		},
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  "span-1",
		},
	})

	// Pod creation event
	time.Sleep(50 * time.Millisecond)
	mock.SendEvent(domain.UnifiedEvent{
		ID:        "corr-2",
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "k8s",
		Message:   "Pod created",
		Kubernetes: &domain.KubernetesData{
			EventType:  "pod_created",
			ObjectKind: "Pod",
			Object:     "test-app-pod-1",
		},
		TraceContext: &domain.TraceContext{
			TraceID:      traceID,
			SpanID:       "span-2",
			ParentSpanID: "span-1",
		},
	})

	// Application started event
	time.Sleep(50 * time.Millisecond)
	mock.SendEvent(domain.UnifiedEvent{
		ID:        "corr-3",
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
		Source:    "app",
		Message:   "Application started successfully",
		Application: &domain.ApplicationData{
			Message: "Application started successfully",
			Level:   "info",
		},
		TraceContext: &domain.TraceContext{
			TraceID:      traceID,
			SpanID:       "span-3",
			ParentSpanID: "span-2",
		},
	})

	// Wait for correlation processing
	time.Sleep(1 * time.Second)

	// Check for semantic groups
	groups := pipeline.GetSemanticGroups()
	assert.NotNil(t, groups)

	// Check for findings
	findings := pipeline.GetLatestFindings()
	t.Logf("Findings: %+v", findings)
}

func TestCollectorIntelligencePipeline_ErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test nil manager
	_, err := NewCollectorIntelligencePipeline(nil, logger, DefaultConfig())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "collector manager is required")

	// Test with valid setup
	manager := collectors.NewManager(collectors.DefaultManagerConfig())
	pipeline, err := NewCollectorIntelligencePipeline(manager, logger, DefaultConfig())
	require.NoError(t, err)

	// Test starting without collectors
	err = pipeline.Start()
	assert.NoError(t, err, "Pipeline should start even without collectors")

	// Clean stop
	err = pipeline.Stop()
	assert.NoError(t, err)
}
