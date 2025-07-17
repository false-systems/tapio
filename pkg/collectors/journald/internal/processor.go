package internal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEntry converts a raw journal entry to a domain event
func (p *eventProcessor) ProcessEntry(ctx context.Context, entry *core.LogEntry) (domain.Event, error) {
	if entry == nil {
		return domain.Event{}, fmt.Errorf("nil entry")
	}
	
	// Create log event payload
	payload := p.createLogPayload(entry)
	
	// Determine severity based on priority
	severity := p.determineSeverity(entry.Priority)
	
	// Create the domain event
	event := domain.Event{
		ID:        domain.EventID(fmt.Sprintf("journald_%s_%d", entry.Cursor, entry.Timestamp.UnixNano())),
		Type:      domain.EventTypeLog,
		Source:    domain.SourceJournald,
		Timestamp: entry.Timestamp,
		Payload:   payload,
		Context:   p.createContext(entry),
		Metadata:  p.createMetadata(entry),
		Severity:  severity,
		Confidence: 1.0, // Journal entries are direct observations
		Fingerprint: domain.EventFingerprint{
			Hash:      p.generateHash(entry),
			Signature: fmt.Sprintf("%s:%s", entry.Unit, entry.Priority.String()),
			Fields: map[string]string{
				"unit":     entry.Unit,
				"priority": entry.Priority.String(),
				"comm":     entry.Comm,
			},
		},
	}
	
	return event, nil
}

// createLogPayload creates a log event payload
func (p *eventProcessor) createLogPayload(entry *core.LogEntry) domain.LogEventPayload {
	payload := domain.LogEventPayload{
		Message:    entry.Message,
		Unit:       entry.Unit,
		Priority:   int32(entry.Priority),
		Facility:   entry.Facility,
		Identifier: entry.Identifier,
		Fields:     make(map[string]string),
	}
	
	// Convert all fields to string map
	for k, v := range entry.Fields {
		if str, ok := v.(string); ok {
			payload.Fields[k] = str
		} else {
			payload.Fields[k] = fmt.Sprintf("%v", v)
		}
	}
	
	// Add important fields that might not be in the Fields map
	if entry.Comm != "" {
		payload.Fields["_COMM"] = entry.Comm
	}
	if entry.Exe != "" {
		payload.Fields["_EXE"] = entry.Exe
	}
	if entry.Cmdline != "" {
		payload.Fields["_CMDLINE"] = entry.Cmdline
	}
	if entry.HostName != "" {
		payload.Fields["_HOSTNAME"] = entry.HostName
	}
	if entry.Session != "" {
		payload.Fields["_SYSTEMD_SESSION"] = entry.Session
	}
	if entry.UserUnit != "" {
		payload.Fields["_SYSTEMD_USER_UNIT"] = entry.UserUnit
	}
	
	// Add boot and machine IDs
	payload.Fields["_BOOT_ID"] = entry.BootID
	payload.Fields["_MACHINE_ID"] = entry.MachineID
	payload.Fields["__CURSOR"] = entry.Cursor
	
	return payload
}

// createContext creates the event context
func (p *eventProcessor) createContext(entry *core.LogEntry) domain.EventContext {
	labels := domain.Labels{
		"priority":   entry.Priority.String(),
		"facility":   entry.Facility,
		"identifier": entry.Identifier,
	}
	
	// Add unit information
	if entry.Unit != "" {
		labels["unit"] = entry.Unit
	}
	if entry.UserUnit != "" {
		labels["user_unit"] = entry.UserUnit
	}
	
	// Add process information
	if entry.Comm != "" {
		labels["comm"] = entry.Comm
	}
	
	tags := domain.Tags{
		"journald",
		entry.Priority.String(),
	}
	
	// Add unit type tags
	if entry.Unit != "" {
		tags = append(tags, "systemd")
		if strings.HasSuffix(entry.Unit, ".service") {
			tags = append(tags, "service")
		} else if strings.HasSuffix(entry.Unit, ".socket") {
			tags = append(tags, "socket")
		} else if strings.HasSuffix(entry.Unit, ".timer") {
			tags = append(tags, "timer")
		}
	}
	
	// Add severity tags
	if entry.Priority <= core.PriorityError {
		tags = append(tags, "error")
	}
	if entry.Priority <= core.PriorityWarning {
		tags = append(tags, "warning")
	}
	
	ctx := domain.EventContext{
		Host:   entry.HostName,
		Labels: labels,
		Tags:   tags,
	}
	
	// Add PID, UID, GID if available
	if entry.PID > 0 {
		ctx.PID = &entry.PID
	}
	if entry.UID >= 0 {
		ctx.UID = &entry.UID
	}
	if entry.GID >= 0 {
		ctx.GID = &entry.GID
	}
	
	// Add container if available
	if containerID, ok := entry.Fields["CONTAINER_ID"]; ok {
		if str, ok := containerID.(string); ok {
			ctx.Container = str
		}
	}
	
	return ctx
}

// createMetadata creates the event metadata
func (p *eventProcessor) createMetadata(entry *core.LogEntry) domain.EventMetadata {
	annotations := map[string]string{
		"priority":         entry.Priority.String(),
		"facility":         entry.Facility,
		"boot_id":          entry.BootID,
		"machine_id":       entry.MachineID,
		"cursor":           entry.Cursor,
		"monotonic_time":   strconv.FormatUint(entry.MonotonicTime, 10),
	}
	
	// Add process information
	if entry.PID > 0 {
		annotations["pid"] = strconv.FormatInt(int64(entry.PID), 10)
	}
	if entry.UID >= 0 {
		annotations["uid"] = strconv.FormatInt(int64(entry.UID), 10)
	}
	if entry.GID >= 0 {
		annotations["gid"] = strconv.FormatInt(int64(entry.GID), 10)
	}
	
	// Add systemd information
	if entry.Unit != "" {
		annotations["systemd_unit"] = entry.Unit
	}
	if entry.UserUnit != "" {
		annotations["systemd_user_unit"] = entry.UserUnit
	}
	if entry.Session != "" {
		annotations["systemd_session"] = entry.Session
	}
	
	return domain.EventMetadata{
		SchemaVersion: "1.0",
		ProcessedAt:   time.Now(),
		ProcessedBy:   "journald-collector",
		Annotations:   annotations,
	}
}

// determineSeverity determines the event severity based on syslog priority
func (p *eventProcessor) determineSeverity(priority core.Priority) domain.Severity {
	switch priority {
	case core.PriorityEmergency:
		return domain.SeverityCritical
	case core.PriorityAlert:
		return domain.SeverityCritical
	case core.PriorityCritical:
		return domain.SeverityCritical
	case core.PriorityError:
		return domain.SeverityError
	case core.PriorityWarning:
		return domain.SeverityWarn
	case core.PriorityNotice:
		return domain.SeverityInfo
	case core.PriorityInfo:
		return domain.SeverityInfo
	case core.PriorityDebug:
		return domain.SeverityDebug
	default:
		return domain.SeverityInfo
	}
}

// generateHash generates a hash for event deduplication
func (p *eventProcessor) generateHash(entry *core.LogEntry) string {
	// Use cursor as primary identifier since it's unique
	if entry.Cursor != "" {
		return entry.Cursor
	}
	
	// Fallback to timestamp + message hash
	return fmt.Sprintf("%d-%s-%s", 
		entry.Timestamp.UnixNano(),
		entry.Unit,
		p.hashString(entry.Message))
}

// hashString creates a simple hash of a string
func (p *eventProcessor) hashString(s string) string {
	if len(s) > 32 {
		return fmt.Sprintf("%x", []byte(s[:32]))
	}
	return fmt.Sprintf("%x", []byte(s))
}