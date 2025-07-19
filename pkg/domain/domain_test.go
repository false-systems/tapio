package domain

import (
	"testing"
	"time"
)

func TestEventCreation(t *testing.T) {
	event := Event{
		ID:        "test-event-1",
		Timestamp: time.Now(),
		Type:      "system",
		Source:    "ebpf",
		Data:      map[string]interface{}{"test": "data"},
	}

	if event.ID == "" {
		t.Error("Event ID should not be empty")
	}

	if event.Type != "system" {
		t.Errorf("Expected type 'system', got %s", event.Type)
	}

	if event.Source != "ebpf" {
		t.Errorf("Expected source 'ebpf', got %s", event.Source)
	}
}

func TestTimeWindow(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Hour)

	window := TimeWindow{
		Start:    start,
		End:      end,
		Duration: time.Hour,
	}

	if window.Duration != time.Hour {
		t.Errorf("Expected duration 1h, got %v", window.Duration)
	}
}

func TestFindingCreation(t *testing.T) {
	finding := Finding{
		ID:          "test-finding-1",
		Type:        "anomaly",
		Confidence:  0.85,
		Description: "Test finding",
		Events:      []string{"event-1", "event-2"},
		Timestamp:   time.Now(),
	}

	if finding.ID == "" {
		t.Error("Finding ID should not be empty")
	}

	if finding.Confidence <= 0 || finding.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", finding.Confidence)
	}

	if len(finding.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(finding.Events))
	}
}

func TestSeverityLevels(t *testing.T) {
	severities := []SeverityLevel{
		SeverityLow,
		SeverityMedium,
		SeverityHigh,
		SeverityCritical,
		SeverityWarning,
	}

	if len(severities) != 5 {
		t.Errorf("Expected 5 severity levels, got %d", len(severities))
	}

	if SeverityHigh != "high" {
		t.Errorf("Expected SeverityHigh to be 'high', got %s", SeverityHigh)
	}
}

func TestEventTypes(t *testing.T) {
	eventTypes := []EventType{
		EventTypeSystem,
		EventTypeKubernetes,
		EventTypeService,
		EventTypeLog,
		EventTypeNetwork,
		EventTypeProcess,
		EventTypeMemory,
		EventTypeCPU,
		EventTypeDisk,
	}

	if len(eventTypes) != 9 {
		t.Errorf("Expected 9 event types, got %d", len(eventTypes))
	}

	if EventTypeSystem != "system" {
		t.Errorf("Expected EventTypeSystem to be 'system', got %s", EventTypeSystem)
	}
}

func TestSourceTypes(t *testing.T) {
	sources := []SourceType{
		SourceEBPF,
		SourceK8s,
		SourceSystemd,
		SourceJournald,
		SourceCNI,
		SourceCustom,
	}

	if len(sources) != 6 {
		t.Errorf("Expected 6 source types, got %d", len(sources))
	}

	if SourceEBPF != "ebpf" {
		t.Errorf("Expected SourceEBPF to be 'ebpf', got %s", SourceEBPF)
	}
}
