package correlation

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockStorage is a mock implementation of the Storage interface for testing
type MockStorage struct {
	mock.Mock
}

// Store saves a correlation result
func (m *MockStorage) Store(ctx context.Context, result *CorrelationResult) error {
	args := m.Called(ctx, result)
	return args.Error(0)
}

// GetRecent retrieves recent correlations
func (m *MockStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

// GetByTraceID retrieves correlations for a specific trace
func (m *MockStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	args := m.Called(ctx, traceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

// GetByTimeRange retrieves correlations within a time range
func (m *MockStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	args := m.Called(ctx, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

// GetByResource retrieves correlations affecting a specific resource
func (m *MockStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	args := m.Called(ctx, resourceType, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

// Cleanup removes old correlations
func (m *MockStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	args := m.Called(ctx, olderThan)
	return args.Error(0)
}

// HealthCheck checks the storage health
func (m *MockStorage) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
