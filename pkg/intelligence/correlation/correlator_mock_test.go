package correlation

import (
	"context"

	"github.com/stretchr/testify/mock"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestifyMockCorrelator is a mock implementation of Correlator using testify/mock
type TestifyMockCorrelator struct {
	mock.Mock
}

// Name returns the correlator name
func (m *TestifyMockCorrelator) Name() string {
	args := m.Called()
	return args.String(0)
}

// Process processes an event and returns correlation results
func (m *TestifyMockCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	args := m.Called(ctx, event)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CorrelationResult), args.Error(1)
}
