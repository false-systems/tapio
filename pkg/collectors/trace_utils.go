package collectors

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateTraceID generates a new 128-bit trace ID
func GenerateTraceID() string {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%016x%016x", 0, 0)
	}
	return hex.EncodeToString(bytes)
}

// GenerateSpanID generates a new 64-bit span ID
func GenerateSpanID() string {
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%016x", 0)
	}
	return hex.EncodeToString(bytes)
}

// ExtractTraceIDFromAnnotations attempts to extract a trace ID from K8s annotations
func ExtractTraceIDFromAnnotations(annotations map[string]string) (string, bool) {
	// Check common trace ID annotation keys
	traceKeys := []string{
		"trace.id",
		"trace-id",
		"x-trace-id",
		"otel.trace.id",
		"opentelemetry.trace.id",
	}

	for _, key := range traceKeys {
		if traceID, ok := annotations[key]; ok && traceID != "" {
			return traceID, true
		}
		// Also check with "tapio/" prefix
		if traceID, ok := annotations["tapio/"+key]; ok && traceID != "" {
			return traceID, true
		}
	}

	// Check for W3C trace context
	if traceContext, ok := annotations["traceparent"]; ok {
		// W3C format: version-trace_id-parent_id-flags
		parts := strings.Split(traceContext, "-")
		if len(parts) >= 3 {
			return parts[1], true
		}
	}

	return "", false
}
