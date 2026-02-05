package domain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEventEmitter_InterfaceCompliance verifies EventEmitter extends Emitter.
func TestEventEmitter_InterfaceCompliance(t *testing.T) {
	// EventEmitter should embed Emitter and add Name, IsCritical, Close
	var _ EventEmitter = &mockEventEmitter{}
}

// mockEventEmitter is a test double for EventEmitter interface verification.
type mockEventEmitter struct {
	closed bool
}

func (m *mockEventEmitter) Emit(ctx context.Context, event *ObserverEvent) error {
	return nil
}

func (m *mockEventEmitter) Name() string {
	return "mock-emitter"
}

func (m *mockEventEmitter) IsCritical() bool {
	return false
}

func (m *mockEventEmitter) Close() error {
	m.closed = true
	return nil
}

// TestEventEmitter_MockImplementation verifies mock behavior.
func TestEventEmitter_MockImplementation(t *testing.T) {
	mock := &mockEventEmitter{}

	assert.Equal(t, "mock-emitter", mock.Name())
	assert.False(t, mock.IsCritical())
	assert.False(t, mock.closed)

	err := mock.Close()
	assert.NoError(t, err)
	assert.True(t, mock.closed)
}
