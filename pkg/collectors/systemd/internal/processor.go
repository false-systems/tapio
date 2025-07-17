package internal

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	// Create service event payload
	payload := p.createServicePayload(raw)
	
	// Determine severity based on event type and state
	severity := p.determineSeverity(raw)
	
	// Create the domain event
	event := domain.Event{
		ID:        domain.EventID(fmt.Sprintf("systemd_%s_%s_%d", raw.UnitName, raw.Type, raw.Timestamp.UnixNano())),
		Type:      domain.EventTypeService,
		Source:    domain.SourceSystemd,
		Timestamp: raw.Timestamp,
		Payload:   payload,
		Context:   p.createContext(raw),
		Metadata:  p.createMetadata(raw),
		Severity:  severity,
		Confidence: 1.0, // systemd events are direct observations
		Fingerprint: domain.EventFingerprint{
			Hash:      p.generateHash(raw),
			Signature: fmt.Sprintf("%s:%s", raw.UnitName, string(raw.Type)),
			Fields: map[string]string{
				"unit_name": raw.UnitName,
				"unit_type": raw.UnitType,
				"type":      string(raw.Type),
			},
		},
	}
	
	return event, nil
}

// createServicePayload creates a service event payload
func (p *eventProcessor) createServicePayload(raw core.RawEvent) domain.ServiceEventPayload {
	payload := domain.ServiceEventPayload{
		ServiceName: raw.UnitName,
		EventType:   p.mapEventType(raw.Type),
		OldState:    raw.OldState,
		NewState:    raw.NewState,
		Properties:  make(map[string]string),
	}
	
	// Add exit code and signal if present
	if raw.ExitCode != 0 {
		payload.ExitCode = &raw.ExitCode
	}
	
	if raw.ExitStatus != 0 {
		exitStatus := raw.ExitStatus
		payload.Signal = &exitStatus
	}
	
	// Convert properties to string map
	for k, v := range raw.Properties {
		if str, ok := v.(string); ok {
			payload.Properties[k] = str
		} else {
			payload.Properties[k] = fmt.Sprintf("%v", v)
		}
	}
	
	// Add important properties
	payload.Properties["sub_state"] = raw.SubState
	payload.Properties["result"] = raw.Result
	if raw.MainPID > 0 {
		payload.Properties["main_pid"] = fmt.Sprintf("%d", raw.MainPID)
	}
	
	return payload
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

// createContext creates the event context
func (p *eventProcessor) createContext(raw core.RawEvent) domain.EventContext {
	labels := domain.Labels{
		"unit_name": raw.UnitName,
		"unit_type": raw.UnitType,
		"state":     raw.NewState,
		"sub_state": raw.SubState,
	}
	
	// Add result for failed services
	if raw.Result != "" && raw.Result != "success" {
		labels["result"] = raw.Result
	}
	
	tags := domain.Tags{
		"systemd",
		raw.UnitType,
	}
	
	// Add state tags
	if raw.NewState == core.StateFailed {
		tags = append(tags, "failed")
	}
	if raw.Type == core.EventTypeFailure {
		tags = append(tags, "failure")
	}
	
	ctx := domain.EventContext{
		Host:   p.getHostname(),
		Labels: labels,
		Tags:   tags,
	}
	
	// Add PID if available
	if raw.MainPID > 0 {
		pid := raw.MainPID
		ctx.PID = &pid
	}
	
	return ctx
}

// createMetadata creates the event metadata
func (p *eventProcessor) createMetadata(raw core.RawEvent) domain.EventMetadata {
	annotations := map[string]string{
		"event_type": string(raw.Type),
		"unit_type":  raw.UnitType,
		"result":     raw.Result,
	}
	
	// Add exit information if present
	if raw.ExitCode != 0 {
		annotations["exit_code"] = fmt.Sprintf("%d", raw.ExitCode)
	}
	if raw.ExitStatus != 0 {
		annotations["exit_status"] = fmt.Sprintf("%d", raw.ExitStatus)
	}
	
	return domain.EventMetadata{
		SchemaVersion: "1.0",
		ProcessedAt:   time.Now(),
		ProcessedBy:   "systemd-collector",
		Annotations:   annotations,
	}
}

// determineSeverity determines the event severity
func (p *eventProcessor) determineSeverity(raw core.RawEvent) domain.Severity {
	// Check for failures
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		// Critical services
		if p.isCriticalService(raw.UnitName) {
			return domain.SeverityCritical
		}
		return domain.SeverityError
	}
	
	// Check for restart events
	if raw.Type == core.EventTypeRestart {
		return domain.SeverityWarn
	}
	
	// State changes
	if raw.Type == core.EventTypeStateChange {
		if raw.OldState == core.StateActive && raw.NewState == core.StateInactive {
			return domain.SeverityWarn
		}
	}
	
	// Exit codes
	if raw.ExitCode != 0 {
		return domain.SeverityWarn
	}
	
	return domain.SeverityInfo
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
	// In production, use os.Hostname()
	// For now, return a placeholder
	return "localhost"
}