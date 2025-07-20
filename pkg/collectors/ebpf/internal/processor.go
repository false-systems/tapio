package internal

import (
	"context"
	"fmt"

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
	// Determine event type and create appropriate data
	eventType, eventData, err := p.createEventData(raw)
	if err != nil {
		return domain.Event{}, fmt.Errorf("failed to create event data: %w", err)
	}

	// Create the domain event
	event := domain.Event{
		ID:         domain.EventID(fmt.Sprintf("ebpf_%d_%d_%d", raw.Timestamp.UnixNano(), raw.PID, raw.CPU)),
		Type:       eventType,
		Source:     domain.SourceEBPF,
		Timestamp:  raw.Timestamp,
		Data:       eventData,
		Context:    p.createContextData(raw),
		Severity:   p.determineSeverity(raw),
		Confidence: 1.0, // eBPF events are direct observations
		Attributes: map[string]interface{}{
			"hash":      p.generateHash(raw),
			"signature": raw.Type,
			"pid":       raw.PID,
			"comm":      raw.Comm,
		},
		Tags: []string{
			"ebpf",
			raw.Type,
		},
	}

	return event, nil
}

// createEventData creates the appropriate event data based on event type
func (p *eventProcessor) createEventData(raw core.RawEvent) (domain.EventType, map[string]interface{}, error) {
	switch raw.Type {
	case "syscall", "network", "memory":
		return domain.EventTypeSystem, p.createSystemData(raw), nil

	case "process_start", "process_exit":
		return domain.EventTypeProcess, p.createSystemData(raw), nil

	default:
		return domain.EventTypeSystem, p.createSystemData(raw), nil
	}
}

// createSystemData creates a system event data map
func (p *eventProcessor) createSystemData(raw core.RawEvent) map[string]interface{} {
	data := make(map[string]interface{})

	// Add basic event information
	data["ebpf_type"] = raw.Type
	data["pid"] = raw.PID
	data["tid"] = raw.TID
	data["uid"] = raw.UID
	data["gid"] = raw.GID
	data["comm"] = raw.Comm
	data["cpu"] = raw.CPU

	// Extract common fields
	if syscall, ok := raw.Decoded["syscall"].(string); ok {
		data["syscall"] = syscall
	}

	if retCode, ok := raw.Decoded["return_code"].(int32); ok {
		data["return_code"] = retCode
	}

	// Extract memory-related fields
	if memUsage, ok := raw.Decoded["memory_usage"].(int64); ok {
		data["memory_usage"] = memUsage
	}

	if memLimit, ok := raw.Decoded["memory_limit"].(int64); ok {
		data["memory_limit"] = memLimit
	}

	// Extract network-related fields
	if srcIP, ok := raw.Decoded["source_ip"].(string); ok {
		data["source_ip"] = srcIP
	}

	if dstIP, ok := raw.Decoded["dest_ip"].(string); ok {
		data["dest_ip"] = dstIP
	}

	if port, ok := raw.Decoded["port"].(int32); ok {
		data["port"] = port
	}

	if protocol, ok := raw.Decoded["protocol"].(string); ok {
		data["protocol"] = protocol
	}

	if bytesSent, ok := raw.Decoded["bytes_sent"].(int64); ok {
		data["bytes_sent"] = bytesSent
	}

	if bytesRecv, ok := raw.Decoded["bytes_received"].(int64); ok {
		data["bytes_received"] = bytesRecv
	}

	// Add any additional decoded fields
	for k, v := range raw.Decoded {
		if _, exists := data[k]; !exists {
			data[k] = v
		}
	}

	return data
}

// createContextData creates the event context data
func (p *eventProcessor) createContextData(raw core.RawEvent) domain.EventContext {
	return domain.EventContext{
		PID:  int(raw.PID),
		UID:  int(raw.UID),
		GID:  int(raw.GID),
		Comm: raw.Comm,
		Labels: map[string]string{
			"comm":      raw.Comm,
			"cpu":       fmt.Sprintf("%d", raw.CPU),
			"ebpf_type": raw.Type,
		},
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.EventSeverity {
	switch raw.Type {
	case "oom_kill", "kernel_panic":
		return domain.EventSeverityCritical

	case "memory_pressure", "cpu_throttle":
		return domain.EventSeverityWarning

	case "syscall_error":
		if retCode, ok := raw.Decoded["return_code"].(int32); ok && retCode < 0 {
			return domain.EventSeverityHigh
		}
		return domain.EventSeverityLow

	default:
		return domain.EventSeverityLow
	}
}

// generateHash generates a hash for event deduplication
func (p *eventProcessor) generateHash(raw core.RawEvent) string {
	// Simple hash based on type, PID, and timestamp
	// In production, use a proper hash function
	return fmt.Sprintf("%s-%d-%d", raw.Type, raw.PID, raw.Timestamp.Unix())
}
