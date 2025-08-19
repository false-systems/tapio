package parsers

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// GenericEvent represents a basic event structure that most collectors might use
type GenericEvent struct {
	Type        string            `json:"type"`
	Action      string            `json:"action,omitempty"`
	Target      string            `json:"target,omitempty"`
	Result      string            `json:"result,omitempty"`
	PID         int32             `json:"pid,omitempty"`
	ContainerID string            `json:"container_id,omitempty"`
	PodName     string            `json:"pod_name,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`
	NodeName    string            `json:"node_name,omitempty"`
	Data        map[string]string `json:"data,omitempty"`
}

// GenericParser provides basic parsing for sources without specialized parsers
type GenericParser struct {
	source string
}

// NewGenericParser creates a new generic event parser for a specific source
func NewGenericParser(source string) *GenericParser {
	return &GenericParser{
		source: source,
	}
}

// Source returns the source this parser handles
func (p *GenericParser) Source() string {
	return p.source
}

// Parse converts a generic RawEvent to an ObservationEvent
func (p *GenericParser) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("cannot parse nil event")
	}

	if raw.Source != p.source {
		return nil, fmt.Errorf("invalid source: expected %s, got %s", p.source, raw.Source)
	}

	// Try to parse as GenericEvent first
	var genericEvent GenericEvent
	if err := json.Unmarshal(raw.Data, &genericEvent); err != nil {
		// If that fails, create a minimal observation event
		return p.createMinimalEvent(raw), nil
	}

	// Create observation event from generic structure
	obs := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Timestamp: raw.Timestamp,
		Source:    p.source,
		Type:      genericEvent.Type,
	}

	// Use raw.Type if generic event type is empty
	if obs.Type == "" {
		obs.Type = raw.Type
	}

	// Add correlation keys if present
	if genericEvent.PID > 0 {
		obs.PID = &genericEvent.PID
	}

	if genericEvent.ContainerID != "" {
		obs.ContainerID = &genericEvent.ContainerID
	}

	if genericEvent.PodName != "" {
		obs.PodName = &genericEvent.PodName
	}

	if genericEvent.Namespace != "" {
		obs.Namespace = &genericEvent.Namespace
	}

	if genericEvent.ServiceName != "" {
		obs.ServiceName = &genericEvent.ServiceName
	}

	if genericEvent.NodeName != "" {
		obs.NodeName = &genericEvent.NodeName
	}

	// Set action, target, result if present
	if genericEvent.Action != "" {
		obs.Action = &genericEvent.Action
	}

	if genericEvent.Target != "" {
		obs.Target = &genericEvent.Target
	}

	if genericEvent.Result != "" {
		obs.Result = &genericEvent.Result
	}

	// Copy additional data
	if len(genericEvent.Data) > 0 {
		obs.Data = genericEvent.Data
	}

	// Ensure we have at least one correlation key
	if !obs.HasCorrelationKey() {
		// Try to extract from metadata
		if raw.Metadata != nil {
			if ns, ok := raw.Metadata["namespace"]; ok && ns != "" {
				obs.Namespace = &ns
			}
			if pod, ok := raw.Metadata["pod_name"]; ok && pod != "" {
				obs.PodName = &pod
			}
		}

		// Still no correlation key? Use source as a fallback namespace
		if !obs.HasCorrelationKey() {
			defaultNS := "system"
			obs.Namespace = &defaultNS
		}
	}

	return obs, nil
}

// createMinimalEvent creates a minimal observation event when parsing fails
func (p *GenericParser) createMinimalEvent(raw *domain.RawEvent) *domain.ObservationEvent {
	obs := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Timestamp: raw.Timestamp,
		Source:    p.source,
		Type:      raw.Type,
	}

	// Use metadata to set correlation keys
	if raw.Metadata != nil {
		if ns, ok := raw.Metadata["namespace"]; ok && ns != "" {
			obs.Namespace = &ns
		}
		if pod, ok := raw.Metadata["pod_name"]; ok && pod != "" {
			obs.PodName = &pod
		}
		if container, ok := raw.Metadata["container_id"]; ok && container != "" {
			obs.ContainerID = &container
		}
	}

	// Ensure at least one correlation key
	if !obs.HasCorrelationKey() {
		defaultNS := "system"
		obs.Namespace = &defaultNS
	}

	// Store raw data size as metadata
	obs.Data = map[string]string{
		"raw_data_size": fmt.Sprintf("%d", len(raw.Data)),
		"parse_status":  "minimal",
	}

	return obs
}
