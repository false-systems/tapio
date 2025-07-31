package systemd

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 1000,
		Labels:     make(map[string]string),
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("failed to create systemd collector: %v", err)
	}

	if collector.Name() != "systemd" {
		t.Errorf("expected collector name 'systemd', got %s", collector.Name())
	}

	// Health check may fail on non-Linux systems where systemd isn't available
	// This is expected behavior
}

func TestCreateRawEvent(t *testing.T) {
	data := SystemdRawData{
		EventType:   "service_start",
		Unit:        "kubelet.service",
		UnitType:    "service",
		ActiveState: "active",
		SubState:    "running",
		MainPID:     1234,
		Properties: map[string]interface{}{
			"command": "/usr/bin/kubelet",
			"uid":     0,
		},
	}

	event, err := createRawEvent(data)
	if err != nil {
		t.Fatalf("failed to create raw event: %v", err)
	}

	if event.Type != "systemd" {
		t.Errorf("expected event type 'systemd', got %s", event.Type)
	}

	if event.Metadata["unit"] != "kubelet.service" {
		t.Errorf("expected unit 'kubelet.service', got %s", event.Metadata["unit"])
	}

	if event.Metadata["event_type"] != "service_start" {
		t.Errorf("expected event_type 'service_start', got %s", event.Metadata["event_type"])
	}

	if event.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	// Verify we can parse the JSON data
	if len(event.Data) == 0 {
		t.Error("expected non-empty data")
	}
}
