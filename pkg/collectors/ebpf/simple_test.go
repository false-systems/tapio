package ebpf

import (
	"testing"
	"time"
)

func TestEventTypeStringSimple(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypeSyscall, "syscall"},
		{EventTypeNetwork, "network"},
		{EventTypeProcess, "process"},
		{EventTypeFile, "file"},
		{EventTypeSecurity, "security"},
		{EventTypeUnknown, "unknown"},
	}

	for _, test := range tests {
		if got := test.eventType.String(); got != test.expected {
			t.Errorf("EventType.String() = %v, want %v", got, test.expected)
		}
	}
}

func TestFilterEngineCreation(t *testing.T) {
	config := &FilterConfig{
		EnableRawFiltering:   true,
		EventTypeBlacklist:   []EventType{EventTypeSyscall},
		ProcessBlacklist:     []string{"systemd"},
		EnableSemanticFilter: true,
		MinImportanceScore:   0.5,
	}

	filterEngine := NewFilterEngine(config)
	if filterEngine == nil {
		t.Error("Expected filter engine to be created")
	}

	// Test raw event filtering
	syscallEvent := &RawEvent{
		Type: EventTypeSyscall,
		Comm: "test",
	}

	if filterEngine.ProcessRawEvent(syscallEvent) {
		t.Error("Expected syscall event to be filtered out")
	}

	allowedEvent := &RawEvent{
		Type: EventTypeNetwork,
		Comm: "curl",
	}

	if !filterEngine.ProcessRawEvent(allowedEvent) {
		t.Error("Expected network event to be allowed")
	}
}

func TestRawEventFormatterCreation(t *testing.T) {
	formatter := NewRawEventFormatter(&RawEventFormatterOptions{
		IncludeTimestamp: false,
		ColorOutput:      false,
		VerboseMode:      false,
	})

	if formatter == nil {
		t.Error("Expected formatter to be created")
	}

	// Test basic network event formatting
	networkEvent := &RawEvent{
		Type: EventTypeNetwork,
		PID:  1234,
		Comm: "curl",
		Details: &NetworkEvent{
			SubType:    NetworkEventConnect,
			Protocol:   6,
			SourceIP:   "192.168.1.100",
			DestIP:     "10.0.0.1",
			SourcePort: 45678,
			DestPort:   80,
			Direction:  "egress",
		},
	}

	formatted := formatter.FormatEvent(networkEvent)

	if len(formatted) == 0 {
		t.Error("Expected non-empty formatted output")
	}

	// Basic checks for key elements
	if !stringContains(formatted, "curl[1234]") {
		t.Errorf("Expected formatted output to contain 'curl[1234]', got: %s", formatted)
	}
}

func TestEventEnumStrings(t *testing.T) {
	// Test NetworkEventType strings
	if NetworkEventConnect.String() != "connect" {
		t.Errorf("Expected 'connect', got %v", NetworkEventConnect.String())
	}

	// Test ProcessEventType strings
	if ProcessEventExec.String() != "exec" {
		t.Errorf("Expected 'exec', got %v", ProcessEventExec.String())
	}

	// Test FileEventType strings
	if FileEventOpen.String() != "open" {
		t.Errorf("Expected 'open', got %v", FileEventOpen.String())
	}
}

func TestEnrichedEventBasics(t *testing.T) {
	rawEvent := &RawEvent{
		Type:      EventTypeNetwork,
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		Comm:      "curl",
	}

	enriched := &EnrichedEvent{
		Raw:         rawEvent,
		EventID:     "test-event-123",
		Timestamp:   time.Now(),
		Hostname:    "test-host",
		ServiceName: "test-service",
		Importance:  0.8,
		Interesting: true,
		Tags:        []string{"network", "outbound"},
	}

	// Test basic functionality
	if enriched.EventID != "test-event-123" {
		t.Errorf("Expected EventID 'test-event-123', got %v", enriched.EventID)
	}

	if enriched.Importance != 0.8 {
		t.Errorf("Expected Importance 0.8, got %v", enriched.Importance)
	}

	if !enriched.Interesting {
		t.Error("Expected event to be interesting")
	}

	if len(enriched.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(enriched.Tags))
	}
}

func TestEventFilterBasics(t *testing.T) {
	filter := &EventFilter{
		EventTypes:    []EventType{EventTypeNetwork, EventTypeProcess},
		PIDs:          []uint32{1234, 5678},
		MinImportance: 0.5,
		IncludeRaw:    true,
		SamplingRate:  0.1,
	}

	if len(filter.EventTypes) != 2 {
		t.Errorf("Expected 2 event types, got %d", len(filter.EventTypes))
	}

	if filter.MinImportance != 0.5 {
		t.Errorf("Expected MinImportance 0.5, got %v", filter.MinImportance)
	}

	if !filter.IncludeRaw {
		t.Error("Expected IncludeRaw to be true")
	}
}

// Helper function to check if string contains substring
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
