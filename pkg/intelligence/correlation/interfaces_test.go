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
