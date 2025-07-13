package opinionated

import "time"

// OpinionatedEvent represents an OPINIONATED event format
type OpinionatedEvent struct {
	// Core identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	
	// OPINIONATED classification
	Category    EventCategory    `json:"category"`
	Severity    EventSeverity    `json:"severity"`
	Confidence  float32          `json:"confidence"`
	
	// Source information
	Source      EventSource      `json:"source"`
	
	// AI-ready context
	Context     OpinionatedContext `json:"context"`
	
	// Structured data
	Data        map[string]interface{} `json:"data"`
	Attributes  map[string]interface{} `json:"attributes"`
	
	// Correlation hints
	CorrelationHints []string `json:"correlation_hints"`
}

// EventCategory represents OPINIONATED event categories
type EventCategory string

const (
	CategorySystemHealth    EventCategory = "system_health"
	CategoryNetworkHealth   EventCategory = "network_health"
	CategoryAppHealth       EventCategory = "app_health"
	CategorySecurityEvent   EventCategory = "security_event"
	CategoryPerformanceIssue EventCategory = "performance_issue"
)

// EventSeverity represents OPINIONATED severity levels
type EventSeverity string

const (
	SeverityCritical EventSeverity = "critical"
	SeverityHigh     EventSeverity = "high"
	SeverityMedium   EventSeverity = "medium"
	SeverityLow      EventSeverity = "low"
	SeverityInfo     EventSeverity = "info"
)

// EventSource identifies the source of an OPINIONATED event
type EventSource struct {
	Collector string `json:"collector"`
	Component string `json:"component"`
	Node      string `json:"node"`
}

// OpinionatedContext provides AI-ready context
type OpinionatedContext struct {
	// Kubernetes context
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	
	// Process context
	PID         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	
	// Network context
	SrcIP   string `json:"src_ip,omitempty"`
	DstIP   string `json:"dst_ip,omitempty"`
	SrcPort uint16 `json:"src_port,omitempty"`
	DstPort uint16 `json:"dst_port,omitempty"`
	
	// Custom context
	Custom map[string]string `json:"custom,omitempty"`
}