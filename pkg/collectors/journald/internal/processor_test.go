package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventProcessor_ProcessEntry(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	tests := []struct {
		name    string
		entry   *core.LogEntry
		wantErr bool
	}{
		{
			name:    "nil entry",
			entry:   nil,
			wantErr: true,
		},
		{
			name: "valid entry",
			entry: &core.LogEntry{
				Message:       "Test message",
				Priority:      core.PriorityInfo,
				Facility:      "syslog",
				Identifier:    "test-app",
				PID:           1234,
				UID:           1000,
				GID:           1000,
				Comm:          "test-command",
				Unit:          "test.service",
				Timestamp:     time.Now(),
				BootID:        "test-boot-id",
				MachineID:     "test-machine-id",
				Cursor:        "test-cursor",
				MonotonicTime: 123456789,
				Fields: map[string]interface{}{
					"CUSTOM_FIELD": "custom_value",
				},
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: &core.LogEntry{
				Message:   "Minimal message",
				Priority:  core.PriorityError,
				Timestamp: time.Now(),
				Cursor:    "minimal-cursor",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := processor.ProcessEntry(ctx, tt.entry)

			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessEntry() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify event structure
			if event.Type != domain.EventTypeLog {
				t.Errorf("Expected event type %v, got %v", domain.EventTypeLog, event.Type)
			}

			if event.Source != domain.SourceJournald {
				t.Errorf("Expected event source %v, got %v", domain.SourceJournald, event.Source)
			}

			if event.Confidence != 1.0 {
				t.Errorf("Expected confidence 1.0, got %v", event.Confidence)
			}

			// Verify payload
			payload, ok := event.Payload.(domain.LogEventPayload)
			if !ok {
				t.Errorf("Expected LogEventPayload, got %T", event.Payload)
				return
			}

			if payload.Message != tt.entry.Message {
				t.Errorf("Expected message %q, got %q", tt.entry.Message, payload.Message)
			}

			if payload.Priority != int32(tt.entry.Priority) {
				t.Errorf("Expected priority %d, got %d", int32(tt.entry.Priority), payload.Priority)
			}
		})
	}
}

func TestDetermineSeverity(t *testing.T) {
	processor := &eventProcessor{}

	tests := []struct {
		priority core.Priority
		expected domain.Severity
	}{
		{core.PriorityEmergency, domain.SeverityCritical},
		{core.PriorityAlert, domain.SeverityCritical},
		{core.PriorityCritical, domain.SeverityCritical},
		{core.PriorityError, domain.SeverityError},
		{core.PriorityWarning, domain.SeverityWarn},
		{core.PriorityNotice, domain.SeverityInfo},
		{core.PriorityInfo, domain.SeverityInfo},
		{core.PriorityDebug, domain.SeverityDebug},
		{core.Priority(99), domain.SeverityInfo}, // Unknown priority defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.priority.String(), func(t *testing.T) {
			severity := processor.determineSeverity(tt.priority)
			if severity != tt.expected {
				t.Errorf("Expected severity %v for priority %v, got %v",
					tt.expected, tt.priority, severity)
			}
		})
	}
}

func TestCreateLogPayload(t *testing.T) {
	processor := &eventProcessor{}

	entry := &core.LogEntry{
		Message:    "Test message",
		Priority:   core.PriorityWarning,
		Facility:   "kern",
		Identifier: "kernel",
		Unit:       "systemd-test.service",
		Comm:       "systemd",
		Exe:        "/usr/lib/systemd/systemd",
		Cmdline:    "/usr/lib/systemd/systemd --system",
		HostName:   "test-host",
		Session:    "1",
		UserUnit:   "test-user.service",
		BootID:     "boot-123",
		MachineID:  "machine-456",
		Cursor:     "cursor-789",
		Fields: map[string]interface{}{
			"CUSTOM_STRING": "value",
			"CUSTOM_INT":    42,
		},
	}

	payload := processor.createLogPayload(entry)

	// Test basic fields
	if payload.Message != entry.Message {
		t.Errorf("Expected message %q, got %q", entry.Message, payload.Message)
	}

	if payload.Unit != entry.Unit {
		t.Errorf("Expected unit %q, got %q", entry.Unit, payload.Unit)
	}

	if payload.Priority != int32(entry.Priority) {
		t.Errorf("Expected priority %d, got %d", int32(entry.Priority), payload.Priority)
	}

	// Test that fields are properly mapped
	expectedFields := map[string]string{
		"_COMM":              entry.Comm,
		"_EXE":               entry.Exe,
		"_CMDLINE":           entry.Cmdline,
		"_HOSTNAME":          entry.HostName,
		"_SYSTEMD_SESSION":   entry.Session,
		"_SYSTEMD_USER_UNIT": entry.UserUnit,
		"_BOOT_ID":           entry.BootID,
		"_MACHINE_ID":        entry.MachineID,
		"__CURSOR":           entry.Cursor,
		"CUSTOM_STRING":      "value",
		"CUSTOM_INT":         "42",
	}

	for key, expectedValue := range expectedFields {
		if actualValue, exists := payload.Fields[key]; !exists {
			t.Errorf("Expected field %q to exist in payload", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected field %q to have value %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestGenerateHash(t *testing.T) {
	processor := &eventProcessor{}

	tests := []struct {
		name  string
		entry *core.LogEntry
	}{
		{
			name: "entry with cursor",
			entry: &core.LogEntry{
				Cursor:  "test-cursor-123",
				Message: "Some message",
			},
		},
		{
			name: "entry without cursor",
			entry: &core.LogEntry{
				Message:   "Some message",
				Unit:      "test.service",
				Timestamp: time.Unix(1234567890, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := processor.generateHash(tt.entry)
			hash2 := processor.generateHash(tt.entry)

			// Hash should be deterministic
			if hash1 != hash2 {
				t.Errorf("Hash should be deterministic, got %q and %q", hash1, hash2)
			}

			// Hash should not be empty
			if hash1 == "" {
				t.Error("Hash should not be empty")
			}

			// If cursor is present, hash should equal cursor
			if tt.entry.Cursor != "" && hash1 != tt.entry.Cursor {
				t.Errorf("Expected hash to equal cursor %q, got %q", tt.entry.Cursor, hash1)
			}
		})
	}
}
