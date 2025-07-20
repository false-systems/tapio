package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventProcessorCreation(t *testing.T) {
	processor := newEventProcessor()
	if processor == nil {
		t.Fatal("Expected processor to be created")
	}
}

func TestProcessServiceStartEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:      core.EventTypeStart,
		UnitName:  "nginx.service",
		UnitType:  "service",
		OldState:  core.StateInactive,
		NewState:  core.StateActive,
		SubState:  "running",
		Result:    "success",
		Timestamp: time.Now(),
		MainPID:   1234,
		Properties: map[string]interface{}{
			"CPUUsageNSec":  uint64(1000000),
			"MemoryCurrent": uint64(10485760),
		},
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process service start event: %v", err)
	}

	// Verify basic event properties
	if event.Type != domain.EventTypeService {
		t.Errorf("Expected event type %s, got %s", domain.EventTypeService, event.Type)
	}

	if event.Source != domain.SourceSystemd {
		t.Errorf("Expected source %s, got %s", domain.SourceSystemd, event.Source)
	}

	// Verify severity for successful start
	if event.Severity != domain.EventSeverityLow {
		t.Errorf("Expected severity %s, got %s", domain.EventSeverityLow, event.Severity)
	}

	// Verify context
	if event.Context.Service != "systemd" {
		t.Errorf("Expected service systemd, got %s", event.Context.Service)
	}

	if event.Context.Component != "nginx.service" {
		t.Errorf("Expected component nginx.service, got %s", event.Context.Component)
	}

	if event.Context.PID != 1234 {
		t.Errorf("Expected PID 1234, got %d", event.Context.PID)
	}
}

func TestProcessServiceFailureEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:       core.EventTypeFailure,
		UnitName:   "critical-app.service",
		UnitType:   "service",
		OldState:   core.StateActivating,
		NewState:   core.StateFailed,
		SubState:   "failed",
		Result:     "exit-code",
		ExitCode:   1,
		ExitStatus: 1,
		Timestamp:  time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process service failure event: %v", err)
	}

	// Verify severity for failure
	if event.Severity != domain.EventSeverityHigh {
		t.Errorf("Expected severity %s, got %s", domain.EventSeverityHigh, event.Severity)
	}

	// Verify data contains exit code
	if exitCode, ok := event.Data["exit_code"].(int); !ok || exitCode != 1 {
		t.Errorf("Expected exit_code 1, got %v", event.Data["exit_code"])
	}
}

func TestProcessCriticalServiceFailure(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:      core.EventTypeFailure,
		UnitName:  "sshd.service",
		UnitType:  "service",
		OldState:  core.StateActive,
		NewState:  core.StateFailed,
		SubState:  "failed",
		Result:    "signal",
		Timestamp: time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process critical service failure: %v", err)
	}

	// Verify severity for critical service failure
	if event.Severity != domain.EventSeverityCritical {
		t.Errorf("Expected severity %s, got %s", domain.EventSeverityCritical, event.Severity)
	}
}

func TestDetermineSeverity(t *testing.T) {
	processor := newEventProcessor()

	tests := []struct {
		name     string
		raw      core.RawEvent
		expected domain.EventSeverity
	}{
		{
			name: "normal start",
			raw: core.RawEvent{
				Type:     core.EventTypeStart,
				NewState: core.StateActive,
			},
			expected: domain.EventSeverityLow,
		},
		{
			name: "service failure",
			raw: core.RawEvent{
				Type:     core.EventTypeFailure,
				NewState: core.StateFailed,
			},
			expected: domain.EventSeverityHigh,
		},
		{
			name: "critical service failure",
			raw: core.RawEvent{
				Type:     core.EventTypeFailure,
				UnitName: "kubelet.service",
				NewState: core.StateFailed,
			},
			expected: domain.EventSeverityCritical,
		},
		{
			name: "restart event",
			raw: core.RawEvent{
				Type: core.EventTypeRestart,
			},
			expected: domain.EventSeverityWarning,
		},
		{
			name: "state change to inactive",
			raw: core.RawEvent{
				Type:     core.EventTypeStateChange,
				OldState: core.StateActive,
				NewState: core.StateInactive,
			},
			expected: domain.EventSeverityWarning,
		},
		{
			name: "non-zero exit code",
			raw: core.RawEvent{
				Type:     core.EventTypeStop,
				ExitCode: 1,
			},
			expected: domain.EventSeverityWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := processor.(*eventProcessor).determineSeverity(tt.raw)
			if severity != tt.expected {
				t.Errorf("Expected severity %s, got %s", tt.expected, severity)
			}
		})
	}
}

func TestIsCriticalService(t *testing.T) {
	processor := newEventProcessor()

	tests := []struct {
		service  string
		critical bool
	}{
		{"sshd.service", true},
		{"systemd-networkd.service", true},
		{"systemd-resolved.service", true},
		{"dbus.service", true},
		{"systemd-journald.service", true},
		{"kubelet.service", true},
		{"docker.service", true},
		{"containerd.service", true},
		{"nginx.service", false},
		{"custom-app.service", false},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			result := processor.(*eventProcessor).isCriticalService(tt.service)
			if result != tt.critical {
				t.Errorf("Expected isCriticalService(%s) = %v, got %v", tt.service, tt.critical, result)
			}
		})
	}
}

func TestProcessRestartEvent(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:      core.EventTypeRestart,
		UnitName:  "app.service",
		UnitType:  "service",
		OldState:  core.StateActive,
		NewState:  core.StateActive,
		SubState:  "running",
		Result:    "success",
		Timestamp: time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process restart event: %v", err)
	}

	// Verify event type mapping
	if eventType, ok := event.Data["event_type"].(string); !ok || eventType != "restart" {
		t.Errorf("Expected event_type 'restart', got %v", event.Data["event_type"])
	}

	// Verify severity for restart
	if event.Severity != domain.EventSeverityWarning {
		t.Errorf("Expected severity %s, got %s", domain.EventSeverityWarning, event.Severity)
	}
}

func TestEventDataEnrichment(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:       core.EventTypeStateChange,
		UnitName:   "test.service",
		UnitType:   "service",
		OldState:   core.StateActive,
		NewState:   core.StateInactive,
		SubState:   "dead",
		Result:     "success",
		MainPID:    5678,
		ExitCode:   0,
		ExitStatus: 0,
		Timestamp:  time.Now(),
		Properties: map[string]interface{}{
			"Description":   "Test Service",
			"LoadState":     "loaded",
			"ActiveState":   "inactive",
			"SubState":      "dead",
			"CPUUsageNSec":  uint64(5000000),
			"MemoryCurrent": uint64(20971520),
		},
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process event: %v", err)
	}

	// Verify properties are included
	properties, ok := event.Data["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected properties in event data")
	}

	if desc, ok := properties["Description"].(string); !ok || desc != "Test Service" {
		t.Errorf("Expected Description 'Test Service', got %v", properties["Description"])
	}

	// Verify PID is included
	if pid, ok := event.Data["main_pid"].(int); !ok || pid != 5678 {
		t.Errorf("Expected main_pid 5678, got %v", event.Data["main_pid"])
	}

	// Verify attributes
	if unitName, ok := event.Attributes["unit_name"].(string); !ok || unitName != "test.service" {
		t.Errorf("Expected unit_name attribute 'test.service', got %v", event.Attributes["unit_name"])
	}
}

func TestEventContextLabels(t *testing.T) {
	processor := newEventProcessor()
	ctx := context.Background()

	raw := core.RawEvent{
		Type:      core.EventTypeFailure,
		UnitName:  "failed.service",
		UnitType:  "service",
		OldState:  core.StateActive,
		NewState:  core.StateFailed,
		SubState:  "failed",
		Result:    "exit-code",
		Timestamp: time.Now(),
	}

	event, err := processor.ProcessEvent(ctx, raw)
	if err != nil {
		t.Fatalf("Failed to process event: %v", err)
	}

	// Verify labels
	if event.Context.Labels["unit_name"] != "failed.service" {
		t.Errorf("Expected label unit_name='failed.service', got %s", event.Context.Labels["unit_name"])
	}

	if event.Context.Labels["state"] != core.StateFailed {
		t.Errorf("Expected label state='%s', got %s", core.StateFailed, event.Context.Labels["state"])
	}

	if event.Context.Labels["result"] != "exit-code" {
		t.Errorf("Expected label result='exit-code', got %s", event.Context.Labels["result"])
	}

	// Verify metadata tags
	metadata := event.Context.Metadata
	tags, ok := metadata["tags"].([]string)
	if !ok {
		t.Fatal("Expected tags in metadata")
	}

	expectedTags := map[string]bool{
		"systemd": true,
		"service": true,
		"failed":  true,
		"failure": true,
	}

	for _, tag := range tags {
		if !expectedTags[tag] {
			t.Errorf("Unexpected tag: %s", tag)
		}
		delete(expectedTags, tag)
	}

	if len(expectedTags) > 0 {
		t.Errorf("Missing expected tags: %v", expectedTags)
	}
}
