package ebpf

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventTypeString(t *testing.T) {
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

func TestEnrichedEventToDomainEvent(t *testing.T) {
	// Create a raw network event
	rawEvent := &RawEvent{
		Type:      EventTypeNetwork,
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       1234,
		UID:       1000,
		GID:       1000,
		Comm:      "curl",
		Details: &NetworkEvent{
			SubType:    NetworkEventConnect,
			Protocol:   6, // TCP
			SourceIP:   "192.168.1.100",
			DestIP:     "10.0.0.1",
			SourcePort: 45678,
			DestPort:   80,
			Direction:  "egress",
		},
	}

	// Create enriched event
	enriched := &EnrichedEvent{
		Raw:       rawEvent,
		EventID:   "test-event-123",
		Timestamp: time.Now(),
		Hostname:  "test-host",
		ProcessInfo: &ProcessInfo{
			PID:  1234,
			Comm: "curl",
		},
		SemanticType: "network_connection",
		ServiceName:  "test-service",
		Importance:   0.8,
		Interesting:  true,
		TraceID:      "trace-123",
		SpanID:       "span-456",
		Tags:         []string{"network", "outbound"},
	}

	// Convert to domain event
	domainEvent := enriched.ToDomainEvent()

	// Verify conversion
	if domainEvent.ID != domain.EventID(enriched.EventID) {
		t.Errorf("Expected ID %v, got %v", enriched.EventID, domainEvent.ID)
	}

	if domainEvent.Type != domain.EventTypeNetwork {
		t.Errorf("Expected type %v, got %v", domain.EventTypeNetwork, domainEvent.Type)
	}

	if domainEvent.Source != domain.SourceEBPF {
		t.Errorf("Expected source %v, got %v", domain.SourceEBPF, domainEvent.Source)
	}

	if domainEvent.Confidence != enriched.Importance {
		t.Errorf("Expected confidence %v, got %v", enriched.Importance, domainEvent.Confidence)
	}

	if domainEvent.Context.TraceID != enriched.TraceID {
		t.Errorf("Expected trace ID %v, got %v", enriched.TraceID, domainEvent.Context.TraceID)
	}

	// Check attributes
	if domainEvent.Attributes["ebpf_type"] != "network" {
		t.Errorf("Expected ebpf_type to be 'network', got %v", domainEvent.Attributes["ebpf_type"])
	}

	if domainEvent.Attributes["pid"] != uint32(1234) {
		t.Errorf("Expected pid to be 1234, got %v", domainEvent.Attributes["pid"])
	}

	// Check tags
	if len(domainEvent.Tags) != len(enriched.Tags) {
		t.Errorf("Expected %d tags, got %d", len(enriched.Tags), len(domainEvent.Tags))
	}

	// Check labels
	labels := domainEvent.Context.Labels
	if labels["service"] != enriched.ServiceName {
		t.Errorf("Expected service label %v, got %v", enriched.ServiceName, labels["service"])
	}

	if labels["semantic_type"] != enriched.SemanticType {
		t.Errorf("Expected semantic_type label %v, got %v", enriched.SemanticType, labels["semantic_type"])
	}
}

func TestFilterEngineBasicFiltering(t *testing.T) {
	config := &FilterConfig{
		EnableRawFiltering:   true,
		EventTypeBlacklist:   []EventType{EventTypeSyscall}, // Block syscall events
		ProcessBlacklist:     []string{"systemd"},           // Block systemd events
		EnableSemanticFilter: true,
		MinImportanceScore:   0.5, // Only events with importance > 0.5
	}

	filterEngine := NewFilterEngine(config)

	// Test raw event filtering
	syscallEvent := &RawEvent{
		Type: EventTypeSyscall,
		Comm: "test",
	}

	if filterEngine.ProcessRawEvent(syscallEvent) {
		t.Error("Expected syscall event to be filtered out")
	}

	systemdEvent := &RawEvent{
		Type: EventTypeProcess,
		Comm: "systemd",
	}

	if filterEngine.ProcessRawEvent(systemdEvent) {
		t.Error("Expected systemd event to be filtered out")
	}

	allowedEvent := &RawEvent{
		Type: EventTypeNetwork,
		Comm: "curl",
	}

	if !filterEngine.ProcessRawEvent(allowedEvent) {
		t.Error("Expected network event to be allowed")
	}

	// Test semantic filtering
	lowImportanceEvent := &EnrichedEvent{
		Raw:        allowedEvent,
		Importance: 0.3, // Below threshold
	}

	decision := filterEngine.ProcessEnrichedEvent(lowImportanceEvent)
	if decision.SendSemantic {
		t.Error("Expected low importance event to not be sent to semantic layer")
	}

	highImportanceEvent := &EnrichedEvent{
		Raw:        allowedEvent,
		Importance: 0.8, // Above threshold
	}

	decision = filterEngine.ProcessEnrichedEvent(highImportanceEvent)
	if !decision.SendSemantic {
		t.Error("Expected high importance event to be sent to semantic layer")
	}
}

func TestRawEventFormatterBasic(t *testing.T) {
	formatter := NewRawEventFormatter(&RawEventFormatterOptions{
		IncludeTimestamp: false, // Easier to test without timestamp
		ColorOutput:      false, // Easier to test without colors
		VerboseMode:      false,
	})

	// Test network event formatting
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

	// Should contain basic elements
	expectedSubstrings := []string{"curl[1234]", "TCP", "192.168.1.100:45678", "10.0.0.1:80", "→"}
	for _, expected := range expectedSubstrings {
		if !contains(formatted, expected) {
			t.Errorf("Expected formatted output to contain '%s', got: %s", expected, formatted)
		}
	}

	// Test process event formatting
	processEvent := &RawEvent{
		Type: EventTypeProcess,
		PID:  5678,
		Comm: "bash",
		Details: &ProcessEvent{
			SubType: ProcessEventExec,
			Args:    []string{"/bin/ls", "-la"},
		},
	}

	formatted = formatter.FormatEvent(processEvent)
	expectedSubstrings = []string{"bash[5678]", "exec", "/bin/ls -la"}
	for _, expected := range expectedSubstrings {
		if !contains(formatted, expected) {
			t.Errorf("Expected formatted output to contain '%s', got: %s", expected, formatted)
		}
	}
}

func TestMemoryRawEventStore(t *testing.T) {
	store := NewMemoryRawEventStore(time.Hour)

	// Create test events
	event1 := &RawEvent{
		Type:      EventTypeNetwork,
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		Comm:      "curl",
	}

	event2 := &RawEvent{
		Type:      EventTypeProcess,
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       5678,
		Comm:      "bash",
	}

	// Store events
	if err := store.Store(nil, event1); err != nil {
		t.Fatalf("Failed to store event1: %v", err)
	}

	if err := store.Store(nil, event2); err != nil {
		t.Fatalf("Failed to store event2: %v", err)
	}

	// Query all events
	allEvents, err := store.Query(nil, &EventFilter{})
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}

	if len(allEvents) != 2 {
		t.Errorf("Expected 2 events, got %d", len(allEvents))
	}

	// Query network events only
	networkFilter := &EventFilter{
		EventTypes: []EventType{EventTypeNetwork},
	}

	networkEvents, err := store.Query(nil, networkFilter)
	if err != nil {
		t.Fatalf("Failed to query network events: %v", err)
	}

	if len(networkEvents) != 1 {
		t.Errorf("Expected 1 network event, got %d", len(networkEvents))
	}

	if networkEvents[0].Type != EventTypeNetwork {
		t.Errorf("Expected network event, got %v", networkEvents[0].Type)
	}

	// Query by PID
	pidFilter := &EventFilter{
		PIDs: []uint32{1234},
	}

	pidEvents, err := store.Query(nil, pidFilter)
	if err != nil {
		t.Fatalf("Failed to query PID events: %v", err)
	}

	if len(pidEvents) != 1 {
		t.Errorf("Expected 1 PID event, got %d", len(pidEvents))
	}

	if pidEvents[0].PID != 1234 {
		t.Errorf("Expected PID 1234, got %d", pidEvents[0].PID)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsHelper(s, substr))))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
