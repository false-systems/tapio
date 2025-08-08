package correlation

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Event type constants for testing - mapping to domain.EventType
const (
	EventTypeSystemd = domain.EventTypeSystem
	EventTypeK8s     = domain.EventTypeKubernetes
	EventTypeEBPF    = domain.EventTypeNetwork
	EventTypeKubelet = domain.EventTypeMetric
)

// TestMockCorrelator for testify-based testing (used in engine_test.go)
type TestMockCorrelator struct {
	mock.Mock
}

func (m *TestMockCorrelator) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *TestMockCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	args := m.Called(ctx, event)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

// MockStorage for testing - implements full Storage interface
type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Store(ctx context.Context, result *CorrelationResult) error {
	args := m.Called(ctx, result)
	return args.Error(0)
}

func (m *MockStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

func (m *MockStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	args := m.Called(ctx, traceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

func (m *MockStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	args := m.Called(ctx, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

func (m *MockStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	args := m.Called(ctx, resourceType, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}

func (m *MockStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	args := m.Called(ctx, olderThan)
	return args.Error(0)
}

// Additional K8s domain types for testing
type K8sNamespace struct {
	Name   string
	Labels map[string]string
}
