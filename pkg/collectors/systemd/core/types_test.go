package core

import (
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		check  func(*Config) bool
	}{
		{
			name: "empty buffer size gets default",
			config: Config{
				EventBufferSize: 0,
			},
			check: func(c *Config) bool {
				return c.EventBufferSize == 1000
			},
		},
		{
			name: "empty poll interval gets default",
			config: Config{
				PollInterval: 0,
			},
			check: func(c *Config) bool {
				return c.PollInterval == 5*time.Second
			},
		},
		{
			name: "existing values preserved",
			config: Config{
				EventBufferSize: 500,
				PollInterval:    10 * time.Second,
			},
			check: func(c *Config) bool {
				return c.EventBufferSize == 500 && c.PollInterval == 10*time.Second
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			if err := config.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !tt.check(&config) {
				t.Errorf("Validation did not set expected defaults")
			}
		})
	}
}

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypeStart, "start"},
		{EventTypeStop, "stop"},
		{EventTypeRestart, "restart"},
		{EventTypeReload, "reload"},
		{EventTypeFailure, "failure"},
		{EventTypeStateChange, "state_change"},
		{EventType("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if got := string(tt.eventType); got != tt.expected {
				t.Errorf("EventType string = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		health   Health
		expected HealthStatus
	}{
		{
			name: "healthy status",
			health: Health{
				Status:          HealthStatusHealthy,
				DBusConnected:   true,
				EventsProcessed: 100,
				ErrorCount:      0,
			},
			expected: HealthStatusHealthy,
		},
		{
			name: "degraded status",
			health: Health{
				Status:          HealthStatusDegraded,
				DBusConnected:   true,
				EventsProcessed: 100,
				ErrorCount:      5,
			},
			expected: HealthStatusDegraded,
		},
		{
			name: "unhealthy status",
			health: Health{
				Status:        HealthStatusUnhealthy,
				DBusConnected: false,
				ErrorCount:    10,
			},
			expected: HealthStatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.health.Status != tt.expected {
				t.Errorf("Health.Status = %v, want %v", tt.health.Status, tt.expected)
			}
		})
	}
}

func TestRawEventFields(t *testing.T) {
	now := time.Now()
	raw := RawEvent{
		Type:       EventTypeStart,
		UnitName:   "test.service",
		UnitType:   "service",
		OldState:   StateInactive,
		NewState:   StateActive,
		SubState:   "running",
		Result:     "success",
		MainPID:    1234,
		ExitCode:   0,
		ExitStatus: 0,
		Timestamp:  now,
		Properties: map[string]interface{}{
			"Description": "Test Service",
			"LoadState":   "loaded",
		},
	}

	// Verify all fields are accessible
	if raw.Type != EventTypeStart {
		t.Errorf("Type = %v, want %v", raw.Type, EventTypeStart)
	}
	if raw.UnitName != "test.service" {
		t.Errorf("UnitName = %v, want test.service", raw.UnitName)
	}
	if raw.UnitType != "service" {
		t.Errorf("UnitType = %v, want service", raw.UnitType)
	}
	if raw.OldState != StateInactive {
		t.Errorf("OldState = %v, want %v", raw.OldState, StateInactive)
	}
	if raw.NewState != StateActive {
		t.Errorf("NewState = %v, want %v", raw.NewState, StateActive)
	}
	if raw.SubState != "running" {
		t.Errorf("SubState = %v, want running", raw.SubState)
	}
	if raw.Result != "success" {
		t.Errorf("Result = %v, want success", raw.Result)
	}
	if raw.MainPID != 1234 {
		t.Errorf("MainPID = %v, want 1234", raw.MainPID)
	}
	if !raw.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", raw.Timestamp, now)
	}
	if len(raw.Properties) != 2 {
		t.Errorf("Properties length = %v, want 2", len(raw.Properties))
	}
}

func TestStatisticsUpdate(t *testing.T) {
	stats := Statistics{
		StartTime:         time.Now(),
		EventsCollected:   0,
		EventsDropped:     0,
		ServicesMonitored: 0,
		ReconnectCount:    0,
		DBusCallsTotal:    0,
		DBusErrors:        0,
	}

	// Simulate updates
	stats.EventsCollected++
	stats.ServicesMonitored = 5
	stats.DBusCallsTotal = 10

	if stats.EventsCollected != 1 {
		t.Errorf("EventsCollected = %v, want 1", stats.EventsCollected)
	}
	if stats.ServicesMonitored != 5 {
		t.Errorf("ServicesMonitored = %v, want 5", stats.ServicesMonitored)
	}
	if stats.DBusCallsTotal != 10 {
		t.Errorf("DBusCallsTotal = %v, want 10", stats.DBusCallsTotal)
	}
}

func TestServiceStates(t *testing.T) {
	// Test all defined states
	states := []string{
		StateActive,
		StateInactive,
		StateActivating,
		StateDeactivating,
		StateFailed,
	}

	// Verify states are unique
	seen := make(map[string]bool)
	for _, state := range states {
		if seen[state] {
			t.Errorf("Duplicate state: %s", state)
		}
		seen[state] = true
	}

	// Verify state values
	if StateActive != "active" {
		t.Errorf("StateActive = %v, want active", StateActive)
	}
	if StateFailed != "failed" {
		t.Errorf("StateFailed = %v, want failed", StateFailed)
	}
	if StateInactive != "inactive" {
		t.Errorf("StateInactive = %v, want inactive", StateInactive)
	}
}
