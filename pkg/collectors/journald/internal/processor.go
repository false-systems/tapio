package internal

import (
	"context"
	"fmt"
	"strings"

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
	
	// Create log event data
	eventData := p.createLogData(entry)
	
	// Determine severity based on priority
	severity := p.determineSeverity(entry.Priority)
	
	// Create the domain event
	event := domain.Event{
		ID:         fmt.Sprintf("journald_%s_%d", entry.Cursor, entry.Timestamp.UnixNano()),
		Type:       string(domain.EventTypeLog),
		Source:     string(domain.SourceJournald),
		Timestamp:  entry.Timestamp,
		Data:       eventData,
		Context:    p.createContextData(entry),
		Severity:   string(severity),
		Confidence: 1.0, // Journal entries are direct observations
		Attributes: map[string]interface{}{
			"hash":      p.generateHash(entry),
			"signature": fmt.Sprintf("%s:%s", entry.Unit, entry.Priority.String()),
			"unit":      entry.Unit,
			"priority":  entry.Priority.String(),
			"comm":      entry.Comm,
		},
	}
	
	return event, nil
}

// createLogData creates a log event data map
func (p *eventProcessor) createLogData(entry *core.LogEntry) map[string]interface{} {
	data := map[string]interface{}{
		"message":    entry.Message,
		"unit":       entry.Unit,
		"priority":   int(entry.Priority),
		"facility":   entry.Facility,
		"identifier": entry.Identifier,
		"cursor":     entry.Cursor,
		"boot_id":    entry.BootID,
		"machine_id": entry.MachineID,
	}
	
	// Add important fields
	if entry.Comm != "" {
		data["comm"] = entry.Comm
	}
	if entry.Exe != "" {
		data["exe"] = entry.Exe
	}
	if entry.Cmdline != "" {
		data["cmdline"] = entry.Cmdline
	}
	if entry.HostName != "" {
		data["hostname"] = entry.HostName
	}
	if entry.Session != "" {
		data["session"] = entry.Session
	}
	if entry.UserUnit != "" {
		data["user_unit"] = entry.UserUnit
	}
	
	// Add all systemd journal fields
	if len(entry.Fields) > 0 {
		fields := make(map[string]interface{})
		for k, v := range entry.Fields {
			fields[k] = v
		}
		data["fields"] = fields
	}
	
	return data
}

// createContextData creates the event context data
func (p *eventProcessor) createContextData(entry *core.LogEntry) map[string]interface{} {
	context := map[string]interface{}{
		"host":       entry.HostName,
		"priority":   entry.Priority.String(),
		"facility":   entry.Facility,
		"identifier": entry.Identifier,
	}
	
	labels := map[string]string{
		"priority":   entry.Priority.String(),
		"facility":   entry.Facility,
		"identifier": entry.Identifier,
	}
	
	// Add unit information
	if entry.Unit != "" {
		labels["unit"] = entry.Unit
		context["unit"] = entry.Unit
	}
	if entry.UserUnit != "" {
		labels["user_unit"] = entry.UserUnit
		context["user_unit"] = entry.UserUnit
	}
	
	// Add process information
	if entry.Comm != "" {
		labels["comm"] = entry.Comm
		context["comm"] = entry.Comm
	}
	
	tags := []string{"journald", entry.Priority.String()}
	
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
	
	context["labels"] = labels
	context["tags"] = tags
	
	// Add PID, UID, GID if available
	if entry.PID > 0 {
		context["pid"] = entry.PID
	}
	if entry.UID >= 0 {
		context["uid"] = entry.UID
	}
	if entry.GID >= 0 {
		context["gid"] = entry.GID
	}
	
	// Add container if available
	if containerID, ok := entry.Fields["CONTAINER_ID"]; ok {
		if str, ok := containerID.(string); ok {
			context["container"] = str
		}
	}
	
	return context
}


// determineSeverity determines the event severity based on syslog priority
func (p *eventProcessor) determineSeverity(priority core.Priority) domain.SeverityLevel {
	switch priority {
	case core.PriorityEmergency:
		return domain.SeverityCritical
	case core.PriorityAlert:
		return domain.SeverityCritical
	case core.PriorityCritical:
		return domain.SeverityCritical
	case core.PriorityError:
		return domain.SeverityHigh
	case core.PriorityWarning:
		return domain.SeverityWarning
	case core.PriorityNotice:
		return domain.SeverityLow
	case core.PriorityInfo:
		return domain.SeverityLow
	case core.PriorityDebug:
		return domain.SeverityLow
	default:
		return domain.SeverityLow
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