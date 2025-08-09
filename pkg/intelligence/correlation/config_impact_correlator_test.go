package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"go.uber.org/zap"
)

// Test ConfigImpactCorrelator creation
func TestNewConfigImpactCorrelator(t *testing.T) {
	t.Run("valid creation", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()

		correlator, err := NewConfigImpactCorrelator(mockStore, logger)

		require.NoError(t, err)
		assert.NotNil(t, correlator)
		assert.Equal(t, "config-impact-correlator", correlator.Name())
		assert.Equal(t, DefaultCorrelatorVersion, correlator.Version())
	})

	t.Run("nil store", func(t *testing.T) {
		logger := zap.NewNop()

		correlator, err := NewConfigImpactCorrelator(nil, logger)

		require.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "graphStore is required")
	})

	t.Run("nil logger", func(t *testing.T) {
		mockStore := &MockGraphStore{}

		correlator, err := NewConfigImpactCorrelator(mockStore, nil)

		require.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "logger is required")
	})
}

// Test event validation
func TestConfigImpactCorrelator_ValidateEvent(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	t.Run("valid config_changed event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-1",
			Type:      "config_changed",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace:   "default",
				Name:        "test-config",
				ClusterName: "test-cluster",
			},
		}

		err := correlator.ValidateEvent(event)

		assert.NoError(t, err)
	})

	t.Run("valid secret_changed event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-2",
			Type:      "secret_changed",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace:   "default",
				Name:        "test-secret",
				ClusterName: "test-cluster",
			},
		}

		err := correlator.ValidateEvent(event)

		assert.NoError(t, err)
	})

	t.Run("valid pod_restart event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-3",
			Type:      "pod_restart",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace:   "default",
				Name:        "test-pod",
				ClusterName: "test-cluster",
			},
		}

		err := correlator.ValidateEvent(event)

		assert.NoError(t, err)
	})

	t.Run("unsupported event type", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-4",
			Type:      "unsupported_event",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace:   "default",
				Name:        "test",
				ClusterName: "test-cluster",
			},
		}

		err := correlator.ValidateEvent(event)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "event type unsupported_event not supported")
	})

	t.Run("missing namespace", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-5",
			Type:      "config_changed",
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Name:        "test-config",
				ClusterName: "test-cluster",
			},
		}

		err := correlator.ValidateEvent(event)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required field namespace is missing")
	})
}

// Test capabilities
func TestConfigImpactCorrelator_Capabilities(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	capabilities := correlator.GetCapabilities()

	assert.Contains(t, capabilities.EventTypes, "config_changed")
	assert.Contains(t, capabilities.EventTypes, "secret_changed")
	assert.Contains(t, capabilities.EventTypes, "pod_restart")
	assert.Contains(t, capabilities.EventTypes, "pod_crash")
	assert.Contains(t, capabilities.EventTypes, "container_restart")
	assert.Contains(t, capabilities.EventTypes, "deployment_rollout")

	assert.Contains(t, capabilities.RequiredData, "namespace")
	assert.Contains(t, capabilities.RequiredData, "cluster")

	assert.Contains(t, capabilities.OptionalData, "configmap")
	assert.Contains(t, capabilities.OptionalData, "secret")
	assert.Contains(t, capabilities.OptionalData, "pod")
	assert.Contains(t, capabilities.OptionalData, "deployment")

	assert.Equal(t, 24*time.Hour, capabilities.MaxEventAge)
	assert.False(t, capabilities.BatchSupport)
}

// Test health check
func TestConfigImpactCorrelator_Health(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()
		correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

		mockStore.On("HealthCheck", mock.Anything).Return(nil)

		ctx := context.Background()
		err := correlator.Health(ctx)

		assert.NoError(t, err)
		mockStore.AssertExpectations(t)
	})

	t.Run("unhealthy", func(t *testing.T) {
		mockStore := &MockGraphStore{}
		logger := zap.NewNop()
		correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

		mockStore.On("HealthCheck", mock.Anything).Return(assert.AnError)

		ctx := context.Background()
		err := correlator.Health(ctx)

		assert.Error(t, err)
		mockStore.AssertExpectations(t)
	})
}

// Test helper functions
func TestConfigImpactCorrelator_HelperFunctions(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	t.Run("getNamespace", func(t *testing.T) {
		// Test K8sContext namespace
		event := &domain.UnifiedEvent{
			K8sContext: &domain.K8sContext{Namespace: "k8s-ns"},
		}
		assert.Equal(t, "k8s-ns", correlator.getNamespace(event))

		// Test Entity namespace fallback
		event = &domain.UnifiedEvent{
			Entity: &domain.EntityContext{Namespace: "entity-ns"},
		}
		assert.Equal(t, "entity-ns", correlator.getNamespace(event))

		// Test default fallback
		event = &domain.UnifiedEvent{}
		assert.Equal(t, "default", correlator.getNamespace(event))
	})

	t.Run("getCluster", func(t *testing.T) {
		// Test K8sContext cluster
		event := &domain.UnifiedEvent{
			K8sContext: &domain.K8sContext{ClusterName: "prod-cluster"},
		}
		assert.Equal(t, "prod-cluster", correlator.getCluster(event))

		// Test unknown fallback
		event = &domain.UnifiedEvent{}
		assert.Equal(t, "unknown", correlator.getCluster(event))
	})

	t.Run("getEntityName", func(t *testing.T) {
		// Test K8sContext name
		event := &domain.UnifiedEvent{
			K8sContext: &domain.K8sContext{Name: "my-config"},
		}
		assert.Equal(t, "my-config", correlator.getEntityName(event))

		// Test Entity name fallback
		event = &domain.UnifiedEvent{
			Entity: &domain.EntityContext{Name: "entity-config"},
		}
		assert.Equal(t, "entity-config", correlator.getEntityName(event))

		// Test empty fallback
		event = &domain.UnifiedEvent{}
		assert.Equal(t, "", correlator.getEntityName(event))
	})

	t.Run("interfaceSliceToStringSlice", func(t *testing.T) {
		// Test normal conversion
		input := []interface{}{"one", "two", "three"}
		result := correlator.interfaceSliceToStringSlice(input)
		assert.Equal(t, []string{"one", "two", "three"}, result)

		// Test mixed types (only strings extracted)
		input = []interface{}{"one", 2, "three", true}
		result = correlator.interfaceSliceToStringSlice(input)
		assert.Equal(t, []string{"one", "three"}, result)

		// Test nil input
		result = correlator.interfaceSliceToStringSlice(nil)
		assert.Empty(t, result)

		// Test non-slice input
		result = correlator.interfaceSliceToStringSlice("not a slice")
		assert.Empty(t, result)
	})
}

// Test confidence calculation
func TestConfigImpactCorrelator_CalculateConfidence(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	t.Run("no findings", func(t *testing.T) {
		findings := []aggregator.Finding{}
		confidence := correlator.calculateConfidence(findings)
		assert.Equal(t, 0.0, confidence)
	})

	t.Run("single critical finding", func(t *testing.T) {
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.9,
			},
		}
		confidence := correlator.calculateConfidence(findings)
		assert.Equal(t, 0.9, confidence)
	})

	t.Run("multiple findings boost", func(t *testing.T) {
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityHigh,
				Confidence: 0.8,
			},
			{
				Severity:   aggregator.SeverityMedium,
				Confidence: 0.7,
			},
		}
		confidence := correlator.calculateConfidence(findings)
		// Should be weighted average + 0.1 boost
		// High weight = 0.8, Medium weight = 0.6
		// (0.8*0.8 + 0.7*0.6)/(0.8+0.6) = 1.06/1.4 = 0.757 + 0.1 = 0.857
		assert.Greater(t, confidence, 0.85)
		assert.Less(t, confidence, 0.87)
	})

	t.Run("confidence capped at 1.0", func(t *testing.T) {
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.95,
			},
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.95,
			},
		}
		confidence := correlator.calculateConfidence(findings)
		assert.Equal(t, 1.0, confidence)
	})
}

// Test GraphCorrelator interface
func TestConfigImpactCorrelator_GraphCorrelatorInterface(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	// Test SetGraphClient
	newMockStore := &MockGraphStore{}
	correlator.SetGraphClient(newMockStore)
	assert.Equal(t, newMockStore, correlator.graphStore)

	// Test PreloadGraph
	ctx := context.Background()
	err := correlator.PreloadGraph(ctx)
	assert.NoError(t, err)
}

// Test event routing
func TestConfigImpactCorrelator_EventRouting(t *testing.T) {
	mockStore := &MockGraphStore{}
	logger := zap.NewNop()
	correlator, _ := NewConfigImpactCorrelator(mockStore, logger)

	supportedEventTypes := []domain.EventType{
		"config_changed",
		"secret_changed",
		"pod_restart",
		"pod_crash",
		"container_restart",
		"deployment_rollout",
	}

	for _, eventType := range supportedEventTypes {
		t.Run("supports "+string(eventType), func(t *testing.T) {
			event := &domain.UnifiedEvent{
				ID:        "test-" + string(eventType),
				Type:      eventType,
				Timestamp: time.Now(),
				K8sContext: &domain.K8sContext{
					Namespace:   "default",
					Name:        "test-entity",
					ClusterName: "test-cluster",
				},
			}
			err := correlator.ValidateEvent(event)
			assert.NoError(t, err, "Event type %s should be supported", eventType)
		})
	}
}
