package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestMultiDimensionalEngineBasic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		TemporalWindow:   5 * time.Minute,
		CausalWindow:     1 * time.Minute,
		MinConfidence:    0.7,
		MinCorrelation:   0.5,
		EnableOwnership:  true,
		EnableSpatial:    true,
		EnableTemporal:   true,
		EnableCausal:     true,
		EnableSemantic:   true,
		EnableDependency: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	// Test event without K8s context
	event1 := &domain.UnifiedEvent{
		ID:        "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeLog,
	}

	results, err := engine.Process(ctx, event1)
	assert.NoError(t, err)
	assert.Len(t, results, 0) // No correlations without K8s context

	// Test event with K8s context
	event2 := &domain.UnifiedEvent{
		ID:        "test-2",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKubernetes,
		K8sContext: &domain.K8sContext{
			Name:         "api-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "api",
			NodeName:     "node-1",
			Labels: map[string]string{
				"app": "api",
			},
		},
	}

	results, err = engine.Process(ctx, event2)
	assert.NoError(t, err)
	// First event, no correlations yet
	assert.Len(t, results, 0)
}

func TestOwnershipDimensionCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		MinConfidence:   0.7,
		EnableOwnership: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	// Create events from same deployment
	baseTime := time.Now()

	// First pod event
	event1 := &domain.UnifiedEvent{
		ID:        "ownership-1",
		Timestamp: baseTime,
		Type:      domain.EventTypeKubernetes,
		Severity:  domain.EventSeverityError,
		Message:   "Pod crashed",
		K8sContext: &domain.K8sContext{
			Name:         "api-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "api",
			OwnerReferences: []domain.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "api-7b9d5c4f6",
				},
			},
		},
	}

	results, err := engine.Process(ctx, event1)
	assert.NoError(t, err)
	assert.Len(t, results, 0)

	// Second pod event from same deployment
	event2 := &domain.UnifiedEvent{
		ID:        "ownership-2",
		Timestamp: baseTime.Add(30 * time.Second),
		Type:      domain.EventTypeKubernetes,
		Severity:  domain.EventSeverityError,
		Message:   "Pod OOMKilled",
		K8sContext: &domain.K8sContext{
			Name:         "api-pod-2",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "api",
			OwnerReferences: []domain.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "api-7b9d5c4f6",
				},
			},
		},
	}

	results, err = engine.Process(ctx, event2)
	assert.NoError(t, err)
	require.Len(t, results, 1)

	// Check correlation
	corr := results[0]
	assert.Equal(t, "ownership_same_workload", corr.Type)
	assert.GreaterOrEqual(t, corr.Confidence, 0.8)
	assert.Contains(t, corr.Events, "ownership-1")
	assert.Contains(t, corr.Events, "ownership-2")

	// Verify ownership dimension was found
	hasOwnership := false
	for _, dim := range corr.Dimensions {
		if dim.Dimension == "ownership" {
			hasOwnership = true
			assert.Equal(t, "same_workload", dim.Type)
		}
	}
	assert.True(t, hasOwnership)
}

func TestSpatialDimensionCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		MinConfidence: 0.7,
		EnableSpatial: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	// Create events on same node
	baseTime := time.Now()

	// Multiple events on same node
	for i := 0; i < 4; i++ {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("spatial-%d", i),
			Timestamp: baseTime.Add(time.Duration(i) * time.Second),
			Type:      domain.EventTypeKubernetes,
			Severity:  domain.EventSeverityWarning,
			K8sContext: &domain.K8sContext{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "production",
				NodeName:  "node-1",
				Zone:      "us-east-1a",
			},
		}

		_, err := engine.Process(ctx, event)
		assert.NoError(t, err)
	}

	// One more event should trigger node-level correlation
	event := &domain.UnifiedEvent{
		ID:        "spatial-trigger",
		Timestamp: baseTime.Add(5 * time.Second),
		Type:      domain.EventTypeKubernetes,
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:      "pod-trigger",
			Namespace: "production",
			NodeName:  "node-1",
			Zone:      "us-east-1a",
		},
	}

	results, err := engine.Process(ctx, event)
	assert.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)

	// Find spatial correlation
	var spatialCorr *MultiDimCorrelationResult
	for _, r := range results {
		if r.Type == "spatial_same_node" {
			spatialCorr = r
			break
		}
	}

	require.NotNil(t, spatialCorr)
	assert.GreaterOrEqual(t, len(spatialCorr.Events), 3)
}

func TestTemporalDimensionBurstDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		TemporalWindow: 1 * time.Minute,
		MinConfidence:  0.7,
		EnableTemporal: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	// Create burst of events
	baseTime := time.Now()

	// Generate 15 events in rapid succession
	for i := 0; i < 15; i++ {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("burst-%d", i),
			Timestamp: baseTime.Add(time.Duration(i) * 100 * time.Millisecond),
			Type:      domain.EventTypeNetwork,
			Severity:  domain.EventSeverityWarning,
			K8sContext: &domain.K8sContext{
				Name:      fmt.Sprintf("pod-%d", i%3),
				Namespace: "production",
			},
			Network: &domain.NetworkData{
				StatusCode: 503,
			},
		}

		_, err := engine.Process(ctx, event)
		assert.NoError(t, err)
	}

	// Trigger event should detect burst
	triggerEvent := &domain.UnifiedEvent{
		ID:        "burst-trigger",
		Timestamp: baseTime.Add(2 * time.Second),
		Type:      domain.EventTypeNetwork,
		K8sContext: &domain.K8sContext{
			Name:      "pod-trigger",
			Namespace: "production",
		},
	}

	results, err := engine.Process(ctx, triggerEvent)
	assert.NoError(t, err)

	// Look for burst detection
	var burstCorr *MultiDimCorrelationResult
	for _, r := range results {
		if r.Type == "temporal_event_burst" {
			burstCorr = r
			break
		}
	}

	require.NotNil(t, burstCorr)
	assert.GreaterOrEqual(t, len(burstCorr.Events), 10)
}

func TestCausalDimensionResourceExhaustion(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		CausalWindow:  2 * time.Minute,
		MinConfidence: 0.7,
		EnableCausal:  true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	baseTime := time.Now()

	// OOM event
	oomEvent := &domain.UnifiedEvent{
		ID:        "oom-1",
		Timestamp: baseTime,
		Type:      domain.EventTypeKubernetes,
		Severity:  domain.EventSeverityCritical,
		K8sContext: &domain.K8sContext{
			Name:         "api-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "api",
		},
		Kubernetes: &domain.KubernetesData{
			EventType: "Warning",
			Reason:    "OOMKilling",
			Message:   "Container api exceeded memory limit",
		},
	}

	_, err := engine.Process(ctx, oomEvent)
	assert.NoError(t, err)

	// Subsequent pod failure
	failureEvent := &domain.UnifiedEvent{
		ID:        "failure-1",
		Timestamp: baseTime.Add(10 * time.Second),
		Type:      domain.EventTypeKubernetes,
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:         "api-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "api",
		},
		Kubernetes: &domain.KubernetesData{
			EventType: "Warning",
			Reason:    "BackOff",
			Message:   "Back-off restarting failed container",
		},
	}

	results, err := engine.Process(ctx, failureEvent)
	assert.NoError(t, err)

	// Should find causal correlation
	var causalCorr *MultiDimCorrelationResult
	for _, r := range results {
		for _, dim := range r.Dimensions {
			if dim.Dimension == "causal" && dim.Type == "resource_exhaustion" {
				causalCorr = r
				break
			}
		}
	}

	require.NotNil(t, causalCorr)
	assert.NotNil(t, causalCorr.RootCause)
	assert.Equal(t, "oom-1", causalCorr.RootCause.EventID)
}

func TestMultiDimensionalCascadingFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := EngineConfig{
		TemporalWindow:   5 * time.Minute,
		CausalWindow:     2 * time.Minute,
		MinConfidence:    0.6,
		EnableOwnership:  true,
		EnableTemporal:   true,
		EnableCausal:     true,
		EnableDependency: true,
	}

	engine := NewMultiDimensionalEngine(logger, config)
	ctx := context.Background()

	baseTime := time.Now()

	// Database failure
	dbEvent := &domain.UnifiedEvent{
		ID:        "db-failure",
		Timestamp: baseTime,
		Type:      domain.EventTypeNetwork,
		Severity:  domain.EventSeverityCritical,
		K8sContext: &domain.K8sContext{
			Name:         "mysql-0",
			Namespace:    "production",
			WorkloadKind: "StatefulSet",
			WorkloadName: "mysql",
			Labels: map[string]string{
				"app": "mysql",
			},
		},
		Network: &domain.NetworkData{
			DestPort:   3306,
			StatusCode: 500,
		},
		Impact: &domain.ImpactContext{
			Severity:       "critical",
			BusinessImpact: 0.9,
		},
	}

	_, err := engine.Process(ctx, dbEvent)
	assert.NoError(t, err)

	// API failures due to DB
	for i := 0; i < 3; i++ {
		apiEvent := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("api-failure-%d", i),
			Timestamp: baseTime.Add(time.Duration(i+1) * 5 * time.Second),
			Type:      domain.EventTypeApplication,
			Severity:  domain.EventSeverityError,
			K8sContext: &domain.K8sContext{
				Name:         fmt.Sprintf("api-pod-%d", i),
				Namespace:    "production",
				WorkloadKind: "Deployment",
				WorkloadName: "api",
				Labels: map[string]string{
					"app": "api",
				},
				Dependencies: []domain.ResourceDependency{
					{
						Kind:      "Service",
						Name:      "mysql",
						Namespace: "production",
						Type:      "database",
						Required:  true,
					},
				},
			},
			Application: &domain.ApplicationData{
				Level:   "error",
				Message: "Database connection failed",
			},
		}

		_, err := engine.Process(ctx, apiEvent)
		assert.NoError(t, err)
	}

	// Frontend errors due to API
	frontendEvent := &domain.UnifiedEvent{
		ID:        "frontend-error",
		Timestamp: baseTime.Add(20 * time.Second),
		Type:      domain.EventTypeNetwork,
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Name:         "frontend-pod-1",
			Namespace:    "production",
			WorkloadKind: "Deployment",
			WorkloadName: "frontend",
			Consumers: []domain.K8sResourceRef{
				{
					Kind:      "Service",
					Name:      "api",
					Namespace: "production",
				},
			},
		},
		Network: &domain.NetworkData{
			DestIP:     "10.96.1.100",
			DestPort:   8080,
			StatusCode: 503,
		},
	}

	results, err := engine.Process(ctx, frontendEvent)
	assert.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)

	// Should detect cascading failure pattern
	var cascadeCorr *MultiDimCorrelationResult
	for _, r := range results {
		if r.Type == "cascading_failure" || r.Type == "complex_correlation" {
			cascadeCorr = r
			break
		}
	}

	require.NotNil(t, cascadeCorr)
	assert.GreaterOrEqual(t, len(cascadeCorr.Events), 4)
	assert.NotNil(t, cascadeCorr.RootCause)
	assert.Equal(t, "db-failure", cascadeCorr.RootCause.EventID)
	assert.NotNil(t, cascadeCorr.Impact)
	assert.Equal(t, "critical", cascadeCorr.Impact.Severity)
}

func TestCorrelationGraphIndexing(t *testing.T) {
	graph := NewCorrelationGraph()

	// Add test events
	event1 := &domain.UnifiedEvent{
		ID:        "graph-1",
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Name:         "test-pod-1",
			Namespace:    "default",
			WorkloadKind: "Deployment",
			WorkloadName: "test-app",
			NodeName:     "node-1",
			Zone:         "us-east-1a",
			Labels: map[string]string{
				"app":     "test",
				"version": "v1",
			},
		},
		Semantic: &domain.SemanticContext{
			Intent:   "pod-crash",
			Category: "reliability",
			Domain:   "infrastructure",
		},
	}

	graph.AddEvent(event1)

	// Test various lookups
	assert.Len(t, graph.FindByWorkload("Deployment", "test-app", "default"), 1)
	assert.Len(t, graph.FindByNode("node-1"), 1)
	assert.Len(t, graph.FindByNamespace("default"), 1)
	assert.Len(t, graph.FindByZone("us-east-1a"), 1)
	assert.Len(t, graph.FindByLabel("app", "test", "default"), 1)
	assert.Len(t, graph.FindBySemantic("pod-crash"), 1)
	assert.Len(t, graph.FindByCategory("reliability"), 1)
	assert.Len(t, graph.FindByDomain("infrastructure"), 1)

	// Test time range query
	events := graph.FindInTimeRange(
		time.Now().Add(-1*time.Minute),
		time.Now().Add(1*time.Minute),
	)
	assert.Len(t, events, 1)

	// Verify stats
	stats := graph.Stats()
	assert.Equal(t, int64(1), stats.TotalEvents)
	assert.Equal(t, int64(1), stats.CurrentEvents)
	assert.Equal(t, 1, stats.Indexes.Workloads)
	assert.Equal(t, 1, stats.Indexes.Nodes)
	assert.Equal(t, 1, stats.Indexes.Namespaces)
}

// Helper to create fmt import
func init() {
	_ = fmt.Sprintf
}
