package correlation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
)

// MockCorrelator implements the StandardCorrelator interface for testing
type MockCorrelator struct {
	*BaseCorrelator
	correlateFunc func(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error)
	healthFunc    func(ctx context.Context) error
}

func (m *MockCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error) {
	if m.correlateFunc != nil {
		return m.correlateFunc(ctx, event)
	}
	return &aggregator.CorrelatorOutput{
		CorrelatorName:    m.Name(),
		CorrelatorVersion: m.Version(),
		Findings:          []aggregator.Finding{},
		Confidence:        0.5,
	}, nil
}

func (m *MockCorrelator) Health(ctx context.Context) error {
	if m.healthFunc != nil {
		return m.healthFunc(ctx)
	}
	return nil
}

func TestBaseCorrelator(t *testing.T) {
	capabilities := CorrelatorCapabilities{
		EventTypes:   []string{"pod_crash", "memory_pressure"},
		RequiredData: []string{"namespace", "pod"},
		OptionalData: []string{"container"},
		MaxEventAge:  30 * time.Minute,
	}

	base := NewBaseCorrelator("TestCorrelator", "1.0.0", capabilities)

	assert.Equal(t, "TestCorrelator", base.Name())
	assert.Equal(t, "1.0.0", base.Version())
	assert.Equal(t, capabilities, base.GetCapabilities())
}

func TestValidateEvent_Success(t *testing.T) {
	capabilities := CorrelatorCapabilities{
		EventTypes:   []string{"pod_crash"},
		RequiredData: []string{"namespace", "pod"},
		MaxEventAge:  30 * time.Minute,
	}

	base := NewBaseCorrelator("TestCorrelator", "1.0.0", capabilities)

	event := &domain.UnifiedEvent{
		ID:        "test-event",
		Type:      domain.EventType("pod_crash"),
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "default",
			Kind:      "Pod",
			Name:      "test-pod",
		},
	}

	err := base.ValidateEvent(event)
	assert.NoError(t, err)
}

func TestValidateEvent_UnsupportedType(t *testing.T) {
	capabilities := CorrelatorCapabilities{
		EventTypes: []string{"pod_crash"},
	}

	base := NewBaseCorrelator("TestCorrelator", "1.0.0", capabilities)

	event := &domain.UnifiedEvent{
		Type:      domain.EventType("network_error"),
		Timestamp: time.Now(),
	}

	err := base.ValidateEvent(event)
	require.Error(t, err)

	var corrErr *CorrelatorError
	assert.True(t, errors.As(err, &corrErr))
	assert.Equal(t, ErrorTypeUnsupportedEvent, corrErr.Type)
}

func TestValidateEvent_EventTooOld(t *testing.T) {
	capabilities := CorrelatorCapabilities{
		EventTypes:  []string{"pod_crash"},
		MaxEventAge: 30 * time.Minute,
	}

	base := NewBaseCorrelator("TestCorrelator", "1.0.0", capabilities)

	event := &domain.UnifiedEvent{
		Type:      domain.EventType("pod_crash"),
		Timestamp: time.Now().Add(-1 * time.Hour),
	}

	err := base.ValidateEvent(event)
	require.Error(t, err)

	var corrErr *CorrelatorError
	assert.True(t, errors.As(err, &corrErr))
	assert.Equal(t, ErrorTypeEventTooOld, corrErr.Type)
}

func TestValidateEvent_MissingRequiredData(t *testing.T) {
	capabilities := CorrelatorCapabilities{
		EventTypes:   []string{"pod_crash"},
		RequiredData: []string{"namespace", "pod"},
	}

	base := NewBaseCorrelator("TestCorrelator", "1.0.0", capabilities)

	event := &domain.UnifiedEvent{
		Type:      domain.EventType("pod_crash"),
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "default", // Missing pod name
		},
	}

	err := base.ValidateEvent(event)
	require.Error(t, err)

	var corrErr *CorrelatorError
	assert.True(t, errors.As(err, &corrErr))
	assert.Equal(t, ErrorTypeMissingData, corrErr.Type)
}

func TestHasField(t *testing.T) {
	base := NewBaseCorrelator("TestCorrelator", "1.0.0", CorrelatorCapabilities{})

	event := &domain.UnifiedEvent{
		Severity: domain.EventSeverity("high"),
		K8sContext: &domain.K8sContext{
			Namespace: "default",
			Kind:      "Pod",
			Name:      "test-pod",
			NodeName: "node-1",
		},
		Attributes: map[string]interface{}{
			"container":    "main",
			"custom_field": "value",
		},
	}

	// Test standard fields
	assert.True(t, base.hasField(event, "cluster"))
	assert.True(t, base.hasField(event, "namespace"))
	assert.True(t, base.hasField(event, "pod"))
	assert.True(t, base.hasField(event, "container"))
	assert.True(t, base.hasField(event, "node"))
	assert.True(t, base.hasField(event, "severity"))

	// Test metadata field
	assert.True(t, base.hasField(event, "custom_field"))

	// Test missing field
	assert.False(t, base.hasField(event, "missing_field"))
}

func TestCorrelatorManager_Register(t *testing.T) {
	manager := NewCorrelatorManager()

	correlator1 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("Correlator1", "1.0.0", CorrelatorCapabilities{}),
	}
	correlator2 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("Correlator2", "1.0.0", CorrelatorCapabilities{}),
	}

	// Register first correlator
	err := manager.Register(correlator1)
	assert.NoError(t, err)

	// Register second correlator
	err = manager.Register(correlator2)
	assert.NoError(t, err)

	// Try to register duplicate
	err = manager.Register(correlator1)
	assert.Error(t, err)
}

func TestCorrelatorManager_Get(t *testing.T) {
	manager := NewCorrelatorManager()

	correlator := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("TestCorrelator", "1.0.0", CorrelatorCapabilities{}),
	}

	err := manager.Register(correlator)
	require.NoError(t, err)

	// Get existing correlator
	retrieved, exists := manager.Get("TestCorrelator")
	assert.True(t, exists)
	assert.Equal(t, correlator, retrieved)

	// Get non-existing correlator
	_, exists = manager.Get("NonExistent")
	assert.False(t, exists)
}

func TestCorrelatorManager_GetAll(t *testing.T) {
	manager := NewCorrelatorManager()

	correlator1 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("Correlator1", "1.0.0", CorrelatorCapabilities{}),
	}
	correlator2 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("Correlator2", "1.0.0", CorrelatorCapabilities{}),
	}

	err := manager.Register(correlator1)
	require.NoError(t, err)
	err = manager.Register(correlator2)
	require.NoError(t, err)

	all := manager.GetAll()
	assert.Len(t, all, 2)
}

func TestCorrelatorManager_GetForEvent(t *testing.T) {
	manager := NewCorrelatorManager()

	// Correlator that handles pod_crash
	correlator1 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("PodCorrelator", "1.0.0", CorrelatorCapabilities{
			EventTypes: []string{"pod_crash", "pod_restart"},
		}),
	}

	// Correlator that handles network events
	correlator2 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("NetworkCorrelator", "1.0.0", CorrelatorCapabilities{
			EventTypes: []string{"network_error", "connection_timeout"},
		}),
	}

	// Correlator that handles all events
	correlator3 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("UniversalCorrelator", "1.0.0", CorrelatorCapabilities{
			EventTypes: []string{"*"},
		}),
	}

	err := manager.Register(correlator1)
	require.NoError(t, err)
	err = manager.Register(correlator2)
	require.NoError(t, err)
	err = manager.Register(correlator3)
	require.NoError(t, err)

	// Test pod event
	podEvent := &domain.UnifiedEvent{
		Type: domain.EventType("pod_crash"),
	}
	correlators := manager.GetForEvent(podEvent)
	assert.Len(t, correlators, 2) // PodCorrelator and UniversalCorrelator

	// Test network event
	networkEvent := &domain.UnifiedEvent{
		Type: domain.EventType("network_error"),
	}
	correlators = manager.GetForEvent(networkEvent)
	assert.Len(t, correlators, 2) // NetworkCorrelator and UniversalCorrelator

	// Test unknown event
	unknownEvent := &domain.UnifiedEvent{
		Type: domain.EventType("unknown_event"),
	}
	correlators = manager.GetForEvent(unknownEvent)
	assert.Len(t, correlators, 1) // Only UniversalCorrelator
}

func TestCorrelatorManager_HealthCheck(t *testing.T) {
	manager := NewCorrelatorManager()

	// Healthy correlator
	correlator1 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("HealthyCorrelator", "1.0.0", CorrelatorCapabilities{}),
		healthFunc:     func(ctx context.Context) error { return nil },
	}

	// Unhealthy correlator
	correlator2 := &MockCorrelator{
		BaseCorrelator: NewBaseCorrelator("UnhealthyCorrelator", "1.0.0", CorrelatorCapabilities{}),
		healthFunc:     func(ctx context.Context) error { return errors.New("database connection failed") },
	}

	err := manager.Register(correlator1)
	require.NoError(t, err)
	err = manager.Register(correlator2)
	require.NoError(t, err)

	results := manager.HealthCheck(context.Background())
	assert.Len(t, results, 2)
	assert.NoError(t, results["HealthyCorrelator"])
	assert.Error(t, results["UnhealthyCorrelator"])
}

func TestCorrelatorError(t *testing.T) {
	// Test error without cause
	err1 := &CorrelatorError{
		Type:    ErrorTypeUnsupportedEvent,
		Message: "event type not supported",
	}
	assert.Equal(t, "event type not supported", err1.Error())

	// Test error with cause
	cause := errors.New("underlying error")
	err2 := &CorrelatorError{
		Type:    ErrorTypeDependencyFailed,
		Message: "database query failed",
		Cause:   cause,
	}
	assert.Equal(t, "database query failed: underlying error", err2.Error())
}
