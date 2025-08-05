package correlation

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"go.uber.org/zap"
)

// SimpleMockNeo4jDriver - minimal mock that focuses on essential methods
type SimpleMockNeo4jDriver struct {
	mock.Mock
}

func (m *SimpleMockNeo4jDriver) NewSession(ctx context.Context, config neo4j.SessionConfig) neo4j.SessionWithContext {
	return nil // Not used in simple tests
}

func (m *SimpleMockNeo4jDriver) VerifyConnectivity(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *SimpleMockNeo4jDriver) Close(ctx context.Context) error {
	return nil
}

func (m *SimpleMockNeo4jDriver) IsEncrypted() bool {
	return false
}

func (m *SimpleMockNeo4jDriver) Target() url.URL {
	u, _ := url.Parse("bolt://localhost:7687")
	return *u
}

func (m *SimpleMockNeo4jDriver) GetServerInfo(ctx context.Context) (neo4j.ServerInfo, error) {
	return nil, nil
}

func (m *SimpleMockNeo4jDriver) VerifyAuthentication(ctx context.Context, auth *neo4j.AuthToken) error {
	return nil
}

func (m *SimpleMockNeo4jDriver) ExecuteQueryBookmarkManager() neo4j.BookmarkManager {
	return nil
}

// Test helper functions
func createSimpleTestEvent(eventType domain.EventType, namespace, entityName string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        "test-event-" + string(eventType),
		Timestamp: time.Now(),
		Type:      eventType,
		Source:    "test",
		Severity:  domain.EventSeverity("high"),
		K8sContext: &domain.K8sContext{
			Namespace:   namespace,
			Name:        entityName,
			ClusterName: "test-cluster",
		},
	}
}

func createSimpleTestLogger() *zap.Logger {
	return zap.NewNop()
}

// Test DependencyCorrelator creation
func TestNewDependencyCorrelator_Simple(t *testing.T) {
	t.Run("valid creation", func(t *testing.T) {
		mockDriver := &SimpleMockNeo4jDriver{}
		logger := createSimpleTestLogger()

		correlator, err := NewDependencyCorrelator(mockDriver, logger)

		require.NoError(t, err)
		assert.NotNil(t, correlator)
		assert.Equal(t, "dependency-correlator", correlator.Name())
		assert.Equal(t, "1.0.0", correlator.Version())
	})

	t.Run("nil driver", func(t *testing.T) {
		logger := createSimpleTestLogger()

		correlator, err := NewDependencyCorrelator(nil, logger)

		require.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "neo4jDriver is required")
	})

	t.Run("nil logger", func(t *testing.T) {
		mockDriver := &SimpleMockNeo4jDriver{}

		correlator, err := NewDependencyCorrelator(mockDriver, nil)

		require.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "logger is required")
	})
}

// Test event validation
func TestDependencyCorrelator_ValidateEvent_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	t.Run("valid event", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "default", "test-pod")

		err := correlator.ValidateEvent(event)

		assert.NoError(t, err)
	})

	t.Run("unsupported event type", func(t *testing.T) {
		event := createSimpleTestEvent("unknown_event", "default", "test-pod")

		err := correlator.ValidateEvent(event)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "event type not supported")
	})

	t.Run("missing namespace", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "", "test-pod")
		event.K8sContext.Namespace = ""
		event.Entity = nil

		err := correlator.ValidateEvent(event)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required field missing: namespace")
	})
}

// Test health check - this is what we can test without complex mocking
func TestDependencyCorrelator_Health_Simple(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		mockDriver := &SimpleMockNeo4jDriver{}
		logger := createSimpleTestLogger()
		correlator, _ := NewDependencyCorrelator(mockDriver, logger)

		mockDriver.On("VerifyConnectivity", mock.Anything).Return(nil)

		ctx := context.Background()
		err := correlator.Health(ctx)

		assert.NoError(t, err)
		mockDriver.AssertExpectations(t)
	})

	t.Run("unhealthy", func(t *testing.T) {
		mockDriver := &SimpleMockNeo4jDriver{}
		logger := createSimpleTestLogger()
		correlator, _ := NewDependencyCorrelator(mockDriver, logger)

		mockDriver.On("VerifyConnectivity", mock.Anything).Return(assert.AnError)

		ctx := context.Background()
		err := correlator.Health(ctx)

		assert.Error(t, err)
		mockDriver.AssertExpectations(t)
	})
}

// Test capabilities
func TestDependencyCorrelator_Capabilities_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	capabilities := correlator.GetCapabilities()

	assert.Contains(t, capabilities.EventTypes, "pod_failed")
	assert.Contains(t, capabilities.EventTypes, "service_unavailable")
	assert.Contains(t, capabilities.EventTypes, "config_changed")
	assert.Contains(t, capabilities.EventTypes, "volume_mount_failed")
	assert.Contains(t, capabilities.EventTypes, "endpoint_not_ready")
	assert.Contains(t, capabilities.EventTypes, "container_crash")

	assert.Contains(t, capabilities.RequiredData, "namespace")
	assert.Contains(t, capabilities.RequiredData, "cluster")

	assert.Contains(t, capabilities.OptionalData, "pod")
	assert.Contains(t, capabilities.OptionalData, "service")
	assert.Contains(t, capabilities.OptionalData, "configmap")
	assert.Contains(t, capabilities.OptionalData, "secret")
	assert.Contains(t, capabilities.OptionalData, "pvc")

	assert.Equal(t, 24*time.Hour, capabilities.MaxEventAge)
	assert.False(t, capabilities.BatchSupport)
	assert.Len(t, capabilities.Dependencies, 1)
	assert.Equal(t, "neo4j", capabilities.Dependencies[0].Name)
	assert.Equal(t, "database", capabilities.Dependencies[0].Type)
	assert.True(t, capabilities.Dependencies[0].Required)
}

// Test helper functions
func TestDependencyCorrelator_HelperFunctions_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	t.Run("getNamespace", func(t *testing.T) {
		// Test K8sContext namespace
		event := &domain.UnifiedEvent{
			K8sContext: &domain.K8sContext{Namespace: "test-ns"},
		}
		assert.Equal(t, "test-ns", correlator.getNamespace(event))

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
			K8sContext: &domain.K8sContext{ClusterName: "test-cluster"},
		}
		assert.Equal(t, "test-cluster", correlator.getCluster(event))

		// Test unknown fallback
		event = &domain.UnifiedEvent{}
		assert.Equal(t, "unknown", correlator.getCluster(event))
	})

	t.Run("getEntityName", func(t *testing.T) {
		// Test K8sContext name
		event := &domain.UnifiedEvent{
			K8sContext: &domain.K8sContext{Name: "test-entity"},
		}
		assert.Equal(t, "test-entity", correlator.getEntityName(event))

		// Test Entity name fallback
		event = &domain.UnifiedEvent{
			Entity: &domain.EntityContext{Name: "entity-name"},
		}
		assert.Equal(t, "entity-name", correlator.getEntityName(event))

		// Test empty fallback
		event = &domain.UnifiedEvent{}
		assert.Equal(t, "", correlator.getEntityName(event))
	})
}

// Test confidence calculation without Neo4j dependency
func TestDependencyCorrelator_CalculateConfidence_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	t.Run("no findings", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "default", "test-pod")
		findings := []aggregator.Finding{}

		confidence := correlator.calculateConfidence(findings, event)

		assert.Equal(t, 0.0, confidence)
	})

	t.Run("single critical finding", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "default", "test-pod")
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.9,
			},
		}

		confidence := correlator.calculateConfidence(findings, event)

		assert.Equal(t, 0.9, confidence)
	})

	t.Run("multiple findings boost confidence", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "default", "test-pod")
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.8,
			},
			{
				Severity:   aggregator.SeverityHigh,
				Confidence: 0.7,
			},
		}

		confidence := correlator.calculateConfidence(findings, event)

		// Should be weighted average + boost for multiple findings
		// Critical weight = 1.0, High weight = 0.8
		// (0.8*1.0 + 0.7*0.8)/(1.0+0.8) = 1.36/1.8 = 0.755... + 0.1 = 0.855...
		assert.Greater(t, confidence, 0.85)
		assert.Less(t, confidence, 0.87)
	})

	t.Run("confidence capped at 1.0", func(t *testing.T) {
		event := createSimpleTestEvent("pod_failed", "default", "test-pod")
		findings := []aggregator.Finding{
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 1.0,
			},
			{
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.95,
			},
		}

		confidence := correlator.calculateConfidence(findings, event)

		assert.Equal(t, 1.0, confidence)
	})
}

// Test correlation method routing without full Neo4j setup
func TestDependencyCorrelator_CorrelationRouting_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	// Test that different event types would route to different handlers
	// We test this by verifying validation passes for supported events

	supportedEventTypes := []domain.EventType{
		"service_unavailable",
		"endpoint_not_ready",
		"pod_failed",
		"container_crash",
		"config_changed",
		"volume_mount_failed",
	}

	for _, eventType := range supportedEventTypes {
		t.Run("supports "+string(eventType), func(t *testing.T) {
			event := createSimpleTestEvent(eventType, "default", "test-entity")
			err := correlator.ValidateEvent(event)
			assert.NoError(t, err, "Event type %s should be supported", eventType)
		})
	}

	// Test unsupported event type
	t.Run("rejects unsupported event", func(t *testing.T) {
		event := createSimpleTestEvent("unsupported_event", "default", "test-entity")
		err := correlator.ValidateEvent(event)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "event type not supported")
	})
}

// Test GraphCorrelator interface implementation
func TestDependencyCorrelator_GraphCorrelatorInterface_Simple(t *testing.T) {
	mockDriver := &SimpleMockNeo4jDriver{}
	logger := createSimpleTestLogger()
	correlator, _ := NewDependencyCorrelator(mockDriver, logger)

	// Test SetGraphClient
	newMockDriver := &SimpleMockNeo4jDriver{}
	correlator.SetGraphClient(newMockDriver)
	assert.Equal(t, newMockDriver, correlator.neo4jDriver)

	// Test PreloadGraph (should not error for basic implementation)
	ctx := context.Background()
	err := correlator.PreloadGraph(ctx)
	assert.NoError(t, err)
}
