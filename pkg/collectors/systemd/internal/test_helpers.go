package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// mockEventProcessor is a mock implementation of core.EventProcessor for testing
type mockEventProcessor struct {
	processFunc func(ctx context.Context, raw core.RawEvent) (domain.Event, error)
	events      []domain.Event
	errors      []error
}

func newMockEventProcessor() *mockEventProcessor {
	return &mockEventProcessor{
		events: make([]domain.Event, 0),
		errors: make([]error, 0),
	}
}

func (m *mockEventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.Event, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, raw)
	}

	// Default implementation
	event := domain.Event{
		ID:         domain.EventID("test-event"),
		Type:       domain.EventTypeService,
		Source:     domain.SourceSystemd,
		Timestamp:  raw.Timestamp,
		Data:       map[string]interface{}{"unit": raw.UnitName},
		Context:    domain.EventContext{Service: "systemd"},
		Severity:   domain.EventSeverityLow,
		Confidence: 1.0,
	}

	m.events = append(m.events, event)
	return event, nil
}

// createTestRawEvent creates a test raw event with sensible defaults
func createTestRawEvent(eventType core.EventType, unitName string) core.RawEvent {
	return core.RawEvent{
		Type:      eventType,
		UnitName:  unitName,
		UnitType:  "service",
		OldState:  core.StateInactive,
		NewState:  core.StateActive,
		SubState:  "running",
		Result:    "success",
		Timestamp: time.Now(),
		Properties: map[string]interface{}{
			"LoadState": "loaded",
		},
	}
}

// createFailedServiceEvent creates a test event for a failed service
func createFailedServiceEvent(unitName string) core.RawEvent {
	return core.RawEvent{
		Type:       core.EventTypeFailure,
		UnitName:   unitName,
		UnitType:   "service",
		OldState:   core.StateActive,
		NewState:   core.StateFailed,
		SubState:   "failed",
		Result:     "exit-code",
		ExitCode:   1,
		ExitStatus: 1,
		Timestamp:  time.Now(),
	}
}

// mockDBusConnection is a mock implementation for testing
type mockDBusConnection struct {
	connected bool
	units     []string
	errors    chan error
}

func newMockDBusConnection() *mockDBusConnection {
	return &mockDBusConnection{
		connected: true,
		units: []string{
			"sshd.service",
			"nginx.service",
			"docker.service",
		},
		errors: make(chan error),
	}
}

func (m *mockDBusConnection) Connect() error {
	m.connected = true
	return nil
}

func (m *mockDBusConnection) Close() error {
	m.connected = false
	return nil
}

func (m *mockDBusConnection) IsConnected() bool {
	return m.connected
}

func (m *mockDBusConnection) ListUnits() ([]string, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected")
	}
	return m.units, nil
}

// Add more mock methods as needed

// testConfig creates a test configuration
func testConfig() core.Config {
	return core.Config{
		Name:            "test-collector",
		Enabled:         true,
		EventBufferSize: 10,
		ServiceFilter:   []string{"test.service"},
		PollInterval:    time.Second,
	}
}
