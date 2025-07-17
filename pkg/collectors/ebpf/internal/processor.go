package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEvent converts a raw eBPF event to a domain event
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.Event, error) {
	// Determine event type and create appropriate payload
	eventType, payload, err := p.createPayload(raw)
	if err != nil {
		return domain.Event{}, fmt.Errorf("failed to create payload: %w", err)
	}
	
	// Create the domain event
	event := domain.Event{
		ID:        domain.EventID(fmt.Sprintf("ebpf_%d_%d_%d", raw.Timestamp.UnixNano(), raw.PID, raw.CPU)),
		Type:      eventType,
		Source:    domain.SourceEBPF,
		Timestamp: raw.Timestamp,
		Payload:   payload,
		Context:   p.createContext(raw),
		Metadata:  p.createMetadata(raw),
		Severity:  p.determineSeverity(raw),
		Confidence: 1.0, // eBPF events are direct observations
		Fingerprint: domain.EventFingerprint{
			Hash:      p.generateHash(raw),
			Signature: raw.Type,
			Fields: map[string]string{
				"type": raw.Type,
				"pid":  fmt.Sprintf("%d", raw.PID),
				"comm": raw.Comm,
			},
		},
	}
	
	return event, nil
}

// createPayload creates the appropriate payload based on event type
func (p *eventProcessor) createPayload(raw core.RawEvent) (domain.EventType, domain.EventPayload, error) {
	switch raw.Type {
	case "syscall", "network", "memory":
		return domain.EventTypeSystem, p.createSystemPayload(raw), nil
		
	case "process_start", "process_exit":
		return domain.EventTypeProcess, p.createSystemPayload(raw), nil
		
	default:
		return domain.EventTypeSystem, p.createSystemPayload(raw), nil
	}
}

// createSystemPayload creates a system event payload
func (p *eventProcessor) createSystemPayload(raw core.RawEvent) domain.SystemEventPayload {
	payload := domain.SystemEventPayload{
		Arguments: make(map[string]string),
	}
	
	// Extract common fields
	if syscall, ok := raw.Decoded["syscall"].(string); ok {
		payload.Syscall = syscall
	}
	
	if retCode, ok := raw.Decoded["return_code"].(int32); ok {
		payload.ReturnCode = retCode
	}
	
	// Extract memory-related fields
	if memUsage, ok := raw.Decoded["memory_usage"].(int64); ok {
		payload.MemoryUsage = &memUsage
	}
	
	if memLimit, ok := raw.Decoded["memory_limit"].(int64); ok {
		payload.MemoryLimit = &memLimit
	}
	
	// Extract network-related fields
	if srcIP, ok := raw.Decoded["source_ip"].(string); ok {
		payload.SourceIP = srcIP
	}
	
	if dstIP, ok := raw.Decoded["dest_ip"].(string); ok {
		payload.DestIP = dstIP
	}
	
	if port, ok := raw.Decoded["port"].(int32); ok {
		payload.Port = &port
	}
	
	if protocol, ok := raw.Decoded["protocol"].(string); ok {
		payload.Protocol = protocol
	}
	
	if bytesSent, ok := raw.Decoded["bytes_sent"].(int64); ok {
		payload.BytesSent = &bytesSent
	}
	
	if bytesRecv, ok := raw.Decoded["bytes_received"].(int64); ok {
		payload.BytesReceived = &bytesRecv
	}
	
	// Add any additional arguments
	for k, v := range raw.Decoded {
		if str, ok := v.(string); ok {
			payload.Arguments[k] = str
		}
	}
	
	return payload
}

// createContext creates the event context
func (p *eventProcessor) createContext(raw core.RawEvent) domain.EventContext {
	pid := int32(raw.PID)
	uid := int32(raw.UID)
	gid := int32(raw.GID)
	
	return domain.EventContext{
		PID: &pid,
		UID: &uid,
		GID: &gid,
		Labels: domain.Labels{
			"comm": raw.Comm,
			"cpu":  fmt.Sprintf("%d", raw.CPU),
		},
		Tags: domain.Tags{
			"ebpf",
			raw.Type,
		},
	}
}

// createMetadata creates the event metadata
func (p *eventProcessor) createMetadata(raw core.RawEvent) domain.EventMetadata {
	return domain.EventMetadata{
		SchemaVersion: "1.0",
		ProcessedAt:   time.Now(),
		ProcessedBy:   "ebpf-collector",
		Annotations: map[string]string{
			"raw_type": raw.Type,
			"tid":      fmt.Sprintf("%d", raw.TID),
		},
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.Severity {
	switch raw.Type {
	case "oom_kill", "kernel_panic":
		return domain.SeverityCritical
		
	case "memory_pressure", "cpu_throttle":
		return domain.SeverityWarn
		
	case "syscall_error":
		if retCode, ok := raw.Decoded["return_code"].(int32); ok && retCode < 0 {
			return domain.SeverityError
		}
		return domain.SeverityInfo
		
	default:
		return domain.SeverityInfo
	}
}

// generateHash generates a hash for event deduplication
func (p *eventProcessor) generateHash(raw core.RawEvent) string {
	// Simple hash based on type, PID, and timestamp
	// In production, use a proper hash function
	return fmt.Sprintf("%s-%d-%d", raw.Type, raw.PID, raw.Timestamp.Unix())
}