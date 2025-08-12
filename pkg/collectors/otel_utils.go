package collectors

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateTraceID generates a new OpenTelemetry-compatible trace ID
// Returns a 32-character lowercase hex string (128-bit value)
func GenerateTraceID() string {
	traceID := make([]byte, 16) // 128 bits
	rand.Read(traceID)
	return hex.EncodeToString(traceID)
}

// GenerateSpanID generates a new OpenTelemetry-compatible span ID
// Returns a 16-character lowercase hex string (64-bit value)
func GenerateSpanID() string {
	spanID := make([]byte, 8) // 64 bits
	rand.Read(spanID)
	return hex.EncodeToString(spanID)
}

// ExtractTraceIDFromHeaders extracts trace ID from HTTP headers
// Supports W3C Trace Context format (traceparent header)
func ExtractTraceIDFromHeaders(headers map[string]string) (traceID, spanID string) {
	// W3C Trace Context format: version-traceid-spanid-flags
	// Example: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
	if traceparent, ok := headers["traceparent"]; ok {
		parts := parseTraceparent(traceparent)
		if len(parts) >= 3 {
			return parts[1], parts[2]
		}
	}
	return "", ""
}

// parseTraceparent parses the W3C traceparent header
func parseTraceparent(traceparent string) []string {
	// Simple parsing - in production would need more validation
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(traceparent); i++ {
		if traceparent[i] == '-' {
			parts = append(parts, traceparent[start:i])
			start = i + 1
		}
	}
	if start < len(traceparent) {
		parts = append(parts, traceparent[start:])
	}
	return parts
}

// ExtractTraceIDFromAnnotations extracts trace ID from K8s annotations
func ExtractTraceIDFromAnnotations(annotations map[string]string) (traceID, spanID string) {
	// Check common annotation keys
	keys := []string{
		"trace.opentelemetry.io/traceid",
		"opentelemetry.io/trace-id",
		"x-trace-id",
		"trace-id",
	}

	for _, key := range keys {
		if id, ok := annotations[key]; ok {
			traceID = id
			break
		}
	}

	// Check for span ID
	spanKeys := []string{
		"trace.opentelemetry.io/spanid",
		"opentelemetry.io/span-id",
		"x-span-id",
		"span-id",
	}

	for _, key := range spanKeys {
		if id, ok := annotations[key]; ok {
			spanID = id
			break
		}
	}

	return traceID, spanID
}
