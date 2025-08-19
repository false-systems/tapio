package domain

import (
	"context"
	"time"
)

// RawEvent represents raw data collected from any source
type RawEvent struct {
	// REQUIRED FIELDS - always present
	Timestamp time.Time // When collected
	Source    string    // Which collector ("kernel", "dns", "kubeapi")
	Data      []byte    // Raw bytes (protobuf, json, binary)

	// OPTIONAL STRUCTURED FIELDS - for collectors that need them
	Type        string            `json:"type,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
	SpanID      string            `json:"span_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	EventSource string            `json:"event_source,omitempty"`
}

// EventParser parses raw events into structured ObservationEvents - CLEAR
type EventParser interface {
	Parse(raw RawEvent) (*ObservationEvent, error)
	Source() string // "kernel", "dns", etc
}

// RawEventProcessor processes raw events
type RawEventProcessor interface {
	// ProcessRawEvent processes a raw event
	ProcessRawEvent(ctx context.Context, event RawEvent) error
}
