package intelligence

import (
	"context"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Mock implements Service for testing.
// Captures all emitted events for assertions.
// Exported so other packages can use it in tests.
type Mock struct {
	mu       sync.Mutex
	events   []*domain.ObserverEvent
	emitErr  error  // If set, Emit returns this error
	name     string // Service name (default: "mock")
	critical bool   // Whether service is critical
	closed   bool
}

// NewMock creates a new mock service for testing.
func NewMock() *Mock {
	return &Mock{
		name:   "mock",
		events: make([]*domain.ObserverEvent, 0),
	}
}

// Emit captures the event for later assertions.
func (m *Mock) Emit(ctx context.Context, event *domain.ObserverEvent) error {
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
func (m *Mock) Name() string {
	if m.name == "" {
		return "mock"
	}
	return m.name
}

// IsCritical returns whether this mock is critical.
func (m *Mock) IsCritical() bool {
	return m.critical
}

// Close marks the mock as closed.
func (m *Mock) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// Events returns all captured events.
func (m *Mock) Events() []*domain.ObserverEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return a copy to avoid race conditions
	result := make([]*domain.ObserverEvent, len(m.events))
	copy(result, m.events)
	return result
}

// EventCount returns the number of captured events.
func (m *Mock) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

// LastEvent returns the most recent event, or nil if none.
func (m *Mock) LastEvent() *domain.ObserverEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.events) == 0 {
		return nil
	}
	return m.events[len(m.events)-1]
}

// Reset clears all captured events.
func (m *Mock) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = make([]*domain.ObserverEvent, 0)
}

// SetEmitError sets the error that Emit will return.
func (m *Mock) SetEmitError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitErr = err
}

// SetCritical sets whether the mock is critical.
func (m *Mock) SetCritical(critical bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.critical = critical
}

// SetName sets the mock service name.
func (m *Mock) SetName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.name = name
}

// IsClosed returns whether Close was called.
func (m *Mock) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}
