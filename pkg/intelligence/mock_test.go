package intelligence

import (
	"context"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// mockService implements Service for testing.
// Captures all emitted events for assertions.
type mockService struct {
	mu       sync.Mutex
	events   []*domain.ObserverEvent
	emitErr  error   // If set, Emit returns this error
	name     string  // Service name (default: "mock")
	critical bool    // Whether service is critical
	closed   bool
}

// newMockService creates a new mock service for testing.
func newMockService() *mockService {
	return &mockService{
		name:   "mock",
		events: make([]*domain.ObserverEvent, 0),
	}
}

// Emit captures the event for later assertions.
func (m *mockService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.emitErr != nil {
		return m.emitErr
	}

	// Deep copy event to avoid mutation issues
	eventCopy := *event
	m.events = append(m.events, &eventCopy)
	return nil
}

// Name returns the mock service name.
func (m *mockService) Name() string {
	if m.name == "" {
		return "mock"
	}
	return m.name
}

// IsCritical returns whether this mock is critical.
func (m *mockService) IsCritical() bool {
	return m.critical
}

// Close marks the mock as closed.
func (m *mockService) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// eventCount returns the number of captured events.
func (m *mockService) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}
