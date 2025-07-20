package internal

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEvent converts a raw systemd event to a domain event
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.Event, error) {
	// Create service event data
	eventData := p.createServiceData(raw)

	// Determine severity based on event type and state
	severity := p.determineSeverity(raw)

	// Create the domain event
	event := domain.Event{
		ID:         domain.EventID(fmt.Sprintf("systemd_%s_%s_%d", raw.UnitName, raw.Type, raw.Timestamp.UnixNano())),
		Type:       domain.EventTypeService,
		Source:     domain.SourceSystemd,
		Timestamp:  raw.Timestamp,
		Data:       eventData,
		Context:    p.createEventContext(raw),
		Severity:   severity,
		Confidence: 1.0, // systemd events are direct observations
		Attributes: map[string]interface{}{
			"hash":      p.generateHash(raw),
			"signature": fmt.Sprintf("%s:%s", raw.UnitName, string(raw.Type)),
			"unit_name": raw.UnitName,
			"unit_type": raw.UnitType,
			"type":      string(raw.Type),
		},
	}

	return event, nil
}

// createServiceData creates a service event data map
func (p *eventProcessor) createServiceData(raw core.RawEvent) map[string]interface{} {
	data := map[string]interface{}{
		"service_name": raw.UnitName,
		"unit_type":    raw.UnitType,
		"event_type":   p.mapEventType(raw.Type),
		"old_state":    raw.OldState,
		"new_state":    raw.NewState,
		"sub_state":    raw.SubState,
		"result":       raw.Result,
	}

	// Add exit code and signal if present
	if raw.ExitCode != 0 {
		data["exit_code"] = raw.ExitCode
	}

	if raw.ExitStatus != 0 {
		data["exit_status"] = raw.ExitStatus
		data["signal"] = raw.ExitStatus
	}

	if raw.MainPID > 0 {
		data["main_pid"] = raw.MainPID
	}

	// Add all properties
	if len(raw.Properties) > 0 {
		properties := make(map[string]interface{})
		for k, v := range raw.Properties {
			properties[k] = v
		}
		data["properties"] = properties
	}

	return data
}

// mapEventType maps internal event types to domain event types
func (p *eventProcessor) mapEventType(eventType core.EventType) string {
	switch eventType {
	case core.EventTypeStart:
		return "start"
	case core.EventTypeStop:
		return "stop"
	case core.EventTypeRestart:
		return "restart"
	case core.EventTypeReload:
		return "reload"
	case core.EventTypeFailure:
		return "failure"
	case core.EventTypeStateChange:
		return "state_change"
	default:
		return string(eventType)
	}
}

// createEventContext creates the event context
func (p *eventProcessor) createEventContext(raw core.RawEvent) domain.EventContext {
	labels := map[string]string{
		"unit_name": raw.UnitName,
		"unit_type": raw.UnitType,
		"state":     raw.NewState,
		"sub_state": raw.SubState,
	}

	// Add result for failed services
	if raw.Result != "" && raw.Result != "success" {
		labels["result"] = raw.Result
	}

	metadata := map[string]interface{}{
		"unit_path": fmt.Sprintf("/system/%s/%s", raw.UnitType, raw.UnitName),
		"source":    "systemd",
	}

	// Add state tags
	tags := []string{"systemd", raw.UnitType}
	if raw.NewState == core.StateFailed {
		tags = append(tags, "failed")
		metadata["failed"] = true
	}
	if raw.Type == core.EventTypeFailure {
		tags = append(tags, "failure")
		metadata["failure_event"] = true
	}

	metadata["tags"] = tags

	return domain.EventContext{
		Service:   "systemd",
		Component: raw.UnitName,
		Host:      p.getHostname(),
		PID:       int(raw.MainPID),
		Labels:    labels,
		Metadata:  metadata,
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.EventSeverity {
	// Check for failures
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		// Critical services
		if p.isCriticalService(raw.UnitName) {
			return domain.EventSeverityCritical
		}
		return domain.EventSeverityHigh
	}

	// Check for restart events
	if raw.Type == core.EventTypeRestart {
		return domain.EventSeverityWarning
	}

	// State changes
	if raw.Type == core.EventTypeStateChange {
		if raw.OldState == core.StateActive && raw.NewState == core.StateInactive {
			return domain.EventSeverityWarning
		}
	}

	// Exit codes
	if raw.ExitCode != 0 {
		return domain.EventSeverityWarning
	}

	return domain.EventSeverityLow
}

// isCriticalService checks if a service is considered critical
func (p *eventProcessor) isCriticalService(serviceName string) bool {
	criticalServices := []string{
		"sshd",
		"systemd-networkd",
		"systemd-resolved",
		"dbus",
		"systemd-journald",
		"kubelet",
		"docker",
		"containerd",
	}

	for _, critical := range criticalServices {
		if strings.Contains(serviceName, critical) {
			return true
		}
	}

	return false
}

// generateHash generates a hash for event deduplication
func (p *eventProcessor) generateHash(raw core.RawEvent) string {
	// Simple hash based on unit name, event type, and state transition
	return fmt.Sprintf("%s-%s-%s-%s-%d",
		raw.UnitName,
		raw.Type,
		raw.OldState,
		raw.NewState,
		raw.Timestamp.Unix())
}

// getHostname gets the system hostname
func (p *eventProcessor) getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return hostname
}
