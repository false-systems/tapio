package correlation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

// MockCorrelator implements the Correlator interface for testing
type MockCorrelator struct {
	name        string
	processFunc func(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error)
}

func (m *MockCorrelator) Name() string {
	return m.name
}

func (m *MockCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, event)
	}
	return nil, nil
}

func TestCorrelatorInterface(t *testing.T) {
	// Test that MockCorrelator implements Correlator
	var _ Correlator = &MockCorrelator{}

	mock := &MockCorrelator{
		name: "test-correlator",
		processFunc: func(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
			return []*CorrelationResult{{
				ID:         "test-correlation",
				Type:       "test",
				Confidence: 0.9,
				Summary:    "Test correlation",
			}}, nil
		},
	}


	assert.Equal(t, "test-correlator", mock.Name())

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
			NodeName:  "node-1",
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

	results, err := mock.Process(context.Background(), &domain.UnifiedEvent{})
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "test-correlation", results[0].ID)
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
