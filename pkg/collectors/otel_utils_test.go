package collectors

import (
	"testing"
)

func TestGenerateTraceID(t *testing.T) {
	traceID := GenerateTraceID()

	// Should be 32 characters (128 bits in hex)
	if len(traceID) != 32 {
		t.Errorf("Expected trace ID length 32, got %d", len(traceID))
	}

	// Should be valid hex
	for _, c := range traceID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Invalid character in trace ID: %c", c)
		}
	}

	// Should be unique
	traceID2 := GenerateTraceID()
	if traceID == traceID2 {
		t.Error("Generated trace IDs should be unique")
	}
}

func TestGenerateSpanID(t *testing.T) {
	spanID := GenerateSpanID()

	// Should be 16 characters (64 bits in hex)
	if len(spanID) != 16 {
		t.Errorf("Expected span ID length 16, got %d", len(spanID))
	}

	// Should be valid hex
	for _, c := range spanID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Invalid character in span ID: %c", c)
		}
	}
}

func TestExtractTraceIDFromHeaders(t *testing.T) {
	tests := []struct {
		name          string
		headers       map[string]string
		expectedTrace string
		expectedSpan  string
	}{
		{
			name: "valid traceparent header",
			headers: map[string]string{
				"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
			expectedTrace: "4bf92f3577b34da6a3ce929d0e0e4736",
			expectedSpan:  "00f067aa0ba902b7",
		},
		{
			name:          "no trace headers",
			headers:       map[string]string{"other": "value"},
			expectedTrace: "",
			expectedSpan:  "",
		},
		{
			name: "malformed traceparent",
			headers: map[string]string{
				"traceparent": "invalid",
			},
			expectedTrace: "",
			expectedSpan:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traceID, spanID := ExtractTraceIDFromHeaders(tt.headers)
			if traceID != tt.expectedTrace {
				t.Errorf("Expected trace ID %s, got %s", tt.expectedTrace, traceID)
			}
			if spanID != tt.expectedSpan {
				t.Errorf("Expected span ID %s, got %s", tt.expectedSpan, spanID)
			}
		})
	}
}

func TestExtractTraceIDFromAnnotations(t *testing.T) {
	tests := []struct {
		name          string
		annotations   map[string]string
		expectedTrace string
		expectedSpan  string
	}{
		{
			name: "standard opentelemetry annotations",
			annotations: map[string]string{
				"trace.opentelemetry.io/traceid": "abc123",
				"trace.opentelemetry.io/spanid":  "def456",
			},
			expectedTrace: "abc123",
			expectedSpan:  "def456",
		},
		{
			name: "alternative annotation keys",
			annotations: map[string]string{
				"x-trace-id": "xyz789",
				"x-span-id":  "uvw012",
			},
			expectedTrace: "xyz789",
			expectedSpan:  "uvw012",
		},
		{
			name:          "no trace annotations",
			annotations:   map[string]string{"app": "myapp"},
			expectedTrace: "",
			expectedSpan:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			traceID, spanID := ExtractTraceIDFromAnnotations(tt.annotations)
			if traceID != tt.expectedTrace {
				t.Errorf("Expected trace ID %s, got %s", tt.expectedTrace, traceID)
			}
			if spanID != tt.expectedSpan {
				t.Errorf("Expected span ID %s, got %s", tt.expectedSpan, spanID)
			}
		})
	}
}
